// Package live holds what the daemon knows that is not worth a log entry:
// the connection, the login, who is typing, who is online, transfers in
// flight, notifications, abouts fetched. it lives in memory, and every
// change is told as a core.Change so views wake on it like on a fold.
package live

import (
	"maps"
	"slices"
	"sync"
	"time"

	"whatevrd/internal/conn"
	"whatevrd/internal/core"
)

// touch kinds, beside the core's
const (
	TouchConn         = "conn"
	TouchLogin        = "login"
	TouchTyping       = "typing"
	TouchPresence     = "presence"
	TouchTransfer     = "transfer"
	TouchNotification = "notification"
	TouchAbout        = "about"
	TouchProblems     = "problems"
)

type LoginState int

const (
	LoginUnknown LoginState = iota
	LoggedIn
	ShowingQR
	Pairing
	LoginFailed
)

type Login struct {
	State   LoginState
	Detail  string
	QR      string
	Expires time.Time
}

// Typist is someone composing in a chat, keyed by their address.
type Typist struct {
	Addr      string
	Recording bool
	Since     time.Time
}

type Presence struct {
	Online   bool
	LastSeen time.Time
	At       time.Time
}

// Transfer is a download or upload moving bytes now.
type Transfer struct {
	Chat, ID string
	Upload   bool
	Done     uint64
	Total    uint64
	Error    string
}

type Notification struct {
	ID string
	// Chat is the chat's key
	Chat    string
	Message string
	Title   string
	Body    string
	Sender  string
	Avatar  string
	Count   int
	T       time.Time
	Sound   bool
}

// About is a person's status text as whatsapp gave it.
type About struct {
	Text string
	At   time.Time
}

// typing expires on its own when the sender's client goes quiet
const typingFor = 25 * time.Second

type Hub struct {
	OnChange func(core.Change)

	mu        sync.Mutex
	conn      conn.Status
	login     Login
	typing    map[string]map[string]Typist
	presence  map[string]Presence
	transfers map[string]Transfer
	notes     []Notification
	about     map[string]About
	now       func() time.Time
}

func New() *Hub {
	return &Hub{typing: map[string]map[string]Typist{}, presence: map[string]Presence{}, transfers: map[string]Transfer{},
		about: map[string]About{}, now: time.Now}
}

func (h *Hub) tell(kind string, keys ...string) {
	if h.OnChange == nil {
		return
	}
	c := core.Change{Keys: map[string][]string{kind: keys}}
	if len(keys) == 0 {
		c = core.Change{All: map[string]bool{kind: true}}
	}
	h.OnChange(c)
}

func (h *Hub) SetConn(s conn.Status) {
	h.mu.Lock()
	h.conn = s
	h.mu.Unlock()
	h.tell(TouchConn)
}

func (h *Hub) Conn() conn.Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn
}

func (h *Hub) SetLogin(l Login) {
	h.mu.Lock()
	h.login = l
	h.mu.Unlock()
	h.tell(TouchLogin)
}

func (h *Hub) Login() Login {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.login
}

// Problems is told when the status board moves.
func (h *Hub) Problems() { h.tell(TouchProblems) }

// SetTyping says who is composing in chat; both are model keys.
func (h *Hub) SetTyping(chat, who string, on, recording bool) {
	h.mu.Lock()
	m := h.typing[chat]
	_, was := m[who]
	if on {
		if m == nil {
			m = map[string]Typist{}
			h.typing[chat] = m
		}
		m[who] = Typist{Addr: who, Recording: recording, Since: h.now()}
	} else {
		delete(m, who)
		if len(m) == 0 {
			delete(h.typing, chat)
		}
	}
	h.mu.Unlock()
	if on || was {
		h.tell(TouchTyping, chat)
	}
	if on {
		time.AfterFunc(typingFor, func() { h.expire(chat, who) })
	}
}

func (h *Hub) expire(chat, who string) {
	h.mu.Lock()
	t, ok := h.typing[chat][who]
	gone := ok && h.now().Sub(t.Since) >= typingFor
	if gone {
		delete(h.typing[chat], who)
		if len(h.typing[chat]) == 0 {
			delete(h.typing, chat)
		}
	}
	h.mu.Unlock()
	if gone {
		h.tell(TouchTyping, chat)
	}
}

// Typing is every chat with someone composing, by chat key.
func (h *Hub) Typing() map[string][]Typist {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string][]Typist, len(h.typing))
	for chat, m := range h.typing {
		ts := slices.Collect(maps.Values(m))
		slices.SortFunc(ts, func(a, b Typist) int { return a.Since.Compare(b.Since) })
		out[chat] = ts
	}
	return out
}

// SetPresence is a person's availability, by model key.
func (h *Hub) SetPresence(who string, p Presence) {
	h.mu.Lock()
	p.At = h.now()
	h.presence[who] = p
	h.mu.Unlock()
	h.tell(TouchPresence, who)
}

func (h *Hub) Presence(who string) (Presence, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.presence[who]
	return p, ok
}

// SetTransfer starts or moves a transfer; Done stops it.
func (h *Hub) SetTransfer(t Transfer) {
	k := t.Chat + ":" + t.ID
	h.mu.Lock()
	h.transfers[k] = t
	h.mu.Unlock()
	h.tell(TouchTransfer, k)
}

func (h *Hub) EndTransfer(chat, id string) {
	k := chat + ":" + id
	h.mu.Lock()
	_, ok := h.transfers[k]
	delete(h.transfers, k)
	h.mu.Unlock()
	if ok {
		h.tell(TouchTransfer, k)
	}
}

func (h *Hub) Transfers() []Transfer {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Collect(maps.Values(h.transfers))
}

// Transferring is whether chat:id is moving bytes now.
func (h *Hub) Transferring(chat, id string) (Transfer, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t, ok := h.transfers[chat+":"+id]
	return t, ok
}

// Notify adds or replaces the notification with n's id.
func (h *Hub) Notify(n Notification) {
	h.mu.Lock()
	h.notes = slices.DeleteFunc(h.notes, func(x Notification) bool { return x.ID == n.ID })
	h.notes = append(h.notes, n)
	h.mu.Unlock()
	h.tell(TouchNotification)
}

// Dismiss drops notifications for which drop says so, and says whether any
// went.
func (h *Hub) Dismiss(drop func(Notification) bool) bool {
	h.mu.Lock()
	n := len(h.notes)
	h.notes = slices.DeleteFunc(h.notes, drop)
	gone := len(h.notes) != n
	h.mu.Unlock()
	if gone {
		h.tell(TouchNotification)
	}
	return gone
}

func (h *Hub) Notifications() []Notification {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.notes)
}

func (h *Hub) SetAbout(who string, a About) {
	h.mu.Lock()
	h.about[who] = a
	h.mu.Unlock()
	h.tell(TouchAbout, who)
}

func (h *Hub) About(who string) (About, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	a, ok := h.about[who]
	return a, ok
}

// Reset forgets everything an account had, on logout.
func (h *Hub) Reset() {
	h.mu.Lock()
	h.typing = map[string]map[string]Typist{}
	h.presence = map[string]Presence{}
	h.transfers = map[string]Transfer{}
	h.notes = nil
	h.about = map[string]About{}
	h.mu.Unlock()
	for _, k := range []string{TouchTyping, TouchPresence, TouchTransfer, TouchNotification, TouchAbout} {
		h.tell(k)
	}
}
