package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

func undecryptableEvent(id string) *events.UndecryptableMessage {
	return &events.UndecryptableMessage{
		Info: types.MessageInfo{
			ID: types.MessageID(id),
			MessageSource: types.MessageSource{
				Chat:   types.JID{User: "5551234", Server: types.DefaultUserServer},
				Sender: types.JID{User: "5551234", Server: types.DefaultUserServer},
			},
			Timestamp: time.Unix(1_700_000_000, 0),
		},
	}
}

// The hole gets a row. Without one the transcript is indistinguishable from
// nobody having said anything, which is the bug this phase exists to fix.
func TestUndecryptableMessageLeavesAWaitingRow(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	evt := undecryptableEvent("u1")

	client.writeWaitingRow(ctx, evt)

	_, internalID := client.internalMessageIDFromInfo(ctx, evt.Info)
	message, err := client.store.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if message.MediaKind != appstore.MediaKindWaiting {
		t.Fatalf("kind = %q", message.MediaKind)
	}
	waiting := appstore.DecodePayload(message.PayloadJSON).Waiting
	if waiting == nil {
		t.Fatal("no waiting payload was stored")
	}
	if waiting.Requests != 1 {
		t.Fatalf("requests = %d, want 1", waiting.Requests)
	}
	if waiting.RetryAt <= time.Now().Unix() {
		t.Fatalf("retry_at = %d, want a moment in the future", waiting.RetryAt)
	}
	if waiting.FirstSeen != evt.Info.Timestamp.Unix() {
		t.Fatalf("first_seen = %d, want the message's own time", waiting.FirstSeen)
	}
	// The row is the message, so it counts and it leads the chat list.
	chat, err := client.store.GetChat(ctx, message.ChatID)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if chat.UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1", chat.UnreadCount)
	}
}

// whatsmeow re-emits the event when a resend fails to decrypt too. That is one
// message asked for twice, not two messages.
func TestRepeatedFailureCountsUpOnOneRow(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	evt := undecryptableEvent("u2")

	client.writeWaitingRow(ctx, evt)
	client.writeWaitingRow(ctx, evt)

	_, internalID := client.internalMessageIDFromInfo(ctx, evt.Info)
	message, err := client.store.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	waiting := appstore.DecodePayload(message.PayloadJSON).Waiting
	if waiting == nil || waiting.Requests != 2 {
		t.Fatalf("waiting = %+v, want two requests on one row", waiting)
	}
	if waiting.FirstSeen != evt.Info.Timestamp.Unix() {
		t.Fatalf("first_seen = %d: a repeat must not reset when the hole appeared", waiting.FirstSeen)
	}
	chat, err := client.store.GetChat(ctx, message.ChatID)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if chat.UnreadCount != 1 {
		t.Fatalf("unread = %d, want one message counted once", chat.UnreadCount)
	}
}

// Two kinds of hole are not holes. A message the server marked hidden was never
// meant to arrive here, and a view-once message is unavailable on purpose:
// promising either is a wait that never ends.
func TestSomeUndecryptableMessagesGetNoRow(t *testing.T) {
	cases := []struct {
		name string
		evt  *events.UndecryptableMessage
	}{
		{
			name: "hidden",
			evt: func() *events.UndecryptableMessage {
				evt := undecryptableEvent("u3")
				evt.DecryptFailMode = events.DecryptFailHide
				return evt
			}(),
		},
		{
			name: "view once",
			evt: func() *events.UndecryptableMessage {
				evt := undecryptableEvent("u4")
				evt.UnavailableType = events.UnavailableTypeViewOnce
				return evt
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newMediaIngestClient(t)
			ctx := context.Background()
			client.writeWaitingRow(ctx, tc.evt)

			_, internalID := client.internalMessageIDFromInfo(ctx, tc.evt.Info)
			if _, err := client.store.GetMessage(ctx, internalID); err == nil {
				t.Fatal("a row was written for a message that is never coming")
			}
		})
	}
}

// If the message is already here, nothing is waiting for it. A late duplicate
// of the failure must not turn a real message back into a hole.
func TestALateFailureDoesNotUnmakeAMessageThatArrived(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	evt := undecryptableEvent("u5")
	chatID, internalID := client.internalMessageIDFromInfo(ctx, evt.Info)

	if _, err := client.store.SaveTextMessage(ctx, appstore.TextMessageInput{
		ID:        internalID,
		ChatID:    chatID,
		SenderID:  evt.Info.Sender.String(),
		Text:      "it got here",
		Timestamp: evt.Info.Timestamp,
		Direction: appstore.DirectionIncoming,
		Status:    appstore.StatusDelivered,
	}); err != nil {
		t.Fatalf("save message: %v", err)
	}

	client.writeWaitingRow(ctx, evt)

	message, err := client.store.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if message.MediaKind == appstore.MediaKindWaiting || message.Text != "it got here" {
		t.Fatalf("the message was replaced by a placeholder: kind=%q text=%q", message.MediaKind, message.Text)
	}
}
