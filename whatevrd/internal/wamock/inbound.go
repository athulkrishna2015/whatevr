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

// handleClientMessage takes a message the account sent from a frontend, opens
// it, and puts it in the world. The ack goes first: whatsmeow parks the send
// until it arrives, and everything after this is the mock's own business.
func (s *session) handleClientMessage(ctx context.Context, node *waBinary.Node) error {
	if err := s.ackStanza(ctx, node); err != nil {
		return err
	}
	ag := node.AttrGetter()
	id := ag.String("id")
	to := ag.OptionalJIDOrEmpty("to")
	if id == "" || to.IsEmpty() {
		return fmt.Errorf("message with no id or recipient")
	}

	message, err := s.openClientMessage(ctx, node, to)
	if err != nil {
		s.srv.log.Printf("could not open outgoing message %s: %v", id, err)
		return nil
	}
	s.srv.noteClientMessage(id, to, message)
	return nil
}

// openClientMessage decrypts whichever copy of the message the mock can read.
// A direct message is encrypted once per device, so any peer that is not the
// account will do; a group message is one skmsg for everyone, with the sender
// key handed out in the per-device copies.
func (s *session) openClientMessage(ctx context.Context, node *waBinary.Node, to types.JID) (*waE2E.Message, error) {
	participants, _ := node.GetOptionalChildByTag("participants")
	var (
		firstErr error
		groupKey *peer
	)
	for _, child := range participants.GetChildren() {
		if child.Tag != "to" {
			continue
		}
		enc, ok := child.GetOptionalChildByTag("enc")
		if !ok {
			continue
		}
		target := child.AttrGetter().OptionalJIDOrEmpty("jid")
		p, err := s.srv.peerForRecipient(target)
		if err != nil {
			continue
		}
		// For a direct message the account's own other device gets a copy too,
		// and that copy is the device-sent wrapper rather than the message.
		if to.Server != types.GroupServer && to.Server != types.BroadcastServer && p.jid.User == s.srv.opts.AccountPhone && to.User != s.srv.opts.AccountPhone {
			continue
		}
		content, ok := enc.Content.([]byte)
		if !ok {
			continue
		}
		plaintext, err := p.decryptDM(ctx, s.jid, enc.AttrGetter().OptionalString("type") == "pkmsg", content)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		var message waE2E.Message
		if err := proto.Unmarshal(plaintext, &message); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if to.Server != types.GroupServer && to.Server != types.BroadcastServer {
			return unwrapDeviceSent(&message), nil
		}
		skdm := message.GetSenderKeyDistributionMessage()
		if skdm == nil {
			continue
		}
		if err := p.processSKDM(ctx, to, s.lid, skdm.GetAxolotlSenderKeyDistributionMessage()); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		groupKey = p
		break
	}

	if groupKey == nil {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("no copy of the message was addressed to anybody the mock speaks for")
	}
	for _, child := range node.GetChildren() {
		if child.Tag != "enc" || child.AttrGetter().OptionalString("type") != "skmsg" {
			continue
		}
		content, ok := child.Content.([]byte)
		if !ok {
			continue
		}
		plaintext, err := groupKey.decryptGroup(ctx, to, s.lid, content)
		if err != nil {
			return nil, err
		}
		var message waE2E.Message
		if err := proto.Unmarshal(plaintext, &message); err != nil {
			return nil, err
		}
		return &message, nil
	}
	return nil, fmt.Errorf("group message carried no skmsg")
}

// unwrapDeviceSent peels the wrapper a client puts around its own copy of a
// message so that its other devices see who it was for.
func unwrapDeviceSent(message *waE2E.Message) *waE2E.Message {
	if dsm := message.GetDeviceSentMessage(); dsm != nil && dsm.GetMessage() != nil {
		return dsm.GetMessage()
	}
	return message
}

// messageText is as much of a message as the mock models. Anything else lands
// in the world as an empty string, which reads as "something was sent" rather
// than pretending to understand it.
func messageText(message *waE2E.Message) string {
	if text := message.GetConversation(); text != "" {
		return text
	}
	return message.GetExtendedTextMessage().GetText()
}

// noteClientMessage puts a sent message in the world, pages the scenario's
// OnSend hooks, and starts the receipt clock.
func (s *Server) noteClientMessage(id string, to types.JID, message *waE2E.Message) {
	world := s.world
	if world == nil {
		return
	}
	if message.GetProtocolMessage() != nil || message.GetReactionMessage() != nil {
		// Edits, revokes and reactions are lifecycle traffic rather than
		// conversation. Stage 2 delivers them as acks and nothing else.
		return
	}
	if to.Server == types.HiddenUserServer {
		to.Server = types.DefaultUserServer
	}
	chat, ok := world.chatByJID(to)
	if !ok {
		contact, known := world.contactByJID(to)
		if !known {
			s.log.Printf("message to %s, who is not in this world", to)
			return
		}
		chat = world.DM(contact)
	}
	msg := &Msg{
		ID:     id,
		Chat:   chat,
		From:   world.Self(),
		Text:   messageText(message),
		At:     time.Now(),
		FromMe: true,
	}
	go s.runSendHooks(msg)
}

// runSendHooks paces the receipts the account's own message gets, then lets the
// scenario answer. Both happen off the read loop so a hook that talks back
// cannot deadlock against the stanza it is answering.
func (s *Server) runSendHooks(msg *Msg) {
	delivery, read := s.world.receiptDelays()
	if delivery > 0 {
		time.Sleep(delivery)
		s.sendReceipt(msg, "")
	}
	if read > 0 {
		time.Sleep(max(read-delivery, 0))
		s.sendReceipt(msg, "read")
	}
	for _, hook := range s.world.sendHooks() {
		hook(msg)
	}
}

// handleClientReceipt takes the account's own read receipts. The mock has
// nothing to tell anybody about them, but the client waits on the ack.
func (s *session) handleClientReceipt(ctx context.Context, node *waBinary.Node) error {
	return s.ackStanza(ctx, node)
}
