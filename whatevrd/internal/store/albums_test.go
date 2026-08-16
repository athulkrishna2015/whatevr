package store

import (
	"context"
	"testing"
	"time"
)

func seedAlbumHeader(t *testing.T, db *DB, messageID, chatID string, expectedImages int) {
	t.Helper()
	payload, err := EncodePayload(MessagePayload{Album: &AlbumPayload{ExpectedImages: expectedImages}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveMediaMessage(context.Background(), MediaMessageInput{
		TextMessageInput: TextMessageInput{
			ID:          messageID,
			ChatID:      chatID,
			SenderID:    "ana@s.whatsapp.net",
			Timestamp:   time.Unix(1_700_000_000, 0),
			CountUnread: true,
		},
		MediaKind:      MediaKindAlbum,
		PayloadJSON:    payload,
		PayloadSummary: "3 photos",
	}); err != nil {
		t.Fatalf("seed album: %v", err)
	}
}

func seedAlbumPicture(t *testing.T, db *DB, messageID, chatID, parentID string, index int32) {
	t.Helper()
	if _, err := db.SaveMediaMessage(context.Background(), MediaMessageInput{
		TextMessageInput: TextMessageInput{
			ID:          messageID,
			ChatID:      chatID,
			SenderID:    "ana@s.whatsapp.net",
			Timestamp:   time.Unix(1_700_000_001, 0),
			CountUnread: true,
		},
		MediaKind:     MediaKindImage,
		MediaMimeType: "image/jpeg",
		AlbumParentID: parentID,
		AlbumIndex:    index,
	}); err != nil {
		t.Fatalf("seed picture %s: %v", messageID, err)
	}
}

// The transcript shows the album, not its pictures, and the album carries them.
func TestAlbumHidesItsPicturesAndCarriesThem(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedAlbumHeader(t, db, "chat-1:al", "chat-1", 3)
	// Deliberately out of order: the sender's index is what decides the layout.
	seedAlbumPicture(t, db, "chat-1:p3", "chat-1", "chat-1:al", 2)
	seedAlbumPicture(t, db, "chat-1:p1", "chat-1", "chat-1:al", 0)
	seedAlbumPicture(t, db, "chat-1:p2", "chat-1", "chat-1:al", 1)

	messages, err := db.ListMessages(ctx, "chat-1", 50, "")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 {
		for _, m := range messages {
			t.Logf("row %s kind %s parent %q", m.ID, m.MediaKind, m.AlbumParentID)
		}
		t.Fatalf("transcript has %d rows, want just the album", len(messages))
	}
	album := messages[0]
	if album.ID != "chat-1:al" {
		t.Fatalf("the row shown is %s", album.ID)
	}
	if len(album.Album) != 3 {
		t.Fatalf("album carries %d pictures, want 3", len(album.Album))
	}
	for i, want := range []string{"chat-1:p1", "chat-1:p2", "chat-1:p3"} {
		if album.Album[i].ID != want {
			t.Errorf("picture %d is %s, want %s", i, album.Album[i].ID, want)
		}
	}
	// A tile is a whole message, not a stripped-down copy of one.
	if album.Album[0].MediaMimeType != "image/jpeg" || album.Album[0].ChatID != "chat-1" {
		t.Errorf("a tile lost the fields that make it a message: %+v", album.Album[0])
	}
}

// A header that never arrives must not swallow its pictures. Hiding and
// grouping ask the same question, so an album nobody can see cannot hide
// anything either.
func TestPicturesWithNoHeaderStayOrdinaryMessages(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedAlbumPicture(t, db, "chat-1:p1", "chat-1", "chat-1:missing", 0)
	seedAlbumPicture(t, db, "chat-1:p2", "chat-1", "chat-1:missing", 1)

	messages, err := db.ListMessages(ctx, "chat-1", 50, "")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("transcript has %d rows, want both orphaned pictures", len(messages))
	}
}

// Five photos sent at once are one line in the chat list and one number on the
// badge. The album header did that accounting; its pictures must not do it
// again.
func TestPicturesInAnAlbumDoNotBumpTheChatAgain(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedAlbumHeader(t, db, "chat-1:al", "chat-1", 3)
	seedAlbumPicture(t, db, "chat-1:p1", "chat-1", "chat-1:al", 0)
	seedAlbumPicture(t, db, "chat-1:p2", "chat-1", "chat-1:al", 1)
	seedAlbumPicture(t, db, "chat-1:p3", "chat-1", "chat-1:al", 2)

	chat, err := db.GetChat(ctx, "chat-1")
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if chat.UnreadCount != 1 {
		t.Errorf("unread = %d, want 1: an album is one thing somebody sent", chat.UnreadCount)
	}
	if chat.LastMessage != "🖼️ Album: 3 photos" {
		t.Errorf("chat preview = %q, want the album rather than its last tile", chat.LastMessage)
	}
}

// Jumping to a picture inside an album lands on the album, because the picture
// is not a row the transcript ever draws.
func TestJumpingToAPictureAnchorsItsAlbum(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedAlbumHeader(t, db, "chat-1:al", "chat-1", 2)
	seedAlbumPicture(t, db, "chat-1:p1", "chat-1", "chat-1:al", 0)
	seedAlbumPicture(t, db, "chat-1:p2", "chat-1", "chat-1:al", 1)

	window, err := db.ListMessagesAround(ctx, "chat-1", 1, "chat-1:p2")
	if err != nil {
		t.Fatalf("list around: %v", err)
	}
	if len(window) != 1 || window[0].ID != "chat-1:al" {
		t.Fatalf("anchored on %+v, want the album", window)
	}
}

// An album still filling says so. One that filled describes itself by what it
// holds instead, which is why the promise is dropped once it is kept.
func TestAlbumExpectedCountOnlyMattersWhileUnkept(t *testing.T) {
	payload := &AlbumPayload{ExpectedImages: 2, ExpectedVideos: 1}
	if payload.Expected() != 3 {
		t.Fatalf("expected = %d, want 3", payload.Expected())
	}
	if (*AlbumPayload)(nil).Expected() != 0 {
		t.Error("an album with no payload promised nothing")
	}
}
