//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// sendReceipt reports what happened to a message the account sent. An empty
// kind is a delivery receipt, which is what the real server leaves unlabelled.
// A group message gets one receipt per member: the daemon only moves a group
// message to two ticks once every member has been heard from, which is what
// WhatsApp's own tick semantics do.
func (s *Server) sendReceipt(m *Msg, kind string) {
	sess := s.live()
	if sess == nil {
		return
	}
	senders := []types.JID{m.Chat.JID}
	if m.Chat.IsGroup {
		senders = senders[:0]
		for _, member := range m.Chat.Members {
			if !member.isSelf {
				senders = append(senders, member.JID)
			}
		}
	}
	for _, sender := range senders {
		attrs := waBinary.Attrs{
			"id":   m.ID,
			"from": m.Chat.JID,
			"t":    fmt.Sprintf("%d", time.Now().Unix()),
		}
		if m.Chat.IsGroup {
			attrs["participant"] = sender
			attrs["participant_lid"] = lidFor(sender)
		} else {
			attrs["sender_lid"] = lidFor(sender)
		}
		if kind != "" {
			attrs["type"] = kind
		}
		sess.enqueue(func(ctx context.Context) error {
			return sess.sendNode(ctx, waBinary.Node{Tag: "receipt", Attrs: attrs})
		})
	}
}

// sendChatState puts somebody in or out of the composing state.
func (s *Server) sendChatState(chat *Chat, from *Contact, composing bool) {
	sess := s.live()
	if sess == nil {
		return
	}
	tag := "paused"
	if composing {
		tag = "composing"
	}
	attrs := waBinary.Attrs{"from": from.JID}
	if chat.IsGroup {
		attrs["from"] = chat.JID
		attrs["participant"] = from.JID
	}
	sess.enqueue(func(ctx context.Context) error {
		return sess.sendNode(ctx, waBinary.Node{
			Tag:     "chatstate",
			Attrs:   attrs,
			Content: []waBinary.Node{{Tag: tag}},
		})
	})
}

// sendPresence reports whether somebody is available. The client only asks for
// this per chat, so an unsubscribed contact never hears about it.
func (s *Server) sendPresence(c *Contact, online bool) {
	sess := s.live()
	if sess == nil {
		return
	}
	attrs := waBinary.Attrs{"from": c.JID}
	if !online {
		attrs["type"] = "unavailable"
		_, lastSeen := s.world.presenceOf(c)
		if !lastSeen.IsZero() {
			attrs["last"] = fmt.Sprintf("%d", lastSeen.Unix())
		}
	}
	sess.enqueue(func(ctx context.Context) error {
		return sess.sendNode(ctx, waBinary.Node{Tag: "presence", Attrs: attrs})
	})
}

// handleClientPresence answers a presence subscription. Availability is only
// ever delivered on request, so this is the one place it can come from.
func (s *session) handleClientPresence(ctx context.Context, node *waBinary.Node) error {
	ag := node.AttrGetter()
	if ag.OptionalString("type") != "subscribe" {
		// The account telling us it is available or away. Nothing answers that.
		return nil
	}
	to := ag.OptionalJIDOrEmpty("to")
	world := s.srv.world
	if world == nil || to.IsEmpty() {
		return nil
	}
	contact, ok := world.contactByJID(to)
	if !ok {
		return nil
	}
	online, _ := world.presenceOf(contact)
	s.srv.sendPresence(contact, online)
	return nil
}

// live is the session a scenario's output goes to, if a frontend is connected.
func (s *Server) live() *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.liveSession
}
