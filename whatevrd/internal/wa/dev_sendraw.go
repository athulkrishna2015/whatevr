package wa

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/app"
)

// Development-only message injection, reachable through the daemon's
// `dev.send_raw` command and only when WHATEVR_DEV_COMMANDS=1. It is not part of
// PROTOCOL.md and never registered otherwise.
//
// Section 1 of feature-gap.md is about *receiving*, and the only way to exercise
// the receive path is for something to send. Waiting on a second handset for
// every kind, every time an ingest changes, is not a development loop. This
// builds the waE2E proto WhatsApp would have delivered and pushes it through the
// real handler, so decode, storage, wire encoding and rendering are all the
// production code.

// RawSendRequest is one injected message.
type RawSendRequest struct {
	ChatID string          `json:"chat_id"`
	Kind   string          `json:"kind"`
	Params json.RawMessage `json:"params"`
	// Local injects the message as if it had arrived from the peer, without
	// touching the network. This is the mode that actually tests the receive
	// path, and it costs the peer nothing.
	Local bool `json:"local"`
	// Incoming decides the direction a real send is ingested under. It has no
	// effect in Local mode, where the message is always incoming.
	Incoming bool `json:"incoming"`
}

// rawMessageBuilder turns the command's free-form params into the proto
// WhatsApp would carry. Each phase that teaches the ingest a new kind registers
// its builder here in the same commit, so there is never a kind the ingest
// understands and the test driver cannot produce.
type rawMessageBuilder func(params json.RawMessage) (*waE2E.Message, error)

var rawMessageBuilders = map[string]rawMessageBuilder{
	"text": buildRawText,
}

// RawSendKinds lists what the driver can produce, for the tool's help output.
func RawSendKinds() []string {
	kinds := make([]string, 0, len(rawMessageBuilders))
	for kind := range rawMessageBuilders {
		kinds = append(kinds, kind)
	}
	return kinds
}

// SendRawMessage builds one message of the named kind and either injects it
// locally or really sends it. It returns the internal message id of the row
// that resulted, so a caller can watch for that exact upsert.
func (c *Client) SendRawMessage(ctx context.Context, req RawSendRequest) (string, error) {
	build, ok := rawMessageBuilders[req.Kind]
	if !ok {
		return "", app.NewCommandError(app.CommandErrorInvalidArgument, "no raw builder for kind %q", req.Kind)
	}
	message, err := build(req.Params)
	if err != nil {
		return "", app.NewCommandError(app.CommandErrorInvalidArgument, "build %s: %v", req.Kind, err)
	}

	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return "", app.NewCommandError(app.CommandErrorNotLoggedIn, "not logged in")
	}
	chatJID, err := types.ParseJID(req.ChatID)
	if err != nil {
		return "", app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}
	chatJID = c.normalizeJIDForChat(ctx, chatJID)

	messageID := client.GenerateMessageID()
	sentAt := time.Now()
	fromMe := !req.Local && !req.Incoming

	if !req.Local {
		resp, err := client.SendMessage(ctx, chatJID, message, whatsmeow.SendRequestExtra{ID: messageID})
		if err != nil {
			return "", app.NewCommandError(app.CommandErrorRejected, "send %s: %v", req.Kind, err)
		}
		if !resp.Timestamp.IsZero() {
			sentAt = resp.Timestamp
		}
	}

	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chatJID,
				Sender:   c.rawSendSender(chatJID, fromMe),
				IsFromMe: fromMe,
				IsGroup:  chatJID.Server == types.GroupServer,
			},
			ID:        messageID,
			Timestamp: sentAt,
			PushName:  "Raw send",
		},
		Message: message,
	}

	c.handleMessage(ctx, evt, false)
	return internalMessageIDForChat(chatJID.String(), messageID), nil
}

// rawSendSender picks a plausible sender for the injected event: ourselves for
// an outgoing copy, otherwise the peer in a direct chat or the chat itself in a
// group, which is what a real participant JID would resolve through anyway.
func (c *Client) rawSendSender(chatJID types.JID, fromMe bool) types.JID {
	if fromMe {
		if client := c.currentClient(); client != nil {
			if own := client.Store.GetJID(); !own.IsEmpty() {
				return own.ToNonAD()
			}
		}
	}
	return chatJID
}

func buildRawText(params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Text string `json:"text"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Text == "" {
		p.Text = "raw send"
	}
	return &waE2E.Message{Conversation: &p.Text}, nil
}

func decodeRawParams(params json.RawMessage, out any) error {
	if len(params) == 0 {
		return nil
	}
	if err := json.Unmarshal(params, out); err != nil {
		return fmt.Errorf("malformed params: %w", err)
	}
	return nil
}
