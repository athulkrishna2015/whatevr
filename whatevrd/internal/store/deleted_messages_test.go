package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Deleting a message removes its row, but the phone still has the message, so
// the next backfill of that conversation carries it again. Without a record of
// the deletion the message comes straight back.
func TestDeletedMessageIsNotResurrectedByBackfill(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	input := TextMessageInput{
		ID:        "chat-1:msg-1",
		ChatID:    "chat-1",
		ChatName:  "Test Chat",
		SenderID:  "sender-1",
		Text:      "regrettable",
		Timestamp: time.Unix(1_700_000_000, 0),
		Direction: DirectionIncoming,
		Status:    StatusDelivered,
	}
	if _, err := db.SaveTextMessage(ctx, input); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, _, existed, err := db.DeleteMessageForMe(ctx, input.ID); err != nil || !existed {
		t.Fatalf("delete: existed=%t err=%v", existed, err)
	}

	// Backfill hands the same message back.
	saved, err := db.SaveTextMessage(ctx, input)
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if saved.Inserted {
		t.Fatal("a deleted message was written back by backfill")
	}

	messages, err := db.ListMessages(ctx, "chat-1", 10, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("chat holds %d rows after deletion, want 0", len(messages))
	}

	// A media re-delivery of the same id must not bring it back either.
	if _, err := db.SaveMediaMessage(ctx, MediaMessageInput{
		TextMessageInput: input,
		MediaKind:        MediaKindImage,
		MediaMimeType:    "image/jpeg",
	}); err != nil {
		t.Fatalf("re-save as media: %v", err)
	}
	messages, err = db.ListMessages(ctx, "chat-1", 10, "")
	if err != nil {
		t.Fatalf("list after media: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("chat holds %d rows after a media re-delivery, want 0", len(messages))
	}
}
