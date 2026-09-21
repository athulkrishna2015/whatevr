//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// keysWait is how long the mock waits for the client to upload the prekeys it
// needs before giving up on delivering anything. The upload happens within a
// second of login in practice.
const keysWait = 20 * time.Second

// selfDevice is the device number the account's other device speaks as. A
// message the account sent elsewhere has to come from some device that is not
// the linked one, and on a real account that is the phone.
const selfDevice = 0

// outboxDepth is how many queued sends a session tolerates before a scenario
// blocks. Scenarios are small; a deep queue would only hide a stuck pump.
const outboxDepth = 64

// postLogin runs everything that happens after <success>: waiting for the
// client's keys, replaying the backlog as an offline sync, then handing the
// connection over to the scenario timeline.
func (s *session) postLogin(ctx context.Context) {
	if err := s.srv.awaitClientKeys(ctx); err != nil {
		s.srv.log.Printf("delivery disabled: %v", err)
		return
	}

	// The app state key goes out before anything is queued behind it. The
	// daemon asks for app state as soon as it is connected, and a key that
	// arrives after that costs a failed fetch and a re-sync.
	if err := s.sendAppStateKey(ctx); err != nil {
		s.srv.log.Printf("app state key: %v", err)
	}

	go s.pumpOutbox(ctx)
	s.srv.setLive(s)

	world := s.srv.world
	if world == nil {
		return
	}
	// Groups have to exist before their messages arrive, or the first message
	// creates a chat named after its own id.
	s.announceGroups()

	// History next: it is what the account already had, and it carries the
	// contact names everything else is displayed under.
	s.sendHistorySync(ctx)

	backlog := world.takeBacklog()
	if len(backlog) > 0 {
		s.enqueue(func(ctx context.Context) error {
			return s.sendNode(ctx, waBinary.Node{Tag: "ib", Content: []waBinary.Node{{
				Tag: "offline_preview",
				Attrs: waBinary.Attrs{
					"count":   fmt.Sprintf("%d", len(backlog)),
					"message": fmt.Sprintf("%d", len(backlog)),
				},
			}}})
		})
		for _, msg := range backlog {
			s.enqueueMessage(msg, true)
		}
		s.enqueue(func(ctx context.Context) error {
			return s.sendNode(ctx, waBinary.Node{Tag: "ib", Content: []waBinary.Node{{
				Tag:   "offline",
				Attrs: waBinary.Attrs{"count": fmt.Sprintf("%d", len(backlog))},
			}}})
		})
	}
	go world.runTimeline(ctx)
}

// awaitClientKeys blocks until the client has uploaded the identity and signed
// prekey an X3DH needs. Nothing can be encrypted to it before that.
func (s *Server) awaitClientKeys(ctx context.Context) error {
	select {
	case <-s.keysReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(keysWait):
		return errNoClientKeys
	}
}

func (s *Server) setLive(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.liveSession = sess
}

func (s *Server) clearLive(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.liveSession == sess {
		s.liveSession = nil
	}
}

// deliverLive sends a message a scenario produced after the frontend was
// already connected.
func (s *Server) deliverLive(m *Msg) {
	sess := s.live()
	if sess == nil {
		s.log.Printf("dropping %q: nothing is connected", m.Text)
		return
	}
	sess.enqueueMessage(m, false)
}

// enqueue hands work to the session's single writer. Everything the server
// originates goes through here so that stanzas reach the client in the order
// the scenario produced them. It blocks when the queue is full rather than
// dropping: a scenario that loses a message halfway through is a mock nobody
// can trust.
func (s *session) enqueue(fn func(context.Context) error) {
	select {
	case s.outbox <- fn:
	case <-s.done:
	}
}

func (s *session) enqueueMessage(m *Msg, offline bool) {
	s.enqueue(func(ctx context.Context) error { return s.sendMessage(ctx, m, offline) })
}

func (s *session) pumpOutbox(ctx context.Context) {
	for {
		select {
		case fn := <-s.outbox:
			if err := fn(ctx); err != nil {
				s.srv.log.Printf("deliver: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// sendMessage encrypts one world message to the connected device and puts it on
// the wire as the <message> stanza whatsmeow expects.
func (s *session) sendMessage(ctx context.Context, m *Msg, offline bool) error {
	plaintext, err := proto.Marshal(&waE2E.Message{Conversation: proto.String(m.Text)})
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if m.Chat.IsGroup && s.srv.world.markAnnounced(m.Chat) {
		// A group a scenario created at runtime still has to exist before its
		// first message lands.
		if err := s.sendGroupCreate(ctx, m.Chat); err != nil {
			return err
		}
	}

	sender := m.From.JID
	if m.FromMe {
		sender.Device = selfDevice
	}
	p, err := s.srv.peerFor(sender, s.srv.encryptionJID(sender))
	if err != nil {
		return fmt.Errorf("peer %s: %w", sender, err)
	}
	enc, err := p.encrypt(ctx, s.srv, s.jid, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt for %s: %w", sender, err)
	}

	attrs := waBinary.Attrs{
		"id":   m.ID,
		"t":    fmt.Sprintf("%d", m.At.Unix()),
		"type": "text",
	}
	switch {
	case m.Chat.IsGroup:
		attrs["from"] = m.Chat.JID
		attrs["participant"] = sender
		// participant_lid is how the client learns which address to decrypt
		// under before it has ever asked who this person is. Without it the
		// first message from somebody new fails, and only the second works.
		attrs["participant_lid"] = lidFor(sender)
	case m.FromMe:
		// A message the account sent from another device arrives addressed
		// from the account, with the chat named as the recipient.
		attrs["from"] = sender
		attrs["recipient"] = m.Chat.JID
		attrs["peer_recipient_lid"] = lidFor(m.Chat.JID)
	default:
		attrs["from"] = sender
		attrs["sender_lid"] = lidFor(sender)
	}
	if !m.FromMe && m.From.Name != "" {
		// notify is how an unsaved contact gets a name. It is the only contact
		// detail a message stanza carries, and the daemon stores it.
		attrs["notify"] = m.From.Name
	}
	if offline {
		attrs["offline"] = "1"
	}

	return s.sendNode(ctx, waBinary.Node{
		Tag:     "message",
		Attrs:   attrs,
		Content: []waBinary.Node{enc},
	})
}

// encryptionJID is the address the client will decrypt a sender under, which is
// always the LID. Modern whatsmeow is LID-first: it rewrites the destination of
// every direct message to the LID, and it decrypts under one whenever its store
// knows the mapping. The mock hands those mappings out on every stanza, so
// there is never a window where the two sides disagree.
func (s *Server) encryptionJID(sender types.JID) types.JID {
	return lidFor(sender)
}
