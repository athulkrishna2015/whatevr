//go:build whatevr_mock

package conn

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/wamock"
)

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

// watched is a machine under test with every status it published.
type watched struct {
	*Machine
	srv *wamock.Server
	cli *whatsmeow.Client

	mu   sync.Mutex
	seen []Status
}

func (w *watched) statuses() []Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Status(nil), w.seen...)
}

// until waits for a status that matches, published after from, and says how
// long it took.
func (w *watched) until(t *testing.T, from int, within time.Duration, what string, ok func(Status) bool) (time.Duration, int) {
	t.Helper()
	start := time.Now()
	for time.Since(start) < within {
		ss := w.statuses()
		for i := from; i < len(ss); i++ {
			if ok(ss[i]) {
				return time.Since(start), i + 1
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %s within %s; saw %s", what, within, trail(w.statuses()[from:]))
	return 0, 0
}

func trail(ss []Status) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString("\n  " + string(s.Kind) + ": " + s.Detail)
	}
	return b.String()
}

func online(s Status) bool { return s.Kind == Online }

// fast are timings small enough for a test and in the same proportions as
// the real ones.
var fast = Options{
	Success:    3 * time.Second,
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: time.Second,
	Probe:      time.Second,
	Silence:    3 * time.Second,
	Jump:       3 * time.Second,
	Tick:       100 * time.Millisecond,
}

// paired is a client paired with a fresh mock and handed to a running
// machine.
func paired(t *testing.T, opts Options) *watched {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	relay := &qrRelay{}
	srv, err := wamock.New(ctx, wamock.Options{Seed: 3, Scenario: "empty", Login: relay})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "session.db")+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Noop)
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
	deadline := time.Now().Add(30 * time.Second)
	for !cli.IsLoggedIn() {
		if time.Now().After(deadline) {
			t.Fatal("pairing never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cli.Disconnect()

	w := &watched{srv: srv, cli: cli}
	opts.Log = zerolog.Nop()
	opts.Wall = wamock.Wall
	opts.Publish = func(s Status) {
		w.mu.Lock()
		w.seen = append(w.seen, s)
		w.mu.Unlock()
	}
	w.Machine = New(opts)
	w.Attach(cli)
	go w.Run(ctx)
	w.until(t, 0, 10*time.Second, "first online", online)
	return w
}

// an unpaired device has to say so, or no frontend knows to show the code
func TestAnUnpairedDevicePublishesNeedLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "session.db")+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Close() })
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		t.Fatal(err)
	}

	w := &watched{cli: whatsmeow.NewClient(device, waLog.Noop)}
	opts := fast
	opts.Log = zerolog.Nop()
	opts.Wall = wamock.Wall
	opts.Publish = func(s Status) {
		w.mu.Lock()
		w.seen = append(w.seen, s)
		w.mu.Unlock()
	}
	opts.Login = func(ctx context.Context, _ *whatsmeow.Client) error {
		<-ctx.Done()
		return ctx.Err()
	}
	w.Machine = New(opts)
	w.Attach(w.cli)
	go w.Run(ctx)
	w.until(t, 0, 5*time.Second, "need login", func(s Status) bool { return s.Kind == NeedLogin })
}

func (w *watched) fault(t *testing.T, f wamock.Fault, ms int) {
	t.Helper()
	if err := w.srv.Fault(f, ms); err != nil {
		t.Fatal(err)
	}
}

// back says the machine got back online after a fault, without being told,
// within the time one backoff and one attempt take.
func (w *watched) back(t *testing.T, from int) {
	t.Helper()
	bound := w.opts.MaxBackoff + w.opts.Success + 2*time.Second
	took, _ := w.until(t, from, bound, "online again", online)
	t.Logf("online again %s after the fault cleared", took.Round(10*time.Millisecond))
	if !w.cli.IsLoggedIn() {
		t.Fatal("published online with the client logged out")
	}
}

func TestADroppedSocketComesBack(t *testing.T) {
	w := paired(t, fast)
	from := len(w.statuses())
	w.fault(t, wamock.FaultDrop, 0)
	_, from = w.until(t, from, 5*time.Second, "a reconnect", func(s Status) bool { return s.Kind != Online })
	w.back(t, from-1)
}

func TestRefusedDialsBackOffThenRecover(t *testing.T) {
	w := paired(t, fast)
	from := len(w.statuses())
	w.fault(t, wamock.FaultRefuse, 0)
	w.fault(t, wamock.FaultDrop, 0)
	_, from = w.until(t, from, 5*time.Second, "a backoff after a refused dial", func(s Status) bool {
		return s.Kind == Waiting && s.Attempt >= 2 && !s.Next.IsZero() && s.Manual
	})
	if s := w.Status(); s.Kind == Online {
		t.Fatal("online while every dial is refused")
	}
	w.fault(t, wamock.FaultClear, 0)
	w.back(t, from)
}

func TestABlackHoledDialIsCutAtTheDeadline(t *testing.T) {
	w := paired(t, fast)
	from := len(w.statuses())
	w.fault(t, wamock.FaultBlackhole, 0)
	w.fault(t, wamock.FaultDrop, 0)
	// the mock holds a dial for two minutes, the deadline has to cut it
	took, from := w.until(t, from, w.opts.Success+3*time.Second, "the attempt torn down", func(s Status) bool {
		return s.Kind == Waiting && strings.Contains(s.Detail, "No answer")
	})
	t.Logf("black hole cut after %s", took.Round(10*time.Millisecond))
	w.fault(t, wamock.FaultClear, 0)
	w.back(t, from)
}

func TestAServerThatNeverSaysSuccessIsLeft(t *testing.T) {
	w := paired(t, fast)
	from := len(w.statuses())
	w.fault(t, wamock.FaultStall, 0)
	w.fault(t, wamock.FaultDrop, 0)
	_, from = w.until(t, from, w.opts.Success+3*time.Second, "the stalled attempt torn down", func(s Status) bool {
		return s.Kind == Waiting && strings.Contains(s.Detail, "No answer")
	})
	w.fault(t, wamock.FaultClear, 0)
	w.back(t, from)
}

func TestASilentSocketIsNoticed(t *testing.T) {
	w := paired(t, fast)
	from := len(w.statuses())
	w.fault(t, wamock.FaultSilent, 0)
	_, from = w.until(t, from, w.opts.Silence+3*time.Second, "the silence noticed", func(s Status) bool {
		return s.Kind == Connecting && strings.Contains(s.Detail, "Nothing heard")
	})
	w.back(t, from-1)
}

func TestAClockJumpProbesTheSocket(t *testing.T) {
	slow := fast
	// only the jump can find it in time
	slow.Silence = time.Minute
	w := paired(t, slow)
	from := len(w.statuses())
	w.fault(t, wamock.FaultSilent, 0)
	w.fault(t, wamock.FaultClockJump, int((10 * time.Minute).Milliseconds()))
	_, from = w.until(t, from, slow.Probe+3*time.Second, "a probe after the jump", func(s Status) bool {
		return s.Kind == Connecting && strings.Contains(s.Detail, string(ReasonClock))
	})
	w.back(t, from-1)
}

func TestANetworkChangeProbesTheSocket(t *testing.T) {
	slow := fast
	slow.Silence = time.Minute
	w := paired(t, slow)
	from := len(w.statuses())
	w.fault(t, wamock.FaultSilent, 0)
	w.Kick(ReasonNetwork)
	_, from = w.until(t, from, slow.Probe+3*time.Second, "a probe after the change", func(s Status) bool {
		return s.Kind == Connecting && strings.Contains(s.Detail, string(ReasonNetwork))
	})
	w.back(t, from-1)
}

func TestAProbeOfAHealthySocketLeavesItAlone(t *testing.T) {
	w := paired(t, fast)
	from := len(w.statuses())
	w.Kick(ReasonResume)
	time.Sleep(fast.Probe + time.Second)
	if ss := w.statuses()[from:]; len(ss) != 0 {
		t.Fatalf("a healthy socket moved:%s", trail(ss))
	}
}

func TestAManualReconnectReconnects(t *testing.T) {
	w := paired(t, fast)
	from := len(w.statuses())
	w.Kick(ReasonManual)
	_, from = w.until(t, from, 3*time.Second, "a new attempt", func(s Status) bool { return s.Kind == Connecting })
	w.back(t, from)
}

func TestAKickCutsTheBackoffShort(t *testing.T) {
	slow := fast
	slow.MinBackoff, slow.MaxBackoff = 20*time.Second, 40*time.Second
	w := paired(t, slow)
	from := len(w.statuses())
	w.fault(t, wamock.FaultRefuse, 0)
	w.fault(t, wamock.FaultDrop, 0)
	_, from = w.until(t, from, 5*time.Second, "a long backoff", func(s Status) bool { return s.Kind == Waiting })
	w.fault(t, wamock.FaultClear, 0)
	w.Kick(ReasonSend)
	took, _ := w.until(t, from, 5*time.Second, "online at once", online)
	if took > 3*time.Second {
		t.Fatalf("a send waited %s on a 20 s backoff", took)
	}
}
