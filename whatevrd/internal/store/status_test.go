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
	saved, err := db.SetStatusMediaPath(ctx, "status:m1", "/cache/status/m1.jpg")
	if err != nil || saved.MediaLocalPath != "/cache/status/m1.jpg" {
		t.Fatalf("set media path = %+v, %v", saved, err)
	}

	pruned, err := db.PruneOldStatusUpdates(ctx, 0)
	if err != nil || pruned != 2 {
		t.Fatalf("prune = %d, %v; want 2", pruned, err)
	}
	if remaining, err := db.ListStatusUpdates(ctx, 0); err != nil || len(remaining) != 0 {
		t.Fatalf("remaining after prune = %v, %v; want none", remaining, err)
	}
}
