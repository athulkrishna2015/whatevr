package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// waitingPlaceholder stores the row the daemon writes for a message that
// arrived and would not decrypt.
func waitingPlaceholder(t *testing.T, db *DB, id string, at time.Time) SavedTextMessage {
	t.Helper()
	saved, err := db.SaveMediaMessage(context.Background(), MediaMessageInput{
		TextMessageInput: TextMessageInput{
			ID:          id,
			ChatID:      "chat-1",
			SenderID:    "sender-1",
			Timestamp:   at,
			Direction:   DirectionIncoming,
			Status:      StatusDelivered,
			CountUnread: true,
		},
		MediaKind: MediaKindWaiting,
	})
	if err != nil {
		t.Fatalf("save placeholder: %v", err)
	}
	if !saved.Inserted {
		t.Fatal("the placeholder was not inserted")
	}
	return saved
}

// The resent message carries the same id as the placeholder standing in for it,
// so the ordinary dedup-by-id rule would drop the real message and leave the
// placeholder forever. This is the one place that rule yields.
func TestResentMessageReplacesItsPlaceholderInPlace(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	placeholder := waitingPlaceholder(t, db, "chat-1:msg-1", time.Unix(100, 0))
	if placeholder.Chat.UnreadCount != 1 {
		t.Fatalf("unread = %d, want the placeholder to count", placeholder.Chat.UnreadCount)
	}

	// The resend arrives later but is the same message, which is why the ingest
	// hands back the original timestamp.
	saved, err := db.SaveTextMessage(ctx, TextMessageInput{
		ID:          "chat-1:msg-1",
		ChatID:      "chat-1",
		SenderID:    "sender-1",
		Text:        "here it is",
		Timestamp:   time.Unix(100, 0),
		Direction:   DirectionIncoming,
		Status:      StatusDelivered,
		CountUnread: true,
	})
	if err != nil {
		t.Fatalf("save resend: %v", err)
	}
	if !saved.Inserted {
		t.Fatal("the resend was dropped as a duplicate of the hole it fills")
	}
	if saved.Message.Text != "here it is" {
		t.Fatalf("text = %q", saved.Message.Text)
	}
	if saved.Message.MediaKind != "" {
		t.Fatalf("kind = %q, want the placeholder's kind gone", saved.Message.MediaKind)
	}
	// One row, not two: the placeholder became the message.
	messages, err := db.ListMessages(ctx, "chat-1", 50, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("got %d rows, want 1", len(messages))
	}
	// The stored sort key is the transcript's order. A placeholder that vanished
	// and came back would land after everything that arrived while it waited.
	if saved.Message.SortMS != placeholder.Message.SortMS {
		t.Fatalf("sort key moved from %d to %d", placeholder.Message.SortMS, saved.Message.SortMS)
	}
	// The placeholder already counted it. Counting again would say two messages
	// arrived when one did.
	if saved.Chat.UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1", saved.Chat.UnreadCount)
	}
	if saved.Chat.LastMessage != "here it is" {
		t.Fatalf("preview = %q, want the message rather than the wait", saved.Chat.LastMessage)
	}
}

// A resend that arrives after the reader already looked at the placeholder must
// not come back unread: the chat's count was settled when the hole appeared.
func TestResendKeepsAPlaceholderThatWasAlreadyRead(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	waitingPlaceholder(t, db, "chat-1:msg-1", time.Unix(100, 0))
	if _, _, err := db.MarkChatReadUpTo(ctx, "chat-1", 200); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	saved, err := db.SaveTextMessage(ctx, TextMessageInput{
		ID:          "chat-1:msg-1",
		ChatID:      "chat-1",
		SenderID:    "sender-1",
		Text:        "here it is",
		Timestamp:   time.Unix(100, 0),
		Direction:   DirectionIncoming,
		Status:      StatusDelivered,
		CountUnread: true,
	})
	if err != nil {
		t.Fatalf("save resend: %v", err)
	}
	if !saved.Message.IsRead {
		t.Fatal("a message somebody already looked at came back unread")
	}
	if saved.Chat.UnreadCount != 0 {
		t.Fatalf("unread = %d, want 0", saved.Chat.UnreadCount)
	}
}

// The exception is exactly one kind wide. Two deliveries of an ordinary message
// are still one row, and the second one changes nothing.
func TestAnOrdinaryDuplicateIsStillDropped(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	input := TextMessageInput{
		ID:          "chat-1:msg-1",
		ChatID:      "chat-1",
		SenderID:    "sender-1",
		Text:        "hello",
		Timestamp:   time.Unix(100, 0),
		Direction:   DirectionIncoming,
		Status:      StatusDelivered,
		CountUnread: true,
	}
	if _, err := db.SaveTextMessage(ctx, input); err != nil {
		t.Fatalf("save: %v", err)
	}
	input.Text = "hello again"
	second, err := db.SaveTextMessage(ctx, input)
	if err != nil {
		t.Fatalf("save again: %v", err)
	}
	if second.Inserted {
		t.Fatal("a duplicate message was inserted")
	}
	if second.Message.Text != "hello" {
		t.Fatalf("text = %q, a duplicate must not rewrite the message", second.Message.Text)
	}
	if second.Chat.UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1", second.Chat.UnreadCount)
	}
}

// A media message fills its own hole too, and brings everything a media row
// carries with it.
func TestResentMediaReplacesItsPlaceholder(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	waitingPlaceholder(t, db, "chat-1:msg-1", time.Unix(100, 0))

	saved, err := db.SaveMediaMessage(ctx, MediaMessageInput{
		TextMessageInput: TextMessageInput{
			ID:          "chat-1:msg-1",
			ChatID:      "chat-1",
			SenderID:    "sender-1",
			Text:        "look",
			Timestamp:   time.Unix(100, 0),
			Direction:   DirectionIncoming,
			Status:      StatusDelivered,
			CountUnread: true,
		},
		MediaKind:     MediaKindImage,
		MediaMimeType: "image/jpeg",
		MediaWidth:    800,
		MediaHeight:   600,
	})
	if err != nil {
		t.Fatalf("save resend: %v", err)
	}
	if saved.Message.MediaKind != MediaKindImage {
		t.Fatalf("kind = %q", saved.Message.MediaKind)
	}
	if saved.Message.MediaWidth != 800 || saved.Message.MediaHeight != 600 {
		t.Fatalf("size = %dx%d", saved.Message.MediaWidth, saved.Message.MediaHeight)
	}
	if saved.Chat.UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1", saved.Chat.UnreadCount)
	}
}

// A resend that reports a later time must not move the message: the placeholder
// already knows when it was really sent.
func TestResendDoesNotMoveTheMessageToWhenItWasResent(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	waitingPlaceholder(t, db, "chat-1:msg-1", time.Unix(100, 0))

	saved, err := db.SaveTextMessage(ctx, TextMessageInput{
		ID:        "chat-1:msg-1",
		ChatID:    "chat-1",
		SenderID:  "sender-1",
		Text:      "here it is",
		Timestamp: time.Unix(9_000, 0),
		Direction: DirectionIncoming,
		Status:    StatusDelivered,
	})
	if err != nil {
		t.Fatalf("save resend: %v", err)
	}
	if saved.Message.TimestampUnix != 100 {
		t.Fatalf("timestamp = %d, want the original 100", saved.Message.TimestampUnix)
	}
}
