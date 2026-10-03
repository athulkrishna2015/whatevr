package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"whatevrd/internal/conn"
	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/status"
)

func TestConnStatesLandOnTheBoard(t *testing.T) {
	b := status.New()
	put := func(s conn.Status) {
		if ps, ok := connProblems(s); ok {
			if err := b.Only("conn", ps...); err != nil {
				t.Fatal(err)
			}
		}
	}
	kinds := func() []status.Kind {
		var out []status.Kind
		for _, p := range b.List(context.Background()) {
			out = append(out, p.Kind)
		}
		return out
	}
	expect := func(want ...status.Kind) {
		t.Helper()
		got := kinds()
		if len(got) != len(want) {
			t.Fatalf("board %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("board %v, want %v", got, want)
			}
		}
	}
	put(conn.Status{Kind: conn.Online})
	expect()
	put(conn.Status{Kind: conn.Connecting, Cause: conn.CauseSilent, Detail: "Reconnecting: Keepalive lost"})
	expect(status.KeepaliveLost)
	// a retry with nothing new keeps what went wrong
	put(conn.Status{Kind: conn.Connecting, Detail: "Connecting to WhatsApp"})
	expect(status.KeepaliveLost)
	put(conn.Status{Kind: conn.Waiting, Cause: conn.CauseNoAnswer})
	expect(status.StuckLogin)
	put(conn.Status{Kind: conn.Waiting, Cause: conn.CauseUnreachable})
	expect(status.NoNetwork)
	put(conn.Status{Kind: conn.Waiting, Cause: conn.CauseRefused})
	expect(status.Refused)
	put(conn.Status{Kind: conn.Replaced})
	expect(status.Replaced)
	put(conn.Status{Kind: conn.Online})
	expect()
	put(conn.Status{Kind: conn.LoggedOut})
	expect(status.LoggedOut)
	put(conn.Status{Kind: conn.NeedLogin})
	expect()
}

func TestProvidersReadTheCore(t *testing.T) {
	ctx := context.Background()
	db, err := core.Open(ctx, filepath.Join(t.TempDir(), "core.db"), core.Options{Domains: model.Domains(), Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := time.Now().Add(-time.Hour)
	add := func(kind string, h any, at time.Time) {
		raw, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		seq, err := db.Append(ctx, core.Input{Kind: kind, V: 1, At: at, Head: raw})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.WaitFolded(ctx, seq); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p status.Provider) map[status.Kind]status.Problem {
		ps, err := p(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[status.Kind]status.Problem{}
		for _, x := range ps {
			out[x.Kind] = x
		}
		return out
	}
	if ps := read(syncProblems(db)); len(ps) != 0 {
		t.Fatalf("a fresh core has problems: %+v", ps)
	}

	chat := "917770000001@s.whatsapp.net"
	add(core.KindSyncState, core.SyncStateHead{Domain: "app_state:regular", State: core.SyncError, Error: "mismatching LTHash"}, old)
	add(core.KindHistoryNotification, core.HistoryNotificationHead{ID: "N1", SyncType: "FULL", ChunkOrder: 1, Progress: 40}, old)
	add(core.KindUndecryptable, core.UndecryptableHead{Source: core.Source{Chat: chat, Sender: chat}, ID: "U1", T: old.Unix()}, old)
	ps := read(syncProblems(db))
	if p := ps[status.AppStateOutOfSync]; !strings.Contains(p.Detail, "regular: mismatching LTHash") {
		t.Errorf("app state %+v", p)
	}
	if p := ps[status.HistoryStalled]; !strings.Contains(p.Detail, "FULL: at 40%") {
		t.Errorf("history %+v", p)
	}
	if p := ps[status.WaitingOnPhone]; !strings.HasPrefix(p.Detail, "1 messages") {
		t.Errorf("waiting %+v", p)
	}

	add(core.KindOutbox, core.OutboxHead{Op: core.OutboxQueue, Chat: chat, ID: "Q1"}, old)
	if ps := read(outboxProblems(db)); len(ps) != 0 {
		t.Errorf("a send not tried yet is not failing: %+v", ps)
	}
	add(core.KindOutbox, core.OutboxHead{Op: core.OutboxAttempt, Chat: chat, ID: "Q1", Error: "timed out"}, old.Add(time.Minute))
	if p := read(outboxProblems(db))[status.OutboxFailing]; p.Detail != "1 sends failing, last: timed out" {
		t.Errorf("outbox %+v", p)
	}
	add(core.KindOutbox, core.OutboxHead{Op: core.OutboxCancel, Chat: chat, ID: "Q1"}, old.Add(2*time.Minute))
	if ps := read(outboxProblems(db)); len(ps) != 0 {
		t.Errorf("a cancelled send still fails: %+v", ps)
	}

	if ps := read(coreProblems(db)); len(ps) != 0 {
		t.Errorf("a working core has problems: %+v", ps)
	}
}

func TestRederiveFoldsTheLogAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	db, err := core.Open(ctx, path, core.Options{Domains: model.Domains(), Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	chat := "917770000001@s.whatsapp.net"
	head, _ := json.Marshal(core.OutboxHead{Op: core.OutboxQueue, Chat: chat, ID: "Q1"})
	seq, err := db.Append(ctx, core.Input{Kind: core.KindOutbox, V: 1, Head: head})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WaitFolded(ctx, seq); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// a derived row nothing in the log says, as a fold bug would leave
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO outbox (chat, id, queued_t, cancelled) VALUES ('x', 'stray', 1, 0)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	var out, errs strings.Builder
	if code := rederive(ctx, path, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if !strings.HasPrefix(out.String(), "folded 1 inputs") {
		t.Errorf("output %q", out.String())
	}
	db, err = core.Open(ctx, path, core.Options{Domains: model.Domains(), Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	unsent, err := model.NewReader(db.Read()).Unsent(ctx)
	if err != nil || len(unsent) != 1 || unsent[0].ID != "Q1" {
		t.Fatalf("after rederive %+v %v", unsent, err)
	}
}

// one failed fetch is that message's problem; a run of them is media's,
// until one works
func TestMediaFailingNeedsARun(t *testing.T) {
	b := status.New()
	m := &mediaWatch{board: b, log: zerolog.Nop(), now: time.Now}
	boom := errors.New("dial tcp: no route to host")
	has := func() bool {
		for _, p := range b.List(context.Background()) {
			if p.Kind == status.MediaFailing {
				return true
			}
		}
		return false
	}
	m.result(boom)
	m.result(boom)
	m.result(nil)
	m.result(boom)
	m.result(boom)
	if has() {
		t.Fatal("media failing after two in a row")
	}
	m.result(boom)
	if !has() {
		t.Fatal("three in a row not on the board")
	}
	m.result(nil)
	if has() {
		t.Fatal("a fetch worked and media is still failing")
	}
}
