package wa

import (
	"context"
	"testing"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

func statusIngestEvent(id, sender string, message *waE2E.Message) *events.Message {
	evt := mediaIngestEvent(id, message)
	evt.Info.Chat = types.StatusBroadcastJID
	evt.Info.Sender = types.JID{User: sender, Server: types.DefaultUserServer}
	evt.Info.MessageSource.IsGroup = false
	return evt
}

// TestStatusBroadcastBypassesChats locks in that status traffic lands in the
// status store for text and photo payloads, and that protocol noise is
// ignored. By default statuses are also mirrored into the status@broadcast
// chat (restored pre-tab behavior) without unread; disabling the mirror
// preference keeps them out of chats entirely.
func TestStatusBroadcastBypassesChats(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()

	textEvt := statusIngestEvent("S1", "5551234", &waE2E.Message{
		Conversation: proto.String("morning"),
	})
	if isStatusBroadcast(textEvt) != true {
		t.Fatal("isStatusBroadcast = false for status@broadcast")
	}
	client.handleMessage(ctx, textEvt, false)

	photoEvt := statusIngestEvent("S2", "5551234", &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			DirectPath: proto.String("/enc/photo.enc"),
			FileLength: proto.Uint64(4321),
		},
	})
	client.handleMessage(ctx, photoEvt, false)

	noiseEvt := statusIngestEvent("S3", "5551234", &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{},
	})
	client.handleMessage(ctx, noiseEvt, false)

	statuses, err := client.store.ListStatusUpdates(ctx, 0)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("stored statuses = %d, want 2 (text + photo, noise skipped)", len(statuses))
	}
	if statuses[0].Kind != appstore.MediaKindImage || len(statuses[0].MediaPayload) == 0 {
		t.Fatalf("photo status = %+v, want image kind with keys", statuses[0])
	}
	if statuses[1].Kind != "text" || statuses[1].Text != "morning" {
		t.Fatalf("text status = %+v, want text/morning", statuses[1])
	}

	// Default: mirrored into status@broadcast, silently (no unread bump).
	chat, err := client.store.GetChat(ctx, types.StatusBroadcastJID.String())
	if err != nil {
		t.Fatalf("status@broadcast chat missing with mirror enabled: %v", err)
	}
	if chat.UnreadCount != 0 {
		t.Fatalf("mirrored status bumped unread to %d, want 0", chat.UnreadCount)
	}

	// Mirror disabled: statuses stay out of chats entirely.
	quiet := newMediaIngestClient(t)
	if _, err := quiet.UpdateAppPreferences(ctx, func(prefs *app.AppPreferences) {
		prefs.StatusMirrorToChat = false
	}); err != nil {
		t.Fatalf("disable status mirror: %v", err)
	}
	quiet.handleMessage(ctx, statusIngestEvent("S9", "5551234", &waE2E.Message{
		Conversation: proto.String("hello"),
	}), false)
	if mirrored, err := quiet.store.GetChat(ctx, types.StatusBroadcastJID.String()); err == nil {
		t.Fatalf("status@broadcast materialized as chat %+v with mirror disabled", mirrored)
	}
}
