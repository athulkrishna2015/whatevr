package wa

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// A message that would not decrypt.
//
// It happens: a device rotated its keys, a session went stale, a message was
// sent while this device was offline and the sender's client picked the wrong
// key. WhatsApp's own answer is to ask the sender to send it again, and failing
// that to ask your own phone, and whatsmeow implements both.
//
// What was missing was the row. The daemon already recorded the hole in
// `undecryptable_messages` so that a resend would land at its original time,
// but the transcript itself showed nothing at all: a gap that reads exactly
// like nobody having said anything. Now the hole is a row, it says it is
// waiting, and it turns into the real message in place when the resend arrives.

// undecryptableRequestGrace is how long a manual "ask again" waits before the
// row offers the button back. It is deliberately longer than whatsmeow's own
// RequestFromPhoneDelay: a person pressing a button expects the answer to have
// a chance to arrive before they are invited to press it again.
const undecryptableRequestGrace = 20 * time.Second

// waitingRowInput builds the placeholder for one undecryptable message.
func (c *Client) waitingRowInput(ctx context.Context, evt *events.UndecryptableMessage, payload appstore.WaitingPayload) (appstore.MediaMessageInput, bool) {
	chatID, internalID := c.internalMessageIDFromInfo(ctx, evt.Info)
	if chatID == "" || internalID == "" {
		return appstore.MediaMessageInput{}, false
	}
	payloadJSON, err := appstore.EncodePayload(appstore.MessagePayload{Waiting: &payload})
	if err != nil {
		c.log.Warnf("Failed to encode waiting payload for %s: %v", internalID, err)
		return appstore.MediaMessageInput{}, false
	}

	timestamp := evt.Info.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return appstore.MediaMessageInput{
		TextMessageInput: appstore.TextMessageInput{
			ID:          internalID,
			ChatID:      chatID,
			SenderID:    senderID(evt.Info),
			SenderName:  c.senderName(ctx, evt.Info.Sender),
			Timestamp:   timestamp,
			Direction:   appstore.DirectionIncoming,
			Status:      appstore.StatusDelivered,
			IsGroup:     evt.Info.IsGroup,
			CountUnread: !evt.Info.IsFromMe,
			PayloadJSON: payloadJSON,
		},
		MediaKind: appstore.MediaKindWaiting,
	}, true
}

// shouldPlaceholderUndecryptable reports whether a hole deserves a row.
//
// Two kinds do not. A message the server marked hidden was never meant to
// arrive here, and a view-once message is unavailable on purpose: telling
// somebody we are waiting for a message that is never coming would be a lie
// that never resolves.
func shouldPlaceholderUndecryptable(evt *events.UndecryptableMessage) bool {
	if evt.DecryptFailMode == events.DecryptFailHide {
		return false
	}
	return evt.UnavailableType != events.UnavailableTypeViewOnce
}

// writeWaitingRow stores or refreshes the placeholder and publishes it.
func (c *Client) writeWaitingRow(ctx context.Context, evt *events.UndecryptableMessage) {
	if !shouldPlaceholderUndecryptable(evt) {
		return
	}
	_, internalID := c.internalMessageIDFromInfo(ctx, evt.Info)
	if internalID == "" {
		return
	}

	// A repeat of the same failure keeps the original first-seen time and adds
	// to the count: this is one message that has now been asked for twice, not
	// two messages.
	payload := appstore.WaitingPayload{FirstSeen: evt.Info.Timestamp.Unix()}
	if existing, err := c.store.GetMessage(ctx, internalID); err == nil {
		if existing.MediaKind != appstore.MediaKindWaiting {
			// The message is already here. Nothing is waiting for anything.
			return
		}
		if previous := appstore.DecodePayload(existing.PayloadJSON).Waiting; previous != nil {
			payload = *previous
		}
	}
	if payload.FirstSeen == 0 {
		payload.FirstSeen = time.Now().Unix()
	}
	payload.Requests++
	payload.RetryAt = time.Now().Add(whatsmeow.RequestFromPhoneDelay).Unix()

	input, ok := c.waitingRowInput(ctx, evt, payload)
	if !ok {
		return
	}
	saved, err := c.store.SaveMediaMessage(ctx, input)
	if err != nil {
		c.log.Warnf("Failed to store placeholder for undecryptable message %s: %v", internalID, err)
		return
	}
	c.daemon.PublishNewMessage(toDaemonMessage(saved.Message), toDaemonChat(saved.Chat))
}

// RequestMessageFromPhone asks our own phone to send a copy of a message this
// device could not decrypt. It is the manual form of what whatsmeow does by
// itself a few seconds after the failure, and the only thing left to try once
// that has come and gone.
func (c *Client) RequestMessageFromPhone(ctx context.Context, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "message_id is required")
	}
	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}
	if message.MediaKind != appstore.MediaKindWaiting {
		return app.NewCommandError(app.CommandErrorRejected, "this message is not waiting for anything")
	}

	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return app.NewCommandError(app.CommandErrorNotLoggedIn, "WhatsApp client is not logged in")
	}
	chatJID, err := types.ParseJID(message.ChatID)
	if err != nil {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat id %q", message.ChatID)
	}
	senderJID := chatJID
	if message.SenderID != "" && message.SenderID != "me" {
		if parsed, parseErr := types.ParseJID(message.SenderID); parseErr == nil {
			senderJID = parsed
		}
	}

	request := client.BuildUnavailableMessageRequest(chatJID, senderJID,
		appstore.ExternalMessageID(message.ChatID, message.ID))
	if _, err := client.SendPeerMessage(ctx, request); err != nil {
		return app.NewCommandError(app.CommandErrorRejected, "could not ask your phone for this message: %v", err)
	}

	// The row says what was done, so the wait can show a request in flight
	// rather than an unchanged button somebody presses again in a second.
	payload := appstore.WaitingPayload{FirstSeen: message.TimestampUnix}
	if previous := appstore.DecodePayload(message.PayloadJSON).Waiting; previous != nil {
		payload = *previous
	}
	payload.Requests++
	payload.Asked = true
	payload.RetryAt = time.Now().Add(undecryptableRequestGrace).Unix()
	payloadJSON, err := appstore.EncodePayload(appstore.MessagePayload{Waiting: &payload})
	if err != nil {
		return err
	}
	updated, err := c.store.UpdateMessagePayload(ctx, message.ID, payloadJSON, "")
	if err != nil {
		// The request went out, which is the part that matters. Failing the
		// command because the bookkeeping did not stick would invite a second
		// request nobody needs.
		c.log.Warnf("Failed to record the phone request for %s: %v", message.ID, err)
		return nil
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	return nil
}
