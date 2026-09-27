package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestStatusUpdateRoundTrip locks in the status store lifecycle: save (with
// dedup), list newest-first, mark viewed, attach a downloaded media path, and
// prune expired rows.
func TestStatusUpdateRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	first, inserted, err := db.SaveStatusUpdate(ctx, StatusUpdateInput{
		ID:        "status:m1",
		SenderID:  "peer@s.whatsapp.net",
		Timestamp: time.Unix(200, 0),
		Kind:      MediaKindImage,
		Text:      "sunset",
	})
	if err != nil || !inserted {
		t.Fatalf("save status = %+v, %v, %v; want inserted", first, inserted, err)
	}
	if _, dup, err := db.SaveStatusUpdate(ctx, StatusUpdateInput{ID: "status:m1", SenderID: "peer@s.whatsapp.net"}); err != nil || dup {
		t.Fatalf("duplicate save = %v, %v; want ignored", dup, err)
	}
	if _, _, err := db.SaveStatusUpdate(ctx, StatusUpdateInput{
		ID:        "status:m2",
		SenderID:  "peer@s.whatsapp.net",
		Timestamp: time.Unix(300, 0),
		Kind:      "text",
		Text:      "hello",
	}); err != nil {
		t.Fatalf("save second status: %v", err)
	}

	listed, err := db.ListStatusUpdates(ctx, 0)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "status:m2" || listed[1].ID != "status:m1" {
		t.Fatalf("list order = %v, want [status:m2 status:m1]", listed)
	}

	viewed, err := db.MarkStatusViewed(ctx, "status:m1")
	if err != nil || !viewed.Viewed {
		t.Fatalf("mark viewed = %+v, %v; want viewed", viewed, err)
	}
	saved, err := db.SetStatusMediaPath(ctx, "status:m1", "/cache/status/m1.jpg", "/cache/status/m1.thumb.jpg", 1280, 720)
	if err != nil || saved.MediaLocalPath != "/cache/status/m1.jpg" {
		t.Fatalf("set media path = %+v, %v", saved, err)
	}
	if saved.MediaThumbnailLocalPath != "/cache/status/m1.thumb.jpg" || saved.MediaWidth != 1280 || saved.MediaHeight != 720 {
		t.Fatalf("set media thumb/dims = %+v; want thumb + 1280x720", saved)
	}

	// The prune hands the caller the cached media paths of the rows it took,
	// so expired statuses do not leave their files behind in the cache.
	pruned, err := db.PruneOldStatusUpdates(ctx, 0)
	if err != nil || len(pruned) != 2 {
		t.Fatalf("prune = %v, %v; want 2 media paths", pruned, err)
	}
	if remaining, err := db.ListStatusUpdates(ctx, 0); err != nil || len(remaining) != 0 {
		t.Fatalf("remaining after prune = %v, %v; want none", remaining, err)
	}
}

// TestPruneOldStatusUpdatesRespects24h locks in the retention window: rows
// inside 24h survive, older rows go with their cached media paths and their
// view receipts.
func TestPruneOldStatusUpdatesRespects24h(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	fresh := time.Now().Add(-2 * time.Hour)
	stale := time.Now().Add(-30 * time.Hour)
	if _, _, err := db.SaveStatusUpdate(ctx, StatusUpdateInput{ID: "status:fresh", SenderID: "a@s.whatsapp.net", Timestamp: fresh, Kind: "text"}); err != nil {
		t.Fatalf("save fresh: %v", err)
	}
	if _, _, err := db.SaveStatusUpdate(ctx, StatusUpdateInput{ID: "status:stale", SenderID: "b@s.whatsapp.net", Timestamp: stale, Kind: MediaKindImage}); err != nil {
		t.Fatalf("save stale: %v", err)
	}
	if _, err := db.SetStatusMediaPath(ctx, "status:stale", "/cache/status/stale.jpg", "/cache/status/stale.thumb.jpg", 10, 10); err != nil {
		t.Fatalf("set stale media path: %v", err)
	}
	if err := db.RecordStatusViewer(ctx, "status:stale", "peer@s.whatsapp.net", fresh); err != nil {
		t.Fatalf("record viewer: %v", err)
	}

	pruned, err := db.PruneOldStatusUpdates(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	want := map[string]bool{"/cache/status/stale.jpg": true, "/cache/status/stale.thumb.jpg": true}
	if len(pruned) != len(want) {
		t.Fatalf("pruned paths = %v; want %v", pruned, want)
	}
	for _, path := range pruned {
		if !want[path] {
			t.Fatalf("pruned path %q is not the stale row's media", path)
		}
	}
	remaining, err := db.ListStatusUpdates(ctx, 0)
	if err != nil || len(remaining) != 1 || remaining[0].ID != "status:fresh" {
		t.Fatalf("remaining = %+v, %v; want only status:fresh", remaining, err)
	}
	if viewers, err := db.ListStatusViewers(ctx, "status:stale"); err != nil || len(viewers) != 0 {
		t.Fatalf("viewers of pruned status = %+v, %v; want none", viewers, err)
	}
}

// TestStatusMutedSendersRoundTrip locks in the mute flag lifecycle: set,
// list in mute order, unset.
func TestStatusMutedSendersRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.SetStatusMutedSender(ctx, "a@s.whatsapp.net", true); err != nil {
		t.Fatalf("mute a: %v", err)
	}
	if err := db.SetStatusMutedSender(ctx, "b@s.whatsapp.net", true); err != nil {
		t.Fatalf("mute b: %v", err)
	}
	muted, err := db.ListMutedStatusSenders(ctx)
	if err != nil || len(muted) != 2 || muted[0] != "a@s.whatsapp.net" || muted[1] != "b@s.whatsapp.net" {
		t.Fatalf("list muted = %v, %v; want [a b]", muted, err)
	}
	if err := db.SetStatusMutedSender(ctx, "a@s.whatsapp.net", false); err != nil {
		t.Fatalf("unmute a: %v", err)
	}
	if muted, err := db.ListMutedStatusSenders(ctx); err != nil || len(muted) != 1 || muted[0] != "b@s.whatsapp.net" {
		t.Fatalf("list muted after unmute = %v, %v; want [b]", muted, err)
	}
}

// TestReplaceMutedStatusSenders locks in the snapshot reconcile: the set
// becomes exactly ids (unlisted senders unmuted), blank ids are skipped,
// empty clears, and listing stays sorted regardless of input order.
func TestReplaceMutedStatusSenders(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.ReplaceMutedStatusSenders(ctx, []string{"b@s.whatsapp.net", "a@s.whatsapp.net", "  ", "a@s.whatsapp.net"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if muted, err := db.ListMutedStatusSenders(ctx); err != nil || len(muted) != 2 || muted[0] != "a@s.whatsapp.net" || muted[1] != "b@s.whatsapp.net" {
		t.Fatalf("list muted after replace = %v, %v; want [a b]", muted, err)
	}
	if err := db.ReplaceMutedStatusSenders(ctx, nil); err != nil {
		t.Fatalf("replace empty: %v", err)
	}
	if muted, err := db.ListMutedStatusSenders(ctx); err != nil || len(muted) != 0 {
		t.Fatalf("list muted after empty replace = %v, %v; want []", muted, err)
	}
}
