package model

import (
	"context"
	"reflect"
	"testing"

	"whatevrd/internal/core"
)

func TestPendingWorkIsReadAPageAtATime(t *testing.T) {
	ctx := context.Background()
	db := openModel(t)
	scen := []core.Input{in(core.KindLIDMapping, core.LIDMappingHead{LID: meLID, PN: mePN, Self: true}, nil, at(0))}
	for i, id := range []string{"N1", "N2", "N3", "N4", "N5"} {
		scen = append(scen, in(core.KindHistoryNotification, core.HistoryNotificationHead{ID: id, SyncType: "FULL", ChunkOrder: uint32(i)}, nil, at(10+i)))
	}
	for i, g := range []string{"3@g.us", "1@g.us", "2@g.us"} {
		scen = append(scen, msgIn("G"+g[:1], g, boL, boPN, false, 20+i, text("hi")))
	}
	feed(t, db, scen)

	var blobs []string
	for after := int64(0); ; {
		page, next, err := PendingHistory(ctx, db.Read(), after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range page {
			blobs = append(blobs, b.ID)
		}
		if next == 0 {
			break
		}
		after = next
	}
	if want := []string{"N1", "N2", "N3", "N4", "N5"}; !reflect.DeepEqual(blobs, want) {
		t.Fatalf("blobs %v, want %v", blobs, want)
	}

	var groups []string
	for after := ""; ; {
		page, err := UnfetchedGroups(ctx, db.Read(), after, 2)
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, page...)
		if len(page) < 2 {
			break
		}
		after = page[len(page)-1]
	}
	if want := []string{"1@g.us", "2@g.us", "3@g.us"}; !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups %v, want %v", groups, want)
	}
}
