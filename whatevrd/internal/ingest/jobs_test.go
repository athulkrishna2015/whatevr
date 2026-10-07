package ingest

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

// a notification still folding when the jobs start is queued once it folds
func TestStartupQueuesHistoryStillFolding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	domains := model.Domains()
	for i := range domains {
		if domains[i].Name != "history" {
			continue
		}
		folds := maps.Clone(domains[i].Folds)
		fold := folds[core.KindHistoryNotification]
		folds[core.KindHistoryNotification] = func(tx *core.Tx, in core.Input) error {
			close(started)
			<-release
			return fold(tx, in)
		}
		domains[i].Folds = folds
	}
	db, err := core.Open(ctx, filepath.Join(t.TempDir(), "core.db"), core.Options{Domains: domains, Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h, _ := json.Marshal(core.HistoryNotificationHead{ID: "N1", SyncType: "FULL", Progress: 100})
	b, _ := proto.Marshal(&waE2E.HistorySyncNotification{})
	if _, err := db.Append(ctx, core.Input{Kind: core.KindHistoryNotification, V: 1, Head: h, Body: b}); err != nil {
		close(release)
		t.Fatal(err)
	}
	<-started
	g := New(ctx, db)
	g.startJobs(nil, db.Read())
	close(release)
	for deadline := time.Now().Add(5 * time.Second); g.jobs.history.pending() != 1; {
		if time.Now().After(deadline) {
			t.Fatalf("history jobs pending=%d, want the one notification", g.jobs.history.pending())
		}
		time.Sleep(time.Millisecond)
	}
}
