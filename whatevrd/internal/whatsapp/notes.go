package whatsapp

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	appstore "whatevrd/internal/store"
)

// Notifier shows notifications on the desktop. Show with an id already up
// replaces it.
type Notifier interface {
	Show(n live.Notification)
	Close(id string)
}

const (
	// older than this is a backlog coming in on connect, not news
	noteFresh = 2 * time.Minute
	noteQueue = 256
)

// notes decides what deserves a notification, one per chat, and drops it
// once the chat is read.
type notes struct {
	c        *Client
	arrivals chan noteArrival
}

type noteArrival struct {
	evt *events.Message
	seq int64
}

func newNotes(c *Client) *notes {
	return &notes{c: c, arrivals: make(chan noteArrival, noteQueue)}
}

// message is a live message, on whatsmeow's goroutine: it only queues.
func (n *notes) message(evt *events.Message) {
	chat := evt.Info.Chat
	if chat.Server == types.BroadcastServer || chat.Server == types.NewsletterServer {
		return
	}
	if evt.Info.IsFromMe {
		// we wrote there, so it is read
		n.read(n.c.chatKey(chat))
		return
	}
	if time.Since(evt.Info.Timestamp) > noteFresh {
		return
	}
	_, seq := n.c.core.Progress()
	select {
	case n.arrivals <- noteArrival{evt: evt, seq: seq}:
	default:
	}
}

func (n *notes) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case a := <-n.arrivals:
			if err := n.c.core.WaitFolded(ctx, a.seq); err != nil {
				return
			}
			n.look(ctx, a.evt)
		}
	}
}

func (n *notes) look(ctx context.Context, evt *events.Message) {
	c := n.c
	p := c.prefs(ctx)
	if !p.GetNotifications() {
		return
	}
	chat := c.chatKey(evt.Info.Chat)
	if c.Looking(chat) {
		return
	}
	w, err := c.world()
	if err != nil {
		return
	}
	ch, ok, err := c.r.ChatIn(ctx, w, chat)
	if err != nil || !ok || ch.Muted {
		return
	}
	line, id := "", evt.Info.ID
	if r := evt.Message.GetReactionMessage(); r != nil {
		// a reaction is news only on our own message
		if r.GetText() == "" || !r.GetKey().GetFromMe() {
			return
		}
		line, id = "Reacted "+r.GetText()+" to your message", r.GetKey().GetID()
	} else {
		m, _, err := c.message(ctx, Ref{Chat: chat, ID: string(evt.Info.ID)})
		// what is not a row of its own (an edit, a vote, a key) counts for
		// nothing, the same as for unread
		if err != nil || m.FromMe || strings.HasPrefix(m.Kind, "stub:") || m.System != nil {
			return
		}
		if sm, _, ok := NewDecoder(WorldNames(w), c.o.Paths.MediaCacheDir).Model(ctx, w, m); ok {
			line = appstore.MessagePreviewLine(sm)
		}
		if line == "" {
			line = "New message"
		}
	}
	sender := c.personKey(evt.Info.Sender)
	if ch.Group {
		if name, _ := w.Name(sender); name != "" {
			line = name + ": " + line
		}
	}
	if !p.GetNotificationPreview() {
		line = ""
	}
	title := ch.Name
	if title == "" {
		title, _ = w.Name(chat)
	}
	note := live.Notification{ID: chat, Chat: chat, Message: id, Title: title, Body: line, Sender: sender,
		Avatar: n.avatar(ctx, w, chat), Count: 1, T: evt.Info.Timestamp, Sound: p.GetNotificationSound()}
	for _, x := range c.live.Notifications() {
		if x.ID == note.ID {
			note.Count += x.Count
		}
	}
	c.live.Notify(note)
	if c.o.Notifier != nil && !c.FrontendNotifies() {
		c.o.Notifier.Show(note)
	}
}

// avatar is the chat's picture file if there is one, and a nudge to fetch
// it when not.
func (n *notes) avatar(ctx context.Context, w *model.World, chat string) string {
	av, err := n.c.r.Avatars(ctx, w.Addrs(chat))
	if err == nil {
		for _, a := range av {
			if a.Status == core.AvatarOK && have(a.Path) {
				return a.Path
			}
		}
	}
	n.c.WantAvatars([]string{chat})
	return ""
}

// read drops a chat's notification: it was read somewhere.
func (n *notes) read(chat string) {
	if n.c.live.Dismiss(func(x live.Notification) bool { return x.Chat == chat }) && n.c.o.Notifier != nil {
		n.c.o.Notifier.Close(chat)
	}
}

// DismissNotification drops one notification everywhere.
func (c *Client) DismissNotification(id string) {
	if c.live.Dismiss(func(x live.Notification) bool { return x.ID == id }) && c.o.Notifier != nil {
		c.o.Notifier.Close(id)
	}
}

// forget takes every notification down, on logout.
func (n *notes) forget() {
	if n.c.o.Notifier != nil {
		for _, x := range n.c.live.Notifications() {
			n.c.o.Notifier.Close(x.ID)
		}
	}
	for {
		select {
		case <-n.arrivals:
		default:
			return
		}
	}
}
