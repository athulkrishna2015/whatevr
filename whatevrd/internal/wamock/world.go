//go:build whatevr_mock

package wamock

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// bootTime is the instant every relative scenario timestamp hangs off. It is
// fixed once per process so that two chats built in the same scenario agree on
// what "three hours ago" means, however long the build took.
var bootTime = time.Now()

// Ago is a scenario timestamp that many durations before the mock booted. Day
// dividers and relative labels in the frontends depend on messages sitting at
// sensible distances from now rather than at fixed wall-clock dates.
func Ago(d time.Duration) time.Time { return bootTime.Add(-d) }

// World is the account a scenario describes: who is in it, what they said, and
// what happens once a frontend connects. A scenario builds it once, before the
// daemon logs in, and may keep mutating it from timeline callbacks afterwards.
type World struct {
	srv *Server

	mu       sync.Mutex
	self     *Contact
	contacts map[string]*Contact
	chats    map[string]*Chat
	order    []*Chat
	backlog  []*Msg
	history  []*Msg
	timeline []timedAction

	// live flips once a frontend is connected and caught up. Before it, a Say
	// joins the backlog; after it, a Say goes out on the wire immediately.
	live bool

	// announced tracks which groups the client has been told about, so a group
	// is created once whether it came from the scenario or from the timeline.
	announced map[string]bool

	groupSeq int

	// onSend is what scenarios hang replies off: every message the account
	// sends from a frontend runs through these.
	onSend []func(*Msg)

	// deliveryDelay and readDelay pace the receipts the mock sends back for a
	// message the account sent. A zero readDelay leaves messages on one tick,
	// which is a state worth being able to script.
	deliveryDelay time.Duration
	readDelay     time.Duration

	// online is who is currently available, for the presence view.
	online map[string]time.Time

	// historyDelay paces the history sync chunks.
	historyDelay time.Duration
}

// HistoryPace spreads the history sync out, one chunk every d. A real initial
// sync takes a while, and a frontend's sync view has nothing to show unless
// something takes long enough to see.
func (w *World) HistoryPace(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.historyDelay = d
}

func (w *World) historyPace() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.historyDelay
}

// Contact is one person in the world, including the account itself.
type Contact struct {
	// JID is the address they are known by: a phone number on s.whatsapp.net.
	JID types.JID
	// Name is the push name they present, which is what a frontend shows for
	// anyone the account has not saved.
	Name string
	// Saved is the address-book name, which is what a frontend shows first.
	// It defaults to the push name, because an account full of phone numbers
	// is not what anybody wants to develop against; call Unsaved to get one.
	Saved string

	isSelf bool
}

// Unsaved drops the address-book name, so this contact renders the way somebody
// who is not in your phone does: by number, with the push name only as a
// fallback.
func (c *Contact) Unsaved() *Contact {
	c.Saved = ""
	return c
}

// SavedAs sets the address-book name, for when it should differ from the push
// name the contact set for themselves.
func (c *Contact) SavedAs(name string) *Contact {
	c.Saved = name
	return c
}

// Chat is a conversation, either one to one or a group.
type Chat struct {
	w *World

	JID     types.JID
	Name    string
	IsGroup bool
	Members []*Contact

	// CreatedAt is when the group came into being. Ignored for direct chats.
	CreatedAt time.Time

	unread   uint32
	pinOrder uint32
	archived bool
	muted    bool
}

// Archive puts the chat in the archived section. It arrives as an app state
// patch, which is where a real account keeps it.
func (c *Chat) Archive() *Chat {
	c.archived = true
	return c
}

// Mute silences the chat forever, the same way the app state does.
func (c *Chat) Mute() *Chat {
	c.muted = true
	return c
}

// Unread sets the badge the chat arrives with. It is part of the history sync,
// so it describes what the account had before this device was linked.
func (c *Chat) Unread(count uint32) *Chat {
	c.unread = count
	return c
}

// Pin puts the chat in the pinned section. Pinning from the UI is app state and
// so is stage 4; this is the state the account already had.
func (c *Chat) Pin() *Chat {
	c.pinOrder = 1
	return c
}

// Msg is one message in the world.
type Msg struct {
	ID     string
	Chat   *Chat
	From   *Contact
	Text   string
	At     time.Time
	FromMe bool
}

func newWorld(srv *Server) *World {
	w := &World{
		srv:           srv,
		contacts:      map[string]*Contact{},
		chats:         map[string]*Chat{},
		announced:     map[string]bool{},
		online:        map[string]time.Time{},
		deliveryDelay: defaultDeliveryDelay,
		readDelay:     defaultReadDelay,
	}
	w.self = &Contact{
		JID:    srv.accountJID(),
		Name:   srv.opts.AccountName,
		isSelf: true,
	}
	w.contacts[w.self.JID.String()] = w.self
	return w
}

// Default receipt pacing. Fast enough that a frontend does not look stuck,
// slow enough that the pending and sent states are actually visible.
const (
	defaultDeliveryDelay = 400 * time.Millisecond
	defaultReadDelay     = 1500 * time.Millisecond
)

// Receipts sets how long after a send the mock reports delivered and read.
// A zero read delay means nobody ever reads anything, which is how you get a
// chat that sits on one tick.
func (w *World) Receipts(delivery, read time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deliveryDelay = delivery
	w.readDelay = read
}

// OnSend registers a hook that runs for every message the account sends from a
// frontend. This is how a scenario answers back.
func (w *World) OnSend(fn func(m *Msg)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onSend = append(w.onSend, fn)
}

func (w *World) sendHooks() []func(*Msg) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]func(*Msg){}, w.onSend...)
}

func (w *World) receiptDelays() (delivery, read time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.deliveryDelay, w.readDelay
}

// Reply is Say with the clock set to now. Scenarios use it from OnSend hooks,
// where the point is that something answers while the frontend watches.
func (c *Chat) Reply(from *Contact, text string) *Msg {
	return c.add(from, text, time.Now())
}

// Typing shows a contact composing in this chat, then stops after d. A zero
// duration leaves them composing until something else stops them, which is what
// the real client does when somebody walks away mid-message.
func (c *Chat) Typing(from *Contact, d time.Duration) {
	c.w.srv.sendChatState(c, from, true)
	if d <= 0 {
		return
	}
	time.AfterFunc(d, func() { c.w.srv.sendChatState(c, from, false) })
}

// SetOnline marks a contact available or not. The presence view only shows it
// once a frontend has subscribed to that chat, which is what the real protocol
// does too.
func (w *World) SetOnline(c *Contact, online bool) {
	w.mu.Lock()
	if online {
		w.online[c.JID.String()] = time.Time{}
	} else {
		w.online[c.JID.String()] = time.Now()
	}
	w.mu.Unlock()
	w.srv.sendPresence(c, online)
}

// presenceOf reports whether a contact is available, and when they were last
// seen if they are not.
func (w *World) presenceOf(c *Contact) (online bool, lastSeen time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	last, ok := w.online[c.JID.String()]
	if !ok {
		// Never scripted either way: offline, last seen a while back, which is
		// the least surprising thing for a UI to render.
		return false, Ago(2 * time.Hour)
	}
	return last.IsZero(), last
}

// Self is the account the daemon is logged in as.
func (w *World) Self() *Contact { return w.self }

// Contact adds somebody to the world, or returns them if they are already in
// it. The phone number is plain digits, country code included.
func (w *World) Contact(phone, name string) *Contact {
	jid := types.JID{User: phone, Server: types.DefaultUserServer}
	w.mu.Lock()
	defer w.mu.Unlock()
	if existing, ok := w.contacts[jid.String()]; ok {
		return existing
	}
	c := &Contact{JID: jid, Name: name, Saved: name}
	w.contacts[jid.String()] = c
	return c
}

// DM returns the one to one chat with a contact.
func (w *World) DM(c *Contact) *Chat {
	w.mu.Lock()
	defer w.mu.Unlock()
	if existing, ok := w.chats[c.JID.String()]; ok {
		return existing
	}
	chat := &Chat{w: w, JID: c.JID, Name: c.Name, Members: []*Contact{w.self, c}}
	w.chats[c.JID.String()] = chat
	w.order = append(w.order, chat)
	return chat
}

// Group creates a group chat with the account and the given members in it.
func (w *World) Group(name string, members ...*Contact) *Chat {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.groupSeq++
	// Real group ids are an 18 digit number that starts 1203 and encodes the
	// creation time. Nothing reads them, so a counter keeps them stable across
	// runs, which a timestamp would not.
	jid := types.JID{
		User:   fmt.Sprintf("120363%012d", 100000+w.groupSeq),
		Server: types.GroupServer,
	}
	chat := &Chat{
		w:         w,
		JID:       jid,
		Name:      name,
		IsGroup:   true,
		Members:   append([]*Contact{w.self}, members...),
		CreatedAt: Ago(30 * 24 * time.Hour),
	}
	w.chats[jid.String()] = chat
	w.order = append(w.order, chat)
	return chat
}

// Other is somebody in the chat who is not the account. Scenarios that answer
// back want whoever is on the far end without caring who that is.
func (c *Chat) Other() *Contact {
	for _, member := range c.Members {
		if !member.isSelf {
			return member
		}
	}
	return nil
}

// Say puts a message in the chat from one of its members. Before a frontend
// connects it joins the backlog the mock delivers on login; afterwards it goes
// out live.
func (c *Chat) Say(from *Contact, text string, at time.Time) *Msg {
	return c.add(from, text, at)
}

// History adds a message the account already had before this device was linked.
// It arrives through a real history sync rather than as live traffic, which is
// what gives a chat something to scroll back into.
func (c *Chat) History(from *Contact, text string, at time.Time) *Msg {
	m := c.newMsg(from, text, at)
	c.w.mu.Lock()
	c.w.history = append(c.w.history, m)
	c.w.mu.Unlock()
	return m
}

// HistoryFromMe is History for something the account itself said.
func (c *Chat) HistoryFromMe(text string, at time.Time) *Msg {
	return c.History(c.w.self, text, at)
}

// SayFromMe puts a message in the chat from the account itself, the way a
// message typed on the phone reaches a linked device.
func (c *Chat) SayFromMe(text string, at time.Time) *Msg {
	return c.add(c.w.self, text, at)
}

func (c *Chat) newMsg(from *Contact, text string, at time.Time) *Msg {
	return &Msg{
		ID:     c.w.srv.rng.messageID(),
		Chat:   c,
		From:   from,
		Text:   text,
		At:     at,
		FromMe: from.isSelf,
	}
}

func (c *Chat) add(from *Contact, text string, at time.Time) *Msg {
	w := c.w
	m := c.newMsg(from, text, at)
	w.mu.Lock()
	live := w.live
	if !live {
		w.backlog = append(w.backlog, m)
	}
	w.mu.Unlock()
	if live {
		w.srv.deliverLive(m)
	}
	return m
}

// markAnnounced records that a group has been introduced and reports whether
// this call was the one that did it.
func (w *World) markAnnounced(chat *Chat) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.announced[chat.JID.String()] {
		return false
	}
	w.announced[chat.JID.String()] = true
	return true
}

// contactByJID finds a person the world knows about.
func (w *World) contactByJID(jid types.JID) (*Contact, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.contacts[jid.ToNonAD().String()]
	return c, ok
}

// chatByJID finds a chat the world knows about.
func (w *World) chatByJID(jid types.JID) (*Chat, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	chat, ok := w.chats[jid.ToNonAD().String()]
	return chat, ok
}

// takeBacklog returns the messages that accumulated before login, oldest
// first, and marks the world live so later Says are delivered rather than
// queued. Calling it twice yields nothing the second time: a reconnect must not
// replay a conversation the frontend already has.
func (w *World) takeBacklog() []*Msg {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live {
		return nil
	}
	w.live = true
	out := w.backlog
	w.backlog = nil
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}
