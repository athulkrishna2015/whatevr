//go:build whatevr_mock

package ingest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/wamock"
)

func init() {
	wamock.Register(wamock.Scenario{
		Name:        "test:model",
		Description: "history in a dm and a group, a live message after",
		Build: func(w *wamock.World) {
			asha := w.Contact("917770000001", "Asha")
			bo := w.Contact("917770000002", "Bo")
			dm := w.DM(asha)
			dm.History(asha, "from long ago", wamock.Ago(48*time.Hour))
			dm.HistoryFromMe("my old reply", wamock.Ago(47*time.Hour))
			g := w.Group("Trip planning", asha, bo)
			g.History(bo, "who is driving", wamock.Ago(24*time.Hour))
			dm.Say(asha, "are you there", wamock.Ago(time.Minute))
		},
	})
	wamock.Register(wamock.Scenario{
		Name:        "test:recovery",
		Description: "an unpin made on the phone after login, for app state recovery",
		Build: func(w *wamock.World) {
			asha := w.Contact("917770000001", "Asha")
			// pinned at login, so regular_low has a version and the unpin is
			// fetched as a patch on top, not a full sync
			dm := w.DM(asha).Pin()
			dm.Say(asha, "unpin me", wamock.Ago(time.Minute))
			w.After(8*time.Second, func() { dm.Unpin() })
		},
	})
}

// modelClient pairs a real client with the mock, logging into a core that
// folds through every model domain, jobs running.
func modelClient(ctx context.Context, t *testing.T, scenario string) (*whatsmeow.Client, *core.DB, *wamock.Server) {
	t.Helper()
	relay := &qrRelay{}
	srv, err := wamock.New(ctx, wamock.Options{Seed: 7, Scenario: scenario, Login: relay})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	dir := t.TempDir()
	db, err := core.Open(ctx, filepath.Join(dir, "core.db"), core.Options{Domains: model.Domains(), Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sessionDB, err := sql.Open("sqlite3", "file:"+filepath.Join(dir, "session.db")+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	sessionDB.SetMaxOpenConns(1)
	container := sqlstore.NewWithDB(sessionDB, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Close() })
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cli := whatsmeow.NewClient(device, waLog.Noop)
	t.Cleanup(cli.Disconnect)
	New(zerolog.Nop().WithContext(ctx), db).Attach(cli)
	qr, err := cli.GetQRChannel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for item := range qr {
			if item.Event == whatsmeow.QRChannelEventCode {
				relay.publish(item.Code)
			}
		}
	}()
	if err := cli.Connect(); err != nil {
		t.Fatal(err)
	}
	return cli, db, srv
}

func rowsOf(t *testing.T, db *core.DB, q string) []string {
	t.Helper()
	rows, err := db.Read().Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestHistoryAndGroupsLandInTheModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, db, _ := modelClient(ctx, t, "test:model")

	eventually(t, "history and the live message", func() bool {
		return len(rowsOf(t, db, `SELECT id FROM msg`)) >= 4
	})
	eventually(t, "every blob downloaded", func() bool {
		return len(rowsOf(t, db, `SELECT notification FROM hist_note`)) > 0 &&
			len(rowsOf(t, db, `SELECT notification FROM hist_note WHERE notification NOT IN (SELECT notification FROM hist_blob)`)) == 0
	})
	eventually(t, "the group fetched", func() bool {
		return len(rowsOf(t, db, `SELECT value FROM grp_field WHERE field = 'name' AND value = 'Trip planning' AND t > 1`)) == 1
	})
	if got := rowsOf(t, db, `SELECT text FROM msg ORDER BY t`); len(got) != 4 || got[0] != "from long ago" || got[3] != "are you there" {
		t.Fatalf("messages %q", got)
	}
	if got := rowsOf(t, db, `SELECT error FROM hist_blob WHERE error != ''`); len(got) != 0 {
		t.Fatalf("blob errors %q", got)
	}
	if got := rowsOf(t, db, `SELECT jid FROM id_self`); len(got) == 0 {
		t.Fatal("own addresses not logged")
	}
	if got := rowsOf(t, db, `SELECT name FROM id_name WHERE source = 'push' AND jid = '917770000001@s.whatsapp.net'`); len(got) != 1 || got[0] != "Asha" {
		t.Fatalf("asha's push name %q", got)
	}
	if f, err := db.FoldFailures(ctx); err != nil || len(f) > 0 {
		t.Fatalf("fold failures %v %v", f, err)
	}
}

// a collection whose patch does not verify is fixed by a full sync, and
// every step of that is in the log
func TestBrokenAppStateRecoversThroughTheLog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, db, srv := modelClient(ctx, t, "test:recovery")
	// the sync after login goes through untouched, the pin's fetch breaks
	eventually(t, "the first regular_low sync", func() bool {
		return len(rowsOf(t, db, `SELECT 1 FROM sync_domain WHERE domain = 'app_state:regular_low' AND ok_t > 0`)) > 0
	})
	if err := srv.Fault(wamock.FaultAppState, 0); err != nil {
		t.Fatal(err)
	}
	steps := func() []string {
		return rowsOf(t, db, `SELECT json_extract(head, '$.step') FROM inputs WHERE kind = 'sync_state'
			AND json_extract(head, '$.step') IS NOT NULL ORDER BY seq`)
	}
	eventually(t, "a recovery, start to end", func() bool {
		s := steps()
		return len(s) >= 2 && s[len(s)-1] == "0"
	})
	if s := steps(); s[0] != "1" {
		t.Fatalf("recovery steps %q, want 1 (full sync) then 0 (healthy)", s)
	}
	eventually(t, "the unpin", func() bool {
		return len(rowsOf(t, db, `SELECT a FROM appstate WHERE kind = 'pin_v1' AND on_ = 0`)) > 0
	})
	// the sync complete that clears the error is dispatched after step 0 is saved
	eventually(t, "regular_low known again", func() bool {
		c, err := model.NewReader(db.Read()).Completeness(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range c.AppState {
			if d.Domain == "regular_low" {
				return d.State == model.Known && d.Recovery == ""
			}
		}
		return false
	})
}

// pairing saves the device, and saving hands it the container's lid map; the
// pairs learned after that still have to reach the log
func TestLIDPairsAfterPairingAreLogged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cli, db, _ := modelClient(ctx, t, "test:model")
	eventually(t, "the live message", func() bool {
		return len(rowsOf(t, db, `SELECT id FROM msg`)) >= 4
	})
	if _, ok := cli.Store.LIDs.(*lids); !ok {
		t.Fatalf("lid map is %T after pairing, not the wrapper", cli.Store.LIDs)
	}
	if err := cli.Store.LIDs.PutLIDMapping(ctx, types.NewJID("100000000077", types.HiddenUserServer), types.NewJID("917770000077", types.DefaultUserServer)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the pair in the log", func() bool {
		return len(rowsOf(t, db, `SELECT seq FROM inputs WHERE kind = 'lid_mapping' AND head LIKE '%100000000077@lid%'`)) == 1
	})
}
