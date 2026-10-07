package wa

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

func newCancelSendTestClient(t *testing.T) (*Client, *appstore.DB) {
	t.Helper()
	db, err := appstore.Open(context.Background(), filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &Client{store: db, daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}, db
}

// Cancelling a still-pending outgoing message marks it failed so the send
// worker skips it; anything already sent (or anyone else's message) is
// rejected, never rewritten.
func TestCancelPendingSendMarksPendingFailed(t *testing.T) {
	ctx := context.Background()
	client, db := newCancelSendTestClient(t)
	chat := "chat@s.whatsapp.net"
	if _, err := db.SaveTextMessage(ctx, appstore.TextMessageInput{
		ID:        internalMessageIDForChat(chat, types.MessageID("m1")),
		ChatID:    chat,
		SenderID:  "me",
		Text:      "waiting in the queue",
		Timestamp: time.Unix(100, 0),
		Direction: appstore.DirectionOutgoing,
		Status:    appstore.StatusPending,
	}); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	if err := client.CancelPendingSend(ctx, internalMessageIDForChat(chat, types.MessageID("m1"))); err != nil {
		t.Fatalf("cancel pending: %v", err)
	}
	message, err := db.GetMessage(ctx, internalMessageIDForChat(chat, types.MessageID("m1")))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if message.Status != appstore.StatusFailed {
		t.Fatalf("status = %q, want failed", message.Status)
	}

	if err := client.CancelPendingSend(ctx, internalMessageIDForChat(chat, types.MessageID("m1"))); err == nil {
		t.Fatal("cancelling a failed message must be rejected")
	}
	if err := client.CancelPendingSend(ctx, ""); err == nil {
		t.Fatal("cancelling without an id must be rejected")
	}
}
