//go:build whatevr_mock

package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/wamock"
)

func init() {
	wamock.Register(wamock.Scenario{
		Name:        "test:ingest",
		Description: "one message waiting in the offline queue",
		Build: func(w *wamock.World) {
			asha := w.Contact("917770000001", "Asha")
			w.DM(asha).Pin().Say(asha, "waiting for you", wamock.Ago(time.Minute))
		},
	})
}

type qrRelay struct {
	mu  sync.Mutex
	chs []chan string
}

func (q *qrRelay) QRCodes() (<-chan string, func()) {
	ch := make(chan string, 8)
	q.mu.Lock()
	q.chs = append(q.chs, ch)
	q.mu.Unlock()
	return ch, func() {}
}

func (q *qrRelay) publish(code string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, ch := range q.chs {
		select {
		case ch <- code:
		default:
		}
	}
}

func conversation(in core.Input) string {
	if in.Kind != core.KindMessage {
		return ""
	}
	var m waE2E.Message
	if proto.Unmarshal(in.Body, &m) != nil {
		return ""
	}
	return m.GetConversation()
}

func pin(in core.Input) bool {
	var h core.AppStateHead
	return in.Kind == core.KindAppState && json.Unmarshal(in.Head, &h) == nil && len(h.Index) > 0 && h.Index[0] == "pin_v1"
}

// flakyLog refuses what refuse picks while failing is set, as a full disk
// would. everything else lands.
type flakyLog struct {
	*core.DB
	refuse   func(core.Input) bool
	mu       sync.Mutex
	failing  bool
	refusals int
}

func (f *flakyLog) AppendBatch(ctx context.Context, ins []core.Input) ([]int64, error) {
	f.mu.Lock()
	refuse := false
	for _, in := range ins {
		refuse = refuse || (f.failing && f.refuse(in))
	}
	if refuse {
		f.refusals++
	}
	f.mu.Unlock()
	if refuse {
		return nil, errors.New("disk full")
	}
	return f.DB.AppendBatch(ctx, ins)
}

func (f *flakyLog) set(failing bool) {
	f.mu.Lock()
	f.failing = failing
	f.mu.Unlock()
}

func (f *flakyLog) refused() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refusals
}

// arrivals counts message stanzas by id as they come off the wire.
type arrivals struct {
	mu   sync.Mutex
	seen map[string]int
}

func (a *arrivals) handler(_ context.Context, raw whatsmeow.RawNode) (*waBinary.Node, bool) {
	if raw.Node.Tag == "message" {
		a.mu.Lock()
		a.seen[raw.Node.AttrGetter().OptionalString("id")]++
		a.mu.Unlock()
	}
	return nil, false
}

func (a *arrivals) twice() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, n := range a.seen {
		if n > 1 {
			return true
		}
	}
	return false
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func kinds(t *testing.T, db *core.DB, kind string) []core.Input {
	t.Helper()
	all, err := db.Inputs(context.Background(), 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	var out []core.Input
	for _, in := range all {
		if in.Kind == kind {
			out = append(out, in)
		}
	}
	return out
}

// pairedClient is a real whatsmeow client, ingest attached, paired with a
// fresh mock and through its first login. buffer false turns the decrypted
// event buffer back off after Attach.
func pairedClient(ctx context.Context, t *testing.T, seed int64, buffer bool, refuse func(core.Input) bool) (*whatsmeow.Client, *flakyLog, *arrivals) {
	t.Helper()
	relay := &qrRelay{}
	srv, err := wamock.New(ctx, wamock.Options{Seed: seed, Scenario: "test:ingest", Login: relay})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	dir := t.TempDir()
	db, err := core.Open(ctx, filepath.Join(dir, "core.db"), core.Options{Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := &flakyLog{DB: db, refuse: refuse, failing: true}

	// opened the way the daemon opens it
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
	New(ctx, log).Attach(cli)
	cli.EnableDecryptedEventBuffer = buffer
	wire := &arrivals{seen: map[string]int{}}
	cli.RawNodeHandler = wire.handler

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
	eventually(t, "a refusal", func() bool { return log.refused() > 0 })
	return cli, log, wire
}

func reconnect(t *testing.T, cli *whatsmeow.Client) {
	t.Helper()
	cli.Disconnect()
	connected := make(chan struct{}, 1)
	id := cli.AddEventHandler(func(evt any) {
		if _, ok := evt.(*events.Connected); ok {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
	})
	defer cli.RemoveEventHandler(id)
	if err := cli.Connect(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(30 * time.Second):
		t.Fatal("no login after reconnecting")
	}
}

func TestARefusedMessageComesAgainAndLandsOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cli, log, _ := pairedClient(ctx, t, 41, true, func(in core.Input) bool { return conversation(in) != "" })
	for _, in := range kinds(t, log.DB, core.KindMessage) {
		if conversation(in) != "" {
			t.Fatal("a refused message is in the log")
		}
	}

	log.set(false)
	reconnect(t, cli)
	eventually(t, "the redelivered message", func() bool {
		for _, in := range kinds(t, log.DB, core.KindMessage) {
			if conversation(in) != "" {
				return true
			}
		}
		return false
	})
	time.Sleep(500 * time.Millisecond) // anything else redelivered would land by now

	msgs := kinds(t, log.DB, core.KindMessage)
	var texts []string
	for _, in := range msgs {
		if text := conversation(in); text != "" {
			texts = append(texts, text)
			var h core.MessageHead
			json.Unmarshal(in.Head, &h)
			if !h.Exact {
				t.Error("the redelivered message lost its exact plaintext")
			}
		}
	}
	if len(texts) != 1 || texts[0] != "waiting for you" {
		t.Fatalf("conversation messages in the log: %q", texts)
	}
	if got := kinds(t, log.DB, core.KindUndecryptable); len(got) != 0 {
		t.Fatalf("%d undecryptable inputs, the buffer should have answered", len(got))
	}
}

// without the buffer the same redelivery cannot be decrypted again: the
// ratchet already moved and whatsmeow drops it as a duplicate. this is what
// the buffer is for.
func TestWithoutTheBufferARefusedMessageIsLost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cli, log, wire := pairedClient(ctx, t, 42, false, func(in core.Input) bool { return conversation(in) != "" })
	log.set(false)
	reconnect(t, cli)
	eventually(t, "the redelivery", wire.twice)
	time.Sleep(500 * time.Millisecond)
	for _, in := range kinds(t, log.DB, core.KindMessage) {
		if text := conversation(in); text != "" {
			t.Fatalf("decrypted %q again, the test proves nothing", text)
		}
	}
}

func TestRefusedAppStateIsFetchedAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cli, log, _ := pairedClient(ctx, t, 43, true, pin)
	for _, in := range kinds(t, log.DB, core.KindAppState) {
		if pin(in) {
			t.Fatal("a refused pin is in the log")
		}
	}
	if version, _, err := cli.Store.AppState.GetAppStateVersion(ctx, "regular_low"); err != nil || version != 0 {
		t.Fatalf("regular_low moved to v%d after its mutations were refused: %v", version, err)
	}

	log.set(false)
	reconnect(t, cli)
	eventually(t, "the pin", func() bool {
		for _, in := range kinds(t, log.DB, core.KindAppState) {
			if pin(in) {
				return true
			}
		}
		return false
	})
	eventually(t, "regular_low to be saved", func() bool {
		version, _, _ := cli.Store.AppState.GetAppStateVersion(ctx, "regular_low")
		return version > 0
	})
	var states int
	for _, in := range kinds(t, log.DB, core.KindSyncState) {
		var h core.SyncStateHead
		json.Unmarshal(in.Head, &h)
		if h.Domain == "app_state:regular_low" {
			states++
		}
	}
	if states == 0 {
		t.Fatal("no sync_state for regular_low")
	}
}
