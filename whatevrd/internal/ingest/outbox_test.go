package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

func TestOnlyWhatThisDaemonQueuedMayGoOut(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := core.Open(ctx, filepath.Join(dir, "core.db"), core.Options{Domains: model.Domains(), Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := New(ctx, db)
	const (
		asha  = "917770000001@s.whatsapp.net"
		boPN  = "917770000002@s.whatsapp.net"
		boLID = "100000000002@lid"
		grp   = "120363000000000001@g.us"
	)
	appendIn := func(kind string, head any, body []byte) {
		raw, _ := json.Marshal(head)
		if _, err := db.Append(ctx, core.Input{Kind: kind, V: 1, Head: raw, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := proto.Marshal(&waE2E.Message{Conversation: proto.String("from the phone, days ago")})
	// ours, in the log, never queued here: the phone's pending message
	appendIn(core.KindMessage, core.MessageHead{Source: core.Source{Chat: asha, Sender: asha, FromMe: true}, ID: "H9", T: 1}, body)
	appendIn(core.KindLIDMapping, core.LIDMappingHead{LID: boLID, PN: boPN}, nil)

	check := func(chat, id string, want error) {
		t.Helper()
		if err := g.MaySend(ctx, chat, id); !errors.Is(err, want) {
			t.Errorf("MaySend(%s, %s) = %v, want %v", chat, id, err, want)
		}
	}
	check(asha, "H9", ErrNotQueued)
	if err := g.Queued(ctx, asha, "Q1"); err != nil {
		t.Fatal(err)
	}
	check(asha, "Q1", nil)
	if err := g.Cancel(ctx, asha, "Q1"); err != nil {
		t.Fatal(err)
	}
	check(asha, "Q1", ErrCancelled)
	if err := g.Queued(ctx, asha, "Q2"); err != nil {
		t.Fatal(err)
	}
	appendIn(core.KindMessage, core.MessageHead{Source: core.Source{Chat: asha, Sender: asha, FromMe: true}, ID: "Q2", T: 2, Sent: true}, body)
	check(asha, "Q2", ErrSent)

	// the guard: bo is allowed by number, so his lid is too; asha and the
	// group are not
	sdb, err := sql.Open("sqlite3", "file:"+filepath.Join(dir, "session.db")+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	sdb.SetMaxOpenConns(1)
	container := sqlstore.NewWithDB(sdb, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	defer container.Close()
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cli := whatsmeow.NewClient(device, waLog.Noop)
	cli.SendGuard = &whatsmeow.SendGuard{Allow: []types.JID{types.NewJID("917770000002", types.DefaultUserServer)}}
	g.jobs = &jobs{g: g, cli: cli}
	for _, q := range []struct{ chat, id string }{{asha, "Q3"}, {boLID, "Q4"}, {boPN, "Q5"}, {grp, "Q6"}} {
		if err := g.Queued(ctx, q.chat, q.id); err != nil {
			t.Fatal(err)
		}
	}
	check(asha, "Q3", ErrGuarded)
	check(boLID, "Q4", nil)
	check(boPN, "Q5", nil)
	check(grp, "Q6", ErrGuarded)
	if err := g.Guard(ctx, asha); !errors.Is(err, ErrGuarded) {
		t.Errorf("Guard(asha) = %v", err)
	}

	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, last := db.Progress()
	if err := db.WaitFolded(wctx, last); err != nil {
		t.Fatal(err)
	}
	unsent, err := model.NewReader(db.Read()).Unsent(ctx)
	if err != nil || len(unsent) != 4 {
		t.Fatalf("unsent %+v %v", unsent, err)
	}
}
