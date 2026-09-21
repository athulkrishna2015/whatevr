package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/util/random"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	"google.golang.org/protobuf/proto"
)

// Message-secret encryption for event responses.
//
// THIS FILE IS OURS TO MAINTAIN. whatsmeow implements this scheme once and
// exposes it for reactions, comments, poll votes and poll options, but the
// event-response pair is not among the exported wrappers and the derivation
// itself (`generateMsgSecretKey`, `decryptMsgSecret`) is unexported. So the
// derivation is reproduced here, over the same exported primitives whatsmeow
// builds it from: `hkdfutil.SHA256`, `gcmutil`, and its own
// `whatsmeow.EncSecretEventResponse` constant.
//
// Taking the use-case string from whatsmeow rather than writing "Event
// Response" out again is deliberate: if upstream ever renames it, this stops
// compiling instead of silently deriving a key nobody else can read.
//
// The scheme, for anyone auditing it against upstream:
//
//	key  = HKDF-SHA256(secret, salt=nil, info=origMsgID ‖ origSender ‖ responder ‖ "Event Response", 32)
//	data = origMsgID ‖ 0x00 ‖ responder
//	out  = AES-256-GCM(key, iv, plaintext, data)
//
// where both jids are their non-AD string forms. The known-vector test in
// msgsecret_event_test.go pins every one of those joins.

// eventResponseSecretKey derives the key and additional data for one response.
func eventResponseSecretKey(responder types.JID, origMsgID types.MessageID, origSender types.JID, origSecret []byte) (key, additionalData []byte) {
	origSenderStr := origSender.ToNonAD().String()
	responderStr := responder.ToNonAD().String()
	useCase := string(whatsmeow.EncSecretEventResponse)

	info := make([]byte, 0, len(origMsgID)+len(origSenderStr)+len(responderStr)+len(useCase))
	info = append(info, origMsgID...)
	info = append(info, origSenderStr...)
	info = append(info, responderStr...)
	info = append(info, useCase...)

	return hkdfutil.SHA256(origSecret, nil, info, 32),
		fmt.Appendf(nil, "%s\x00%s", origMsgID, responderStr)
}

// eventOrigSenderFromKey works out who created the event a response points at.
// It mirrors whatsmeow's unexported getOrigSenderFromKey, because the answer
// is part of the key derivation and getting it wrong fails authentication with
// no clue as to why.
func eventOrigSenderFromKey(msg *events.Message, key *waCommon.MessageKey) (types.JID, error) {
	if key.GetFromMe() {
		// fromMe means the event and the response came from the same account.
		return msg.Info.Sender, nil
	}
	if msg.Info.Chat.Server == types.DefaultUserServer || msg.Info.Chat.Server == types.HiddenUserServer {
		sender, err := types.ParseJID(key.GetRemoteJID())
		if err != nil {
			return types.EmptyJID, fmt.Errorf("parse remote jid %q of the event's sender: %w", key.GetRemoteJID(), err)
		}
		return sender, nil
	}
	sender, err := types.ParseJID(key.GetParticipant())
	if err != nil {
		return types.EmptyJID, fmt.Errorf("parse participant %q of the event's sender: %w", key.GetParticipant(), err)
	}
	if sender.Server != types.DefaultUserServer && sender.Server != types.HiddenUserServer {
		return types.EmptyJID, fmt.Errorf("the event's sender %s is on an unexpected server", sender)
	}
	return sender, nil
}

// decryptEventResponse opens somebody's RSVP.
func (c *Client) decryptEventResponse(ctx context.Context, evt *events.Message) (*waE2E.EventResponseMessage, error) {
	enc := evt.Message.GetEncEventResponseMessage()
	if enc == nil {
		return nil, fmt.Errorf("message carries no event response")
	}
	client := c.currentClient()
	if client == nil || client.Store == nil || client.Store.MsgSecrets == nil {
		return nil, fmt.Errorf("not connected")
	}

	origKey := enc.GetEventCreationMessageKey()
	origSender, err := eventOrigSenderFromKey(evt, origKey)
	if err != nil {
		return nil, err
	}
	secret, storedSender, err := client.Store.MsgSecrets.GetMessageSecret(ctx, evt.Info.Chat, origSender, origKey.GetID())
	if err != nil {
		return nil, fmt.Errorf("get the event's message secret: %w", err)
	}
	if secret == nil {
		return nil, whatsmeow.ErrOriginalMessageSecretNotFound
	}

	key, additionalData := eventResponseSecretKey(evt.Info.Sender, origKey.GetID(), origSender, secret)
	plaintext, err := gcmutil.Decrypt(key, enc.GetEncIV(), enc.GetEncPayload(), additionalData)
	if err != nil {
		// The same fallback whatsmeow keeps for its own secret types: the jid
		// the response names as the event's sender and the jid we filed the
		// secret under can disagree while WhatsApp is still moving accounts to
		// LIDs, and only one of them derives the key that works.
		if origSender == storedSender || !strings.Contains(err.Error(), "message authentication failed") {
			return nil, fmt.Errorf("decrypt event response: %w", err)
		}
		key, additionalData = eventResponseSecretKey(evt.Info.Sender, origKey.GetID(), storedSender, secret)
		plaintext, err = gcmutil.Decrypt(key, enc.GetEncIV(), enc.GetEncPayload(), additionalData)
		if err != nil {
			return nil, fmt.Errorf("decrypt event response: %w", err)
		}
	}

	var response waE2E.EventResponseMessage
	if err := proto.Unmarshal(plaintext, &response); err != nil {
		return nil, fmt.Errorf("parse event response: %w", err)
	}
	return &response, nil
}

// encryptEventResponse seals an RSVP for the event named by eventInfo.
//
// The responder is a parameter rather than always the local account, because
// the key is derived from who is answering and the ciphertext is only readable
// by somebody deriving it from the same person. Assuming "us" here made the
// encrypt and decrypt halves silently disagree the moment anything sealed a
// response on behalf of another jid, and the only symptom was an
// authentication failure with nothing to say why.
func (c *Client) encryptEventResponse(ctx context.Context, eventInfo *types.MessageInfo, responder types.JID, response *waE2E.EventResponseMessage) (*waE2E.Message, error) {
	client := c.currentClient()
	if client == nil || client.Store == nil || client.Store.MsgSecrets == nil {
		return nil, fmt.Errorf("not connected")
	}
	if responder.IsEmpty() {
		return nil, whatsmeow.ErrNotLoggedIn
	}

	plaintext, err := proto.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode event response: %w", err)
	}

	secret, origSender, err := client.Store.MsgSecrets.GetMessageSecret(ctx, eventInfo.Chat, eventInfo.Sender, eventInfo.ID)
	if err != nil {
		return nil, fmt.Errorf("get the event's message secret: %w", err)
	}
	if secret == nil {
		return nil, whatsmeow.ErrOriginalMessageSecretNotFound
	}

	key, additionalData := eventResponseSecretKey(responder.ToNonAD(), eventInfo.ID, origSender, secret)
	iv := random.Bytes(12)
	ciphertext, err := gcmutil.Encrypt(key, iv, plaintext, additionalData)
	if err != nil {
		return nil, fmt.Errorf("encrypt event response: %w", err)
	}

	return &waE2E.Message{EncEventResponseMessage: &waE2E.EncEventResponseMessage{
		EventCreationMessageKey: client.BuildMessageKey(eventInfo.Chat, eventInfo.Sender, eventInfo.ID),
		EncPayload:              ciphertext,
		EncIV:                   iv,
	}}, nil
}
