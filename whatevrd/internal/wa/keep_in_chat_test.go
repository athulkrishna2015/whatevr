package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"google.golang.org/protobuf/proto"

	appstore "whatevrd/internal/store"
)

// keepTarget stores one ordinary message for a keep to name, and yields its
// internal id.
func keepTarget(t *testing.T, client *Client, id string) string {
	t.Helper()
	evt := mediaIngestEvent(id, &waE2E.Message{Conversation: proto.String("gone in a day")})
	chatID, internalID := client.internalMessageIDFromInfo(context.Background(), evt.Info)
	saved, err := client.store.SaveTextMessage(context.Background(), appstore.TextMessageInput{
		ID:        internalID,
		ChatID:    chatID,
		SenderID:  evt.Info.Sender.String(),
		Text:      "gone in a day",
		Timestamp: evt.Info.Timestamp,
		Direction: "in",
		Status:    "delivered",
	})
	if err != nil {
		t.Fatalf("save target: %v", err)
	}
	return saved.Message.ID
}

// keepEvent is the control message a phone sends to keep or un-keep a message.
func keepEvent(target string, keepType waE2E.KeepType) *waE2E.Message {
	return &waE2E.Message{
		KeepInChatMessage: &waE2E.KeepInChatMessage{
			Key:         &waCommon.MessageKey{ID: proto.String(target)},
			KeepType:    keepType.Enum(),
			TimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	}
}

func keptFlag(t *testing.T, client *Client, internalID string) bool {
	t.Helper()
	message, err := client.store.GetMessage(context.Background(), internalID)
	if err != nil {
		t.Fatalf("get %s: %v", internalID, err)
	}
	return message.IsKept
}

// A keep is not something anyone said, so it marks the message it names and
// never becomes a row of its own.
func TestKeepInChatMarksItsTargetAndIsNotARow(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	internalID := keepTarget(t, client, "keep1")

	if !client.handleKeepInChat(ctx, mediaIngestEvent("k1", keepEvent("keep1", waE2E.KeepType_KEEP_FOR_ALL)), false) {
		t.Fatal("a keep must be swallowed, not ingested as a message")
	}
	if !keptFlag(t, client, internalID) {
		t.Fatal("the kept message was not flagged")
	}

	if !client.handleKeepInChat(ctx, mediaIngestEvent("k2", keepEvent("keep1", waE2E.KeepType_UNDO_KEEP_FOR_ALL)), false) {
		t.Fatal("an undo-keep must be swallowed too")
	}
	if keptFlag(t, client, internalID) {
		t.Fatal("the undo did not clear the flag")
	}
}

// A keep type we do not understand is still a control message: swallowing it is
// right, and acting on it as an un-keep would silently drop a real keep.
func TestUnknownKeepTypeIsSwallowedButChangesNothing(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	internalID := keepTarget(t, client, "keep2")

	if !client.handleKeepInChat(ctx, mediaIngestEvent("k3", keepEvent("keep2", waE2E.KeepType_KEEP_FOR_ALL)), false) {
		t.Fatal("a keep must be swallowed")
	}
	if !client.handleKeepInChat(ctx, mediaIngestEvent("k4", keepEvent("keep2", waE2E.KeepType_UNKNOWN_KEEP_TYPE)), false) {
		t.Fatal("an unknown keep type is still not a message anyone sent")
	}
	if !keptFlag(t, client, internalID) {
		t.Fatal("an unknown keep type cleared a keep that was really asked for")
	}
}

// The keep can arrive for a message we never synced, which is ordinary rather
// than an error: it must be swallowed all the same, and quietly.
func TestKeepForAnUnknownMessageIsHarmless(t *testing.T) {
	client := newMediaIngestClient(t)

	if !client.handleKeepInChat(context.Background(), mediaIngestEvent("k5", keepEvent("never-arrived", waE2E.KeepType_KEEP_FOR_ALL)), false) {
		t.Fatal("a keep naming a message we do not have is still a keep")
	}
}

// Anything that is not a keep must fall through to the ingest untouched.
func TestNonKeepMessagesFallThrough(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()

	if client.handleKeepInChat(ctx, mediaIngestEvent("t1", &waE2E.Message{Conversation: proto.String("hello")}), false) {
		t.Fatal("a text message was swallowed as a keep")
	}
	if client.handleKeepInChat(ctx, mediaIngestEvent("t2", &waE2E.Message{
		KeepInChatMessage: &waE2E.KeepInChatMessage{KeepType: waE2E.KeepType_KEEP_FOR_ALL.Enum()},
	}), false) {
		t.Fatal("a keep naming nothing at all must not be treated as one")
	}
}

// History sync never replays the control message: the phone folds its outcome
// into the kept message itself, so the flag has to be read off the backfilled
// message or a keep made before we linked is lost.
func TestHistoryKeepStateReadsTheBackfilledFlag(t *testing.T) {
	cases := []struct {
		name  string
		web   *waWeb.WebMessageInfo
		kept  bool
		known bool
	}{
		{name: "absent", web: &waWeb.WebMessageInfo{}},
		{
			name:  "kept",
			web:   &waWeb.WebMessageInfo{KeepInChat: &waWeb.KeepInChat{KeepType: waE2E.KeepType_KEEP_FOR_ALL.Enum()}},
			kept:  true,
			known: true,
		},
		{
			name:  "undone",
			web:   &waWeb.WebMessageInfo{KeepInChat: &waWeb.KeepInChat{KeepType: waE2E.KeepType_UNDO_KEEP_FOR_ALL.Enum()}},
			known: true,
		},
		{
			name: "unknown type",
			web:  &waWeb.WebMessageInfo{KeepInChat: &waWeb.KeepInChat{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, known := historyKeepState(tc.web)
			if kept != tc.kept || known != tc.known {
				t.Fatalf("kept, known = %v, %v; want %v, %v", kept, known, tc.kept, tc.known)
			}
		})
	}
}

// The wire carries the flag so a frontend can draw the bookmark. Nothing else
// about the row changes: a kept message is still whatever it was.
func TestKeptRidesTheWire(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	internalID := keepTarget(t, client, "keep3")

	client.handleKeepInChat(ctx, mediaIngestEvent("k6", keepEvent("keep3", waE2E.KeepType_KEEP_FOR_ALL)), false)

	message, err := client.store.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if !message.IsKept {
		t.Fatal("the store did not keep the flag")
	}
	if message.Text != "gone in a day" {
		t.Fatalf("text = %q: keeping a message must not change what it says", message.Text)
	}
}
