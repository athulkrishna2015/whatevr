//go:build whatevr_mock

package wamock

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
)

// testLogin stands in for app.Daemon: the mock needs somewhere to read the QR
// from, and the test plays the part of the daemon that publishes it.
type testLogin struct {
	mu  sync.Mutex
	chs []chan app.LoginEvent
}

func (t *testLogin) SubscribeLoginEvents() (<-chan app.LoginEvent, func()) {
	ch := make(chan app.LoginEvent, 8)
	t.mu.Lock()
	t.chs = append(t.chs, ch)
	t.mu.Unlock()
	return ch, func() {}
}

func (t *testLogin) publish(code string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, ch := range t.chs {
		select {
		case ch <- app.LoginEvent{Kind: app.LoginEventQR, QRCode: code}:
		default:
		}
	}
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// dialMock brings up a mock server and drives a real whatsmeow client through
// pairing into a logged-in session. Everything below the daemon runs exactly as
// it does in production, which is the only reason any of these tests mean
// anything.
func dialMock(ctx context.Context, t *testing.T, opts Options) (*Server, *whatsmeow.Client, <-chan *events.Message) {
	t.Helper()

	login := &testLogin{}
	opts.Login = login
	if opts.Logger == nil {
		opts.Logger = discardLogger()
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	dsn := "file:" + filepath.Join(t.TempDir(), "session.db") + "?_foreign_keys=on"
	container, err := sqlstore.New(ctx, "sqlite3", dsn, waLog.Noop)
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	t.Cleanup(func() { _ = container.Close() })

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	cli := whatsmeow.NewClient(device, waLog.Noop)
	t.Cleanup(cli.Disconnect)

	connected := make(chan struct{})
	messages := make(chan *events.Message, 64)
	var once sync.Once
	cli.AddEventHandler(func(raw any) {
		switch evt := raw.(type) {
		case *events.Connected:
			once.Do(func() { close(connected) })
		case *events.Message:
			select {
			case messages <- evt:
			default:
			}
		}
	})

	qrChan, err := cli.GetQRChannel(ctx)
	if err != nil {
		t.Fatalf("qr channel: %v", err)
	}
	// Relay the code the client renders back to the mock, which is exactly what
	// a phone scanning the screen does.
	go func() {
		for item := range qrChan {
			if item.Event == whatsmeow.QRChannelEventCode {
				login.publish(item.Code)
			}
		}
	}()

	if err := cli.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	select {
	case <-connected:
	case <-ctx.Done():
		t.Fatal("timed out waiting for the client to authenticate")
	}
	return srv, cli, messages
}

// TestPairAndLogin drives a real whatsmeow client through the whole of stage 0:
// the Noise handshake against our fabricated cert chain, QR pairing, the 515
// restart, and the login that follows. If the wire format drifts, this is what
// notices.
func TestPairAndLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	srv, cli, _ := dialMock(ctx, t, Options{Seed: 7})

	if !cli.IsLoggedIn() {
		t.Fatal("client reports it is not logged in after Connected")
	}
	if cli.Store.ID == nil {
		t.Fatal("client has no device id after pairing")
	}
	paired, ok := srv.Paired()
	if !ok {
		t.Fatal("server did not record a pairing")
	}
	if paired.JID.User != srv.opts.AccountPhone {
		t.Errorf("paired as %s, want user %s", paired.JID, srv.opts.AccountPhone)
	}
	if cli.Store.ID.User != srv.opts.AccountPhone {
		t.Errorf("client stored %s, want user %s", cli.Store.ID, srv.opts.AccountPhone)
	}
}

// TestSeedIsDeterministic guards the property golden frames will depend on:
// the same seed has to produce the same key material every run.
func TestSeedIsDeterministic(t *testing.T) {
	first, err := newServerIdentity(newSeededRand(99), time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("first identity: %v", err)
	}
	second, err := newServerIdentity(newSeededRand(99), time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("second identity: %v", err)
	}
	if *first.static.Pub != *second.static.Pub {
		t.Error("same seed produced different static noise keys")
	}
	if *first.root.Pub != *second.root.Pub {
		t.Error("same seed produced different root keys")
	}
	if *first.intermediate.Pub != *second.intermediate.Pub {
		t.Error("same seed produced different intermediate keys")
	}
	// The chain bytes deliberately are not compared: XEdDSA draws a random
	// nonce per signature, so two chains over identical keys differ. Nothing
	// observable depends on those bytes, only on the keys above.

	other, err := newServerIdentity(newSeededRand(100), time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("other identity: %v", err)
	}
	if *first.static.Pub == *other.static.Pub {
		t.Error("different seeds produced the same static noise key")
	}
}
