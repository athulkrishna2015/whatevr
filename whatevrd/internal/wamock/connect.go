//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// onConnected decides what the client asked for. A payload carrying
// DevicePairingData is a fresh device wanting a QR; anything else is a device
// that already has an identity and just wants to log in.
func (s *session) onConnected(ctx context.Context) error {
	if s.payload.GetDevicePairingData() != nil {
		return s.startPairing(ctx)
	}
	user := s.payload.GetUsername()
	if user == 0 {
		return fmt.Errorf("client payload has neither pairing data nor a username")
	}
	s.jid = types.JID{
		User:   fmt.Sprintf("%d", user),
		Device: uint16(s.payload.GetDevice()),
		Server: types.DefaultUserServer,
	}
	s.lid = lidFor(s.jid)
	if err := s.sendSuccess(ctx); err != nil {
		return err
	}
	// Everything the world has to say happens off the read loop: the client
	// still has to upload its prekeys, and it cannot do that while we block.
	go s.postLogin(ctx)
	return nil
}

// sendSuccess is the node that flips whatsmeow's isLoggedIn. It is ignored
// unless the client is already paired, which is why the pairing path sends it
// only after the device identity has been accepted.
func (s *session) sendSuccess(ctx context.Context) error {
	return s.sendNode(ctx, waBinary.Node{
		Tag: "success",
		Attrs: waBinary.Attrs{
			"lid": s.lid,
			"t":   fmt.Sprintf("%d", time.Now().Unix()),
		},
	})
}

// lidFor derives the hidden-user address for a phone number. WhatsApp's real
// mapping is opaque; the mock only needs it to be stable and one to one.
func lidFor(jid types.JID) types.JID {
	return types.JID{User: jid.User, Device: jid.Device, Server: types.HiddenUserServer}
}

func (s *session) handleNode(ctx context.Context, node *waBinary.Node) error {
	switch node.Tag {
	case "iq":
		return s.handleIQ(ctx, node)
	case "ack":
		// The client acking something we sent. Nothing to do yet.
		return nil
	case "ib":
		// Info blob the client volunteers about itself. Nothing to answer.
		return nil
	case "message":
		return s.handleClientMessage(ctx, node)
	case "presence":
		return s.handleClientPresence(ctx, node)
	case "chatstate":
		// The account typing. Nothing in the world watches, and a real server
		// does not ack these.
		return nil
	case "receipt":
		return s.handleClientReceipt(ctx, node)
	case "notification", "call":
		// Acking keeps the client from retrying something the mock has no
		// opinion about.
		return s.ackStanza(ctx, node)
	default:
		s.srv.log.Printf("unhandled client node <%s>", node.Tag)
		return nil
	}
}

// ackStanza answers a client stanza with the <ack> it is waiting on. The
// response waiter matches on id alone, so this also releases any send that
// parked on this stanza.
func (s *session) ackStanza(ctx context.Context, node *waBinary.Node) error {
	ag := node.AttrGetter()
	attrs := waBinary.Attrs{
		"id":    ag.String("id"),
		"class": node.Tag,
		// The send path reads the sent timestamp straight off the ack. Without
		// it every message the account sends is stored as sent in 1970.
		"t": fmt.Sprintf("%d", time.Now().Unix()),
	}
	if to := ag.OptionalJIDOrEmpty("to"); !to.IsEmpty() {
		attrs["from"] = to
	} else {
		attrs["from"] = types.ServerJID
	}
	if participant := ag.OptionalJIDOrEmpty("participant"); !participant.IsEmpty() {
		attrs["participant"] = participant
	}
	return s.sendNode(ctx, waBinary.Node{Tag: "ack", Attrs: attrs})
}
