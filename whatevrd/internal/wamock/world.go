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
	timeline []timedAction

	// live flips once a frontend is connected and caught up. Before it, a Say
	// joins the backlog; after it, a Say goes out on the wire immediately.
	live bool

	// announced tracks which groups the client has been told about, so a group
	// is created once whether it came from the scenario or from the timeline.
	announced map[string]bool

	groupSeq int
}

// Contact is one person in the world, including the account itself.
type Contact struct {
	// JID is the address they are known by: a phone number on s.whatsapp.net.
	JID types.JID
	// Name is the push name they present, which is what a frontend shows for
	// anyone the account has not saved.
	Name string

	isSelf bool
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
		srv:       srv,
		contacts:  map[string]*Contact{},
		chats:     map[string]*Chat{},
		announced: map[string]bool{},
	}
	w.self = &Contact{
		JID:    srv.accountJID(),
		Name:   srv.opts.AccountName,
		isSelf: true,
	}
	w.contacts[w.self.JID.String()] = w.self
	return w
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
	c := &Contact{JID: jid, Name: name}
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

// Say puts a message in the chat from one of its members. Before a frontend
// connects it joins the backlog the mock delivers on login; afterwards it goes
// out live.
func (c *Chat) Say(from *Contact, text string, at time.Time) *Msg {
	return c.add(from, text, at)
}

// SayFromMe puts a message in the chat from the account itself, the way a
// message typed on the phone reaches a linked device.
func (c *Chat) SayFromMe(text string, at time.Time) *Msg {
	return c.add(c.w.self, text, at)
}

func (c *Chat) add(from *Contact, text string, at time.Time) *Msg {
	w := c.w
	m := &Msg{
		ID:     w.srv.rng.messageID(),
		Chat:   c,
		From:   from,
		Text:   text,
		At:     at,
		FromMe: from.isSelf,
	}
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
