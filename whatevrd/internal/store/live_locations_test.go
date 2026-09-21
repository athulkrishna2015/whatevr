package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedLiveShare(t *testing.T, db *DB, messageID, chatID, senderID string, startedAt, expiresAt int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SaveTextMessage(ctx, TextMessageInput{
		ID:        messageID,
		ChatID:    chatID,
		SenderID:  senderID,
		Text:      "live",
		Timestamp: time.Unix(startedAt, 0),
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := db.OpenLiveLocationShare(ctx, LiveLocationShare{
		MessageID: messageID,
		ChatID:    chatID,
		SenderID:  senderID,
		StartedAt: startedAt,
		ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("open share: %v", err)
	}
}

// whatsmeow gives no correlation between a live share and the position updates
// that follow it, so matching an update to the newest open share from the same
// sender in the same chat is entirely our rule. If it breaks, updates land on
// the wrong pin or nowhere.
func TestLatestOpenLiveLocationShareMatchesTheNewestFromTheSender(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := int64(1_700_000_000)

	seedLiveShare(t, db, "old", "chat-1", "ana@s.whatsapp.net", now-3600, now+3600)
	seedLiveShare(t, db, "new", "chat-1", "ana@s.whatsapp.net", now-60, now+3600)
	seedLiveShare(t, db, "other-sender", "chat-1", "bo@s.whatsapp.net", now-10, now+3600)
	seedLiveShare(t, db, "other-chat", "chat-2", "ana@s.whatsapp.net", now-5, now+3600)

	share, err := db.LatestOpenLiveLocationShare(ctx, "chat-1", "ana@s.whatsapp.net", now)
	if err != nil {
		t.Fatalf("LatestOpenLiveLocationShare() error = %v", err)
	}
	if share.MessageID != "new" {
		t.Fatalf("matched %q, want the newest share from that sender", share.MessageID)
	}

	// An expired share is not a candidate, so a stale update opens its own.
	seedLiveShare(t, db, "expired", "chat-3", "cy@s.whatsapp.net", now-7200, now-3600)
	if _, err := db.LatestOpenLiveLocationShare(ctx, "chat-3", "cy@s.whatsapp.net", now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("an expired share matched: %v", err)
	}
}

// WhatsApp replays and reorders live-location updates. Applying a sequence
// number we have already passed would drag the pin backwards.
func TestAppendLiveLocationPointRejectsStaleSequences(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := int64(1_700_000_000)
	seedLiveShare(t, db, "m1", "chat-1", "ana@s.whatsapp.net", now, now+3600)

	applied, err := db.AppendLiveLocationPoint(ctx, "m1", LiveLocationPoint{Seq: 5, TimestampUnix: now + 10, Latitude: 1, Longitude: 2})
	if err != nil || !applied {
		t.Fatalf("first point: applied = %v, err = %v", applied, err)
	}
	applied, err = db.AppendLiveLocationPoint(ctx, "m1", LiveLocationPoint{Seq: 3, TimestampUnix: now + 20, Latitude: 9, Longitude: 9})
	if err != nil {
		t.Fatalf("stale point: %v", err)
	}
	if applied {
		t.Fatal("a sequence number we already passed was applied")
	}
	applied, err = db.AppendLiveLocationPoint(ctx, "m1", LiveLocationPoint{Seq: 6, TimestampUnix: now + 30, Latitude: 3, Longitude: 4})
	if err != nil || !applied {
		t.Fatalf("newer point: applied = %v, err = %v", applied, err)
	}

	trail, err := db.LiveLocationTrail(ctx, "m1", 0)
	if err != nil {
		t.Fatalf("LiveLocationTrail() error = %v", err)
	}
	if len(trail) != 2 {
		t.Fatalf("trail has %d points, want 2", len(trail))
	}
	// Oldest first, so drawing a path just walks the slice.
	if trail[0].Seq != 5 || trail[1].Seq != 6 {
		t.Fatalf("trail order = %d, %d; want 5, 6", trail[0].Seq, trail[1].Seq)
	}
}

// A sender that does not number its updates sends sequence 0 every time, and
// those must all be kept rather than each one looking already-seen.
func TestAppendLiveLocationPointKeepsUnsequencedUpdates(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := int64(1_700_000_000)
	seedLiveShare(t, db, "m1", "chat-1", "ana@s.whatsapp.net", now, now+3600)

	for i := range 3 {
		applied, err := db.AppendLiveLocationPoint(ctx, "m1", LiveLocationPoint{
			Seq: 0, TimestampUnix: now + int64(i), Latitude: float64(i), Longitude: float64(i),
		})
		if err != nil || !applied {
			t.Fatalf("unsequenced point %d: applied = %v, err = %v", i, applied, err)
		}
	}

	trail, err := db.LiveLocationTrail(ctx, "m1", 0)
	if err != nil {
		t.Fatalf("LiveLocationTrail() error = %v", err)
	}
	// They collapse onto one row by primary key, which is the honest outcome:
	// without sequence numbers there is no trail, only a current position.
	if len(trail) != 1 || trail[0].Latitude != 2 {
		t.Fatalf("trail = %+v, want one point holding the newest position", trail)
	}
}

func TestExpireLiveLocationSharesClosesOnlyThePastOnes(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := int64(1_700_000_000)

	seedLiveShare(t, db, "running", "chat-1", "ana@s.whatsapp.net", now-60, now+3600)
	seedLiveShare(t, db, "over", "chat-1", "bo@s.whatsapp.net", now-7200, now-60)
	seedLiveShare(t, db, "endless", "chat-1", "cy@s.whatsapp.net", now-60, 0)

	expired, err := db.ExpireLiveLocationShares(ctx, now)
	if err != nil {
		t.Fatalf("ExpireLiveLocationShares() error = %v", err)
	}
	if len(expired) != 1 || expired[0] != "over" {
		t.Fatalf("expired = %v, want just the finished share", expired)
	}

	open, err := db.ListLiveLocationShares(ctx, "chat-1", now)
	if err != nil {
		t.Fatalf("ListLiveLocationShares() error = %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("%d shares still open, want 2", len(open))
	}
}

// Deleting a message must take its share and trail with it, or a cleared chat
// leaves a banner claiming somebody is still sharing.
func TestLiveLocationRowsCascadeWithTheirMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := int64(1_700_000_000)
	seedLiveShare(t, db, "m1", "chat-1", "ana@s.whatsapp.net", now, now+3600)
	if _, err := db.AppendLiveLocationPoint(ctx, "m1", LiveLocationPoint{Seq: 1, TimestampUnix: now, Latitude: 1, Longitude: 2}); err != nil {
		t.Fatalf("append point: %v", err)
	}

	if _, _, _, err := db.DeleteMessageForMe(ctx, "m1"); err != nil {
		t.Fatalf("DeleteMessageForMe() error = %v", err)
	}

	if _, err := db.GetLiveLocationShare(ctx, "m1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the share outlived its message: %v", err)
	}
	trail, err := db.LiveLocationTrail(ctx, "m1", 0)
	if err != nil {
		t.Fatalf("LiveLocationTrail() error = %v", err)
	}
	if len(trail) != 0 {
		t.Fatalf("the trail outlived its message: %+v", trail)
	}
}
