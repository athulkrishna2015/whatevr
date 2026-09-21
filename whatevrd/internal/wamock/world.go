//go:build whatevr_mock

package wamock

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
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

	// messages is every message the world has made, by id, so app state
	// mutations that name one can find it. starred is the subset a scenario
	// marked, kept in order so the patches encode the same way every run.
	messages map[string]*Msg
	starred  []string

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
func (c *Chat) Archive() *Chat { return c.SetArchived(true) }

// Unarchive is Archive the other way round.
func (c *Chat) Unarchive() *Chat { return c.SetArchived(false) }

func (c *Chat) SetArchived(archived bool) *Chat {
	c.archived = archived
	c.pushState(appstate.BuildArchive(c.JID, archived, time.Time{}, nil))
	return c
}

// Mute silences the chat forever, the same way the app state does.
func (c *Chat) Mute() *Chat { return c.SetMuted(true) }

// Unmute is Mute the other way round.
func (c *Chat) Unmute() *Chat { return c.SetMuted(false) }

func (c *Chat) SetMuted(muted bool) *Chat {
	c.muted = muted
	c.pushState(appstate.BuildMuteAbs(c.JID, muted, nil))
	return c
}

// Unread sets the badge the chat arrives with. It is part of the history sync,
// so it describes what the account had before this device was linked.
func (c *Chat) Unread(count uint32) *Chat {
	c.unread = count
	return c
}

// MarkRead clears the chat's badge the way reading it on the phone would.
// Unlike Unread, this one travels as app state, so it works mid-run.
func (c *Chat) MarkRead() *Chat { return c.setRead(true) }

// MarkUnread is the deliberate "leave this for later" badge.
func (c *Chat) MarkUnread() *Chat { return c.setRead(false) }

func (c *Chat) setRead(read bool) *Chat {
	if read {
		c.unread = 0
	} else if c.unread == 0 {
		c.unread = 1
	}
	c.pushState(appstate.BuildMarkChatAsRead(c.JID, read, time.Time{}, nil))
	return c
}

// Pin puts the chat in the pinned section.
func (c *Chat) Pin() *Chat { return c.SetPinned(true) }

// Unpin is Pin the other way round.
func (c *Chat) Unpin() *Chat { return c.SetPinned(false) }

func (c *Chat) SetPinned(pinned bool) *Chat {
	c.pinOrder = 0
	if pinned {
		c.pinOrder = 1
	}
	c.pushState(appstate.BuildPin(c.JID, pinned))
	return c
}

// pushState sends an app state change to a client that is already connected.
// Before login it does nothing: the flags set above are what the login snapshot
// is built from, and pushing as well would encode the mutation twice.
func (c *Chat) pushState(info appstate.PatchInfo) {
	c.w.srv.pushAppState(info)
}

// Msg is one message in the world.
type Msg struct {
	ID     string
	Chat   *Chat
	From   *Contact
	Text   string
	At     time.Time
	FromMe bool

	// media is the protobuf an attachment built, hosted and encrypted at the
	// moment the message was made. Nil for an ordinary text message.
	media *waE2E.Message

	// starred and the rest are app state the account already had, which the
	// client can also change from a frontend.
	starred bool
}

// payload is what goes inside the Signal envelope.
func (m *Msg) payload() *waE2E.Message {
	if m.media != nil {
		return m.media
	}
	return &waE2E.Message{Conversation: proto.String(m.Text)}
}

// stanzaType is what the real server puts in the type attribute: media
// messages are announced as such before anything is decrypted.
func (m *Msg) stanzaType() string {
	if m.media != nil {
		return "media"
	}
	return "text"
}

// Star marks the message starred, the way it would be if somebody had starred
// it on another device. It rides the same app state collection the UI writes
// to, so unstarring it from a frontend works.
func (m *Msg) Star() *Msg { return m.SetStarred(true) }

// Unstar is Star the other way round.
func (m *Msg) Unstar() *Msg { return m.SetStarred(false) }

func (m *Msg) SetStarred(starred bool) *Msg {
	w := m.Chat.w
	w.setStarred(m.ID, starred)
	sender := m.From.JID
	if m.FromMe {
		sender = w.Self().JID
	}
	w.srv.pushAppState(appstate.BuildStar(m.Chat.JID, sender, m.ID, m.FromMe, starred))
	return m
}

func newWorld(srv *Server) *World {
	w := &World{
		srv:           srv,
		contacts:      map[string]*Contact{},
		chats:         map[string]*Chat{},
		messages:      map[string]*Msg{},
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
	m := &Msg{
		ID:     c.w.srv.rng.messageID(),
		Chat:   c,
		From:   from,
		Text:   text,
		At:     at,
		FromMe: from.isSelf,
	}
	c.w.mu.Lock()
	c.w.messages[m.ID] = m
	c.w.mu.Unlock()
	return m
}

// newAttachment is newMsg for media. The file is generated, encrypted and
// hosted here rather than at delivery time, so a scenario that builds a hundred
// messages fails loudly at build rather than quietly on the wire.
func (c *Chat) newAttachment(from *Contact, a *Attachment, at time.Time) *Msg {
	m := c.newMsg(from, a.text(), at)
	media, err := c.w.srv.buildAttachment(a, m.ID)
	if err != nil {
		c.w.srv.log.Printf("attachment in %s: %v", c.Name, err)
		return m
	}
	m.media = media
	return m
}

// Attach puts a media message in the chat, the same way Say puts a text one:
// queued as an offline sync before login, live on the wire afterwards.
func (c *Chat) Attach(from *Contact, a *Attachment, at time.Time) *Msg {
	m := c.newAttachment(from, a, at)
	c.deliver(m)
	return m
}

// AttachFromMe is Attach for something the account itself sent.
func (c *Chat) AttachFromMe(a *Attachment, at time.Time) *Msg {
	return c.Attach(c.w.self, a, at)
}

// AttachHistory is Attach for media the account already had, so it arrives
// through a real history sync rather than as live traffic.
func (c *Chat) AttachHistory(from *Contact, a *Attachment, at time.Time) *Msg {
	m := c.newAttachment(from, a, at)
	c.w.mu.Lock()
	c.w.history = append(c.w.history, m)
	c.w.mu.Unlock()
	return m
}

func (c *Chat) add(from *Contact, text string, at time.Time) *Msg {
	m := c.newMsg(from, text, at)
	c.deliver(m)
	return m
}

func (c *Chat) deliver(m *Msg) {
	w := c.w
	w.mu.Lock()
	live := w.live
	if !live {
		w.backlog = append(w.backlog, m)
	}
	w.mu.Unlock()
	if live {
		w.srv.deliverLive(m)
	}
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

// starredMessages is every message a scenario marked starred. Stars live in app
// state rather than on the message, so they are collected here and encoded as
// patches at login.
func (w *World) starredMessages() []*Msg {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]*Msg, 0, len(w.starred))
	for _, id := range w.starred {
		if m, ok := w.messages[id]; ok && m.starred {
			out = append(out, m)
		}
	}
	return out
}

// setStarred records a star the client made, so a scenario can read back what
// the UI did.
func (w *World) setStarred(messageID string, starred bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.messages[messageID]
	if !ok {
		return
	}
	if starred && !m.starred {
		w.starred = append(w.starred, messageID)
	}
	m.starred = starred
}

// Starred reports whether a message is starred, which is how a scenario checks
// that a star made in the UI reached the server.
func (m *Msg) Starred() bool { return m.starred }

// Msg finds a message by the id the mock gave it.
func (w *World) Msg(id string) (*Msg, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.messages[id]
	return m, ok
}

// remember files a message under its id. Messages the world made are filed by
// newMsg; this is for the ones the account sent, which arrive already built.
func (w *World) remember(m *Msg) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages[m.ID] = m
}
