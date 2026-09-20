package ui

import (
	"whattui/internal/proto"
	"whattui/internal/view"
)

// conversation is one open transcript: its subscription, its window state and
// where the reader is in it.
//
// All of it is presentation state. The messages themselves live in the
// collection, which the daemon keeps correct.
type conversation struct {
	chatID string
	sub    *proto.Subscription
	msgs   *view.Collection[proto.MessageRow]

	// scroll counts rows up from the live edge. Zero is pinned to the bottom,
	// which is where a chat opens and where it stays while messages arrive.
	scroll int
	// selected is the message the actions act on, by id, or empty for none.
	selected string

	atLiveEdge   bool
	canLoadOlder bool
	// olderFailed stops a rejected extend from spinning the list: without it
	// the guard never clears and the transcript asks forever.
	olderFailed   bool
	extendPending bool
}

func (a *App) openChat(chatID string) {
	a.mu.Lock()
	if a.conversation != nil && a.conversation.chatID == chatID {
		a.activeChat = chatID
		a.focus = FocusComposer
		a.mu.Unlock()
		return
	}
	old := a.conversation
	a.mu.Unlock()

	if old != nil && old.sub != nil {
		old.sub.Close()
	}

	c := &conversation{
		chatID:       chatID,
		msgs:         view.NewCollection[proto.MessageRow](),
		canLoadOlder: true,
	}
	// The live edge is row 0, so the transcript reads bottom to top and a new
	// message lands where the reader already is.
	c.msgs.SetReverse(true)

	c.sub = a.client.Subscribe("messages", proto.Params{
		"chat_id": chatID,
		"anchor":  "latest",
		"limit":   messagePageSize,
	}, c.msgs)

	c.sub.OnExtendFailed = func(direction string, _ *proto.Error) {
		a.mu.Lock()
		if a.conversation == c {
			c.extendPending = false
			if direction == proto.Older {
				c.olderFailed = true
			}
		}
		a.mu.Unlock()
		a.vx.PostEvent(redraw{})
	}

	a.mu.Lock()
	a.conversation = c
	a.activeChat = chatID
	a.focus = FocusComposer
	a.mu.Unlock()

	// Telling the daemon which chat is open is what makes its notifier stay
	// quiet about the one the reader is looking at.
	a.client.Do("session.update", proto.Params{"focused": true, "active_chat_id": chatID}, nil)
}

const messagePageSize = 60

// loadOlder reaches back up the transcript. Guarded so a scroll that outruns
// the daemon does not queue a hundred extends, and so a rejected one stops.
func (a *App) loadOlder() {
	a.mu.Lock()
	c := a.conversation
	if c == nil || c.extendPending || c.olderFailed || !c.canLoadOlder {
		a.mu.Unlock()
		return
	}
	if exhausted, has := c.msgs.Exhausted(); has && exhausted {
		c.canLoadOlder = false
		a.mu.Unlock()
		return
	}
	c.extendPending = true
	sub := c.sub
	a.mu.Unlock()

	if sub != nil {
		sub.Extend(messagePageSize, proto.Older)
	}
}

// noteReady clears the extend guard when a window finishes filling.
func (a *App) noteReady() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conversation != nil {
		a.conversation.extendPending = false
	}
}
