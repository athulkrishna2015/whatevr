package views

import (
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/model"
)

// two finished types: the row is the one the phone sent last, whichever
// download landed last
func TestADoneSyncShowsWhatThePhoneSentLast(t *testing.T) {
	hs := []model.HistoryState{
		{SyncType: "INITIAL_BOOTSTRAP", State: model.Known, Progress: 100, Blobs: 5, Last: 2000, Newest: 7},
		{SyncType: "PUSH_NAME", State: model.Known, Blobs: 1, Last: 3000, Newest: 4},
	}
	row := syncRow(hs, time.UnixMilli(4000))
	if row.GetType() != v2.SyncType_SYNC_TYPE_INITIAL || row.GetPhase() != v2.SyncPhase_SYNC_PHASE_DONE {
		t.Fatalf("got %v %v", row.GetType(), row.GetPhase())
	}
	hs[1].State = model.Syncing
	if row := syncRow(hs, time.UnixMilli(4000)); row.GetType() != v2.SyncType_SYNC_TYPE_PUSH_NAMES {
		t.Fatalf("a running type should win, got %v", row.GetType())
	}
}
