package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
	"whatevrd/internal/conn"
	"whatevrd/internal/core"
	"whatevrd/internal/ingest"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	"whatevrd/internal/sqlitex"
)

type Options struct {
	Paths app.Paths
	Core  *core.DB
	IDs   *model.IDs
	Live  *live.Hub
	// World is identity now, kept by whoever keeps one. nil reads the model
	World func(context.Context) (*model.World, error)
	Log   zerolog.Logger
	// Hook runs on every whatsmeow client before anything else touches it:
	// captures, the send guard
	Hook func(*whatsmeow.Client)
	// Transport carries the daemon's own http: avatars, stickers, maps,
	// streams, the version fetch. nil is http.DefaultTransport
	Transport http.RoundTripper
	// Network is nil when there is no host network to watch (the mock)
	Network conn.Network
	Wall    func() time.Time
	// Conn hears every connection state, after the live hub has it
	Conn func(conn.Status)
	// Mocked turns off what only makes sense against the real servers
	Mocked bool
	// Notifier shows a notification on the desktop. nil shows none
	Notifier Notifier
	// Media hears how each download from the network ended, nil for one
	// that worked
	Media func(err error)
	// Relink is set when data from the daemon before this core is still
	// here. that link never asked for the whole history or inline contacts,
	// so it is logged out on its first connect and pairing again brings
	// them. Relink runs once nothing is linked, to clear the old data
	Relink func() error
}

// Client is the account on whatsapp: one whatsmeow client at a time, the
// connection machine driving it, and the work the daemon does on its own.
// it speaks the core's terms: chat and person addresses, message ids, model
// rows. everything it learns goes into the log, nothing is kept here.
type Client struct {
	o      Options
	core   *core.DB
	r      *model.Reader
	live   *live.Hub
	ingest *ingest.Ingest
	conn   *conn.Machine
	// relink is old data waiting on a logout, see Options.Relink
	relink atomic.Bool
	log    zerolog.Logger
	http   *http.Client
	ctx    context.Context

	// lifecycle serializes start, logout and close
	lifecycle sync.Mutex

	mu        sync.Mutex
	container *sqlstore.Container
	cli       *whatsmeow.Client
	acct      *account

	versionMu      sync.Mutex
	versionFetched time.Time

	fronts   frontends
	sends    chan struct{}
	media    *media
	avatars  *avatars
	stickers *stickers
	notes    *notes

	// appState serializes our app state patches
	appState sync.Mutex
	older    older
	turns    turns
}

// New opens the session store and makes the first client. nothing connects
// until Run.
func New(ctx context.Context, o Options) (*Client, error) {
	if o.Wall == nil {
		o.Wall = time.Now
	}
	c := &Client{o: o, core: o.Core, r: model.NewReader(o.Core.Read()), live: o.Live, log: o.Log, ctx: ctx,
		http: &http.Client{Transport: o.Transport}, sends: make(chan struct{}, 1)}
	c.relink.Store(o.Relink != nil)
	c.ingest = ingest.New(ctx, o.Core)
	c.ingest.OnDemand = c.olderAnswered
	// Keep-archived holds this device on archived chats by dropping
	// unarchive mutations arriving from WhatsApp; archives still apply, and
	// the fold never sees the pref directly.
	c.ingest.KeepArchived = func() bool { return c.prefs(ctx).GetKeepChatsArchived() }
	// The fold reads this for every tombstone; seed it from stored prefs so a
	// fresh start honors the saved choice before any SetPreferences lands.
	model.SetAntiDelete(c.prefs(ctx).GetAntiDelete())
	c.conn = conn.New(conn.Options{
		Network: o.Network,
		Log:     o.Log.With().Str("module", "conn").Logger(),
		Publish: c.published,
		Login:   c.qrLogin,
		Before:  c.refreshVersion,
		Wall:    o.Wall,
	})
	c.media = newMedia(c)
	c.avatars = newAvatars(c)
	c.stickers = newStickers(c)
	c.notes = newNotes(c)
	container, err := openSessionStore(ctx, o.Paths.SessionDBPath, waLogger(o.Log).Sub("DB"))
	if err != nil {
		return nil, err
	}
	c.container = container
	if err := c.newClient(ctx); err != nil {
		container.Close()
		return nil, err
	}
	return c, nil
}

// Run connects and keeps the account's work going until ctx ends.
func (c *Client) Run(ctx context.Context) {
	if !c.o.Mocked {
		go conn.WatchSleep(ctx, c.log, func() { c.conn.Kick(conn.ReasonResume) })
	}
	c.lifecycle.Lock()
	c.startAccount(ctx)
	c.lifecycle.Unlock()
	c.conn.Run(ctx)
}

func (c *Client) Close() error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.endAccount()
	c.mu.Lock()
	cli, container := c.cli, c.container
	c.mu.Unlock()
	if cli != nil {
		cli.Disconnect()
	}
	c.media.close()
	if container == nil {
		return nil
	}
	return container.Close()
}

// Ingest is where the client logs what whatsmeow hands it.
func (c *Client) Ingest() *ingest.Ingest { return c.ingest }

// Reconnect drops the socket and connects again, asked for by a person.
func (c *Client) Reconnect() { c.conn.Kick(conn.ReasonManual) }

// WantLogin is a frontend showing the login screen: a machine sitting
// logged out looks again at once.
func (c *Client) WantLogin() {
	if c.conn.Status().Kind == conn.LoggedOut {
		c.conn.Kick(conn.ReasonManual)
	}
}

func (c *Client) client() *whatsmeow.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cli
}

// account is the work one logged in account runs. ending it cancels that
// work and waits for it, so nothing writes for an account that is gone.
type account struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (a *account) spawn(fn func(context.Context)) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		fn(a.ctx)
	}()
}

func (c *Client) startAccount(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	a := &account{ctx: ctx, cancel: cancel}
	c.mu.Lock()
	c.acct = a
	c.mu.Unlock()
	a.spawn(c.runSender)
	a.spawn(c.media.run)
	a.spawn(c.avatars.run)
	a.spawn(c.stickers.run)
	a.spawn(c.notes.run)
}

func (c *Client) endAccount() {
	c.mu.Lock()
	a := c.acct
	c.acct = nil
	c.mu.Unlock()
	if a != nil {
		a.cancel()
		a.wg.Wait()
	}
}

// accountCtx is the context for work that outlives the request that
// started it but not the account.
func (c *Client) accountCtx() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.acct == nil {
		ctx, cancel := context.WithCancel(c.ctx)
		cancel()
		return ctx
	}
	return c.acct.ctx
}

func (c *Client) spawn(fn func(context.Context)) {
	c.mu.Lock()
	a := c.acct
	c.mu.Unlock()
	if a != nil {
		a.spawn(fn)
	}
}

// newClient makes a whatsmeow client on the stored device, or a fresh one
// to pair, and hands it to the log and the machine.
func (c *Client) newClient(ctx context.Context) error {
	c.mu.Lock()
	container := c.container
	c.mu.Unlock()
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return err
	}
	if device.ID == nil {
		c.relinked()
	}
	if device.EventBuffer != nil {
		device.EventBuffer = &retryBuffer{EventBuffer: device.EventBuffer, c: c}
	}
	cli := whatsmeow.NewClient(device, waLogger(c.log).Sub("Client"))
	cli.BackgroundEventCtx = ctx
	cli.DisableManualHistorySyncReceipt = true
	cli.AutoTrustIdentity = autoTrustIdentity()
	cli.SetForceActiveDeliveryReceipts(true)
	cli.UseRetryMessageStore = true
	// a message nobody could decrypt is asked for again from the sender, then
	// from our phone
	cli.AutomaticMessageRerequestFromPhone = true
	if c.o.Hook != nil {
		c.o.Hook(cli)
	}
	c.ingest.Attach(cli)
	c.conn.Attach(cli)
	cli.AddEventHandler(func(evt any) { c.event(cli, evt) })
	c.mu.Lock()
	c.cli = cli
	c.mu.Unlock()
	return nil
}

// autoTrustIdentity is whatsapp's own behaviour: a contact who reinstalls
// keeps working. WHATEVRD_AUTO_TRUST_IDENTITY=0 is strict mode.
func autoTrustIdentity() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WHATEVRD_AUTO_TRUST_IDENTITY"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

func (c *Client) published(s conn.Status) {
	c.live.SetConn(s)
	if s.Kind == conn.Online {
		c.signalSender()
	}
	if c.o.Conn != nil {
		c.o.Conn(s)
	}
}

// event is everything whatsmeow says that the log does not keep: the
// moment, not the fact.
func (c *Client) event(cli *whatsmeow.Client, raw any) {
	if c.client() != cli {
		return
	}
	switch evt := raw.(type) {
	case *events.Connected:
		if c.relink.Load() {
			// not on whatsmeow's goroutine: the logout waits on the client
			go c.relinkOld()
			return
		}
		c.live.SetLogin(live.Login{State: live.LoggedIn})
		c.syncPresence(true)
		c.signalSender()
		c.avatars.kick()
		c.stickers.connected()
	case *events.PairSuccess:
		c.live.SetLogin(live.Login{State: live.Pairing, Detail: "QR scanned, pairing"})
	case *events.PairError:
		c.live.SetLogin(live.Login{State: live.LoginFailed, Detail: fmt.Sprintf("Pairing failed: %v", evt.Error)})
	case *events.LoggedOut:
		c.live.SetLogin(live.Login{State: live.LoginFailed, Detail: "Logged out: " + evt.Reason.String()})
		// not on whatsmeow's goroutine: the wipe waits on the client
		go func() {
			if err := c.wipe(context.WithoutCancel(c.ctx), cli, false); err != nil {
				c.log.Error().Err(err).Msg("whatsapp: wipe after a remote logout")
			}
		}()
	case *events.ChatPresence:
		c.live.SetTyping(c.chatKey(evt.Chat), c.personKey(evt.Sender),
			evt.State == types.ChatPresenceComposing, evt.Media == types.ChatPresenceMediaAudio)
	case *events.Presence:
		p := live.Presence{Online: !evt.Unavailable, LastSeen: evt.LastSeen, At: time.Now()}
		c.live.SetPresence(c.personKey(evt.From), p)
	case *events.UserAbout:
		c.live.SetAbout(c.personKey(evt.JID), live.About{Text: evt.Status, At: evt.Timestamp})
	case *events.Picture:
		c.avatars.changed(evt)
	case *events.MediaRetry:
		c.media.retried(evt)
	case *events.Message:
		c.notes.message(evt)
		c.media.arrived(evt)
	case *events.Receipt:
		if evt.IsFromMe && (evt.Type == types.ReceiptTypeRead || evt.Type == types.ReceiptTypeReadSelf) {
			c.notes.read(c.chatKey(evt.Chat))
		}
	case *events.MarkChatAsRead:
		if evt.Action.GetRead() {
			c.notes.read(c.chatKey(evt.JID))
		}
	}
}

// chatKey is the model's key for a chat jid, the jid itself until the world
// knows better. a transient world-load error keeps the raw address rather
// than mixing keyspaces: callers comparing keys must see one scheme.
func (c *Client) chatKey(j types.JID) string {
	addr := j.ToNonAD().String()
	if w, err := c.world(); err == nil {
		return w.Now(model.Norm(addr))
	}
	return addr
}

func (c *Client) personKey(j types.JID) string { return c.chatKey(j) }

func (c *Client) world() (*model.World, error) {
	if c.o.World != nil {
		return c.o.World(c.ctx)
	}
	return c.r.World(c.ctx)
}

// qrLogin is the pairing flow on a device with no session: codes go to the
// login view, and it returns once the phone scanned one or it failed.
func (c *Client) qrLogin(ctx context.Context, cli *whatsmeow.Client) (err error) {
	c.live.SetLogin(live.Login{State: live.ShowingQR, Detail: "Waiting for a QR scan"})
	// GetQRChannel refuses a connected socket: an attempt that ended with one
	// up would wedge every attempt after it
	if cli.IsConnected() {
		cli.Disconnect()
	}
	defer func() {
		if err != nil && cli.IsConnected() {
			cli.Disconnect()
		}
	}()
	qr, err := cli.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("qr channel: %w", err)
	}
	if err := cli.ConnectContext(ctx); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		return fmt.Errorf("connect for qr: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-qr:
			if !ok {
				return errors.New("qr channel closed")
			}
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				c.live.SetLogin(live.Login{State: live.ShowingQR, QR: item.Code, Expires: time.Now().Add(item.Timeout)})
			case whatsmeow.QRChannelSuccess.Event:
				c.live.SetLogin(live.Login{State: live.Pairing, Detail: "QR scanned, pairing"})
				return nil
			case whatsmeow.QRChannelTimeout.Event:
				return conn.ErrRetryNow
			case whatsmeow.QRChannelClientOutdated.Event:
				c.live.SetLogin(live.Login{State: live.LoginFailed, Detail: "WhatsApp says this client is outdated. Update whatevr."})
				return errors.New("client outdated")
			case whatsmeow.QRChannelScannedWithoutMultidevice.Event:
				c.live.SetLogin(live.Login{State: live.LoginFailed, Detail: "Turn on multi-device on the phone and scan again"})
				return errors.New("multi-device off")
			case whatsmeow.QRChannelEventError:
				c.live.SetLogin(live.Login{State: live.LoginFailed, Detail: fmt.Sprintf("QR login failed: %v", item.Error)})
				return fmt.Errorf("qr: %w", item.Error)
			}
		}
	}
}

const (
	versionEvery   = 6 * time.Hour
	versionTimeout = 10 * time.Second
)

// refreshVersion fetches the version whatsapp web is on, at most every
// versionEvery. a failed fetch never holds a connect up.
func (c *Client) refreshVersion(ctx context.Context) {
	if c.o.Mocked {
		return
	}
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if time.Since(c.versionFetched) < versionEvery {
		return
	}
	fctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	v, err := whatsmeow.GetLatestVersion(fctx, &http.Client{Timeout: versionTimeout, Transport: c.o.Transport})
	if err != nil {
		c.log.Warn().Err(err).Msg("whatsapp: latest version")
		return
	}
	store.SetWAVersion(*v)
	c.versionFetched = time.Now()
}

// Logout ends the session on the server and wipes the account here.
func (c *Client) Logout(ctx context.Context) error {
	cli := c.client()
	if cli != nil && cli.Store.ID != nil {
		lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := cli.Logout(lctx); err != nil && !errors.Is(err, store.ErrDeviceDeleted) {
			c.log.Warn().Err(err).Msg("whatsapp: remote logout failed, wiping here anyway")
		}
		cancel()
	}
	return c.wipe(ctx, cli, true)
}

// relinkOld logs out the account the daemon before this core linked
func (c *Client) relinkOld() {
	c.log.Info().Msg("whatsapp: logging out the old daemon's account so pairing again brings the whole history")
	if err := c.Logout(context.WithoutCancel(c.ctx)); err != nil {
		c.log.Error().Err(err).Msg("whatsapp: relink")
	}
}

// relinked clears the old daemon's data once nothing is linked, once
func (c *Client) relinked() {
	if !c.relink.CompareAndSwap(true, false) {
		return
	}
	if err := c.o.Relink(); err != nil {
		c.log.Error().Err(err).Msg("whatsapp: clear the old daemon's data")
		return
	}
	c.log.Info().Msg("whatsapp: cleared the old daemon's data")
}

// wipe forgets the account: the log, the media, the session. the
// preferences are the machine's, they stay. a remote logout comes once per
// client, a stale one finds a newer client and does nothing.
func (c *Client) wipe(ctx context.Context, from *whatsmeow.Client, asked bool) error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	if !asked && c.client() != from {
		return nil
	}
	c.endAccount()
	defer c.startAccount(c.ctx)
	// Read before anything destructive: a failed read aborts the wipe while
	// the old store still exists, instead of silently losing preferences.
	prefs, err := c.r.Prefs(ctx)
	if err != nil {
		c.log.Error().Err(err).Msg("whatsapp: cannot read preferences, aborting wipe")
		return err
	}
	if from != nil {
		from.Disconnect()
		if from.Store.ID != nil {
			if err := from.Store.Delete(ctx); err != nil {
				c.log.Warn().Err(err).Msg("whatsapp: delete the device")
			}
		}
	}
	backup := filepath.Join(c.o.Paths.DataDir, fmt.Sprintf("whatevr-before-logout-%d.db", time.Now().Unix()))
	if err := c.core.Reset(ctx, backup); err != nil {
		return err
	}
	keepNewest(filepath.Join(c.o.Paths.DataDir, "whatevr-before-logout-*.db"), backup)
	if c.o.IDs != nil {
		c.o.IDs.Forget()
	}
	if len(prefs) > 0 {
		if err := c.SetPrefsRaw(ctx, prefs); err != nil {
			c.log.Warn().Err(err).Msg("whatsapp: preferences lost in the wipe")
		}
	}
	c.media.forget()
	c.notes.forget()
	c.avatars.forget()
	c.live.Reset()
	if err := os.RemoveAll(c.o.Paths.MediaCacheDir); err != nil {
		return err
	}
	if err := os.MkdirAll(c.o.Paths.MediaCacheDir, 0o700); err != nil {
		return err
	}
	c.mu.Lock()
	old := c.container
	c.container = nil
	c.mu.Unlock()
	if old != nil {
		if err := old.Close(); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(c.o.Paths.SessionDir); err != nil {
		return err
	}
	if err := os.MkdirAll(c.o.Paths.SessionDir, 0o700); err != nil {
		return err
	}
	container, err := openSessionStore(ctx, c.o.Paths.SessionDBPath, waLogger(c.log).Sub("DB"))
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.container = container
	c.mu.Unlock()
	return c.newClient(c.ctx)
}

// keepNewest removes every file matching glob but keep.
func keepNewest(glob, keep string) {
	matches, _ := filepath.Glob(glob)
	for _, m := range matches {
		if m != keep {
			_ = os.Remove(m)
		}
	}
}

func waLogger(log zerolog.Logger) waLog.Logger {
	return waLog.Zerolog(log.With().Str("module", "whatsmeow").Logger())
}

const sessionDriver = "whatevrd-session"

func init() {
	sql.Register(sessionDriver, &sqlite3.SQLiteDriver{ConnectHook: sqlitex.StablePlans})
}

func openSessionStore(ctx context.Context, path string, log waLog.Logger) (*sqlstore.Container, error) {
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL", filepath.ToSlash(path))
	db, err := sql.Open(sessionDriver, sqlitex.DSN(dsn, 128))
	if err != nil {
		return nil, err
	}
	// whatsmeow writes from many handlers; one connection keeps sqlite from
	// answering busy in a burst
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	container := sqlstore.NewWithDB(db, "sqlite3", log)
	if err := container.Upgrade(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return container, nil
}

// loggedIn is the client when it has a session, or the error a command
// answers without one.
func (c *Client) loggedIn() (*whatsmeow.Client, error) {
	cli := c.client()
	if cli == nil || cli.Store.ID == nil {
		return nil, ErrNotLoggedIn
	}
	return cli, nil
}

// connected is the client when the socket is up.
func (c *Client) connected() (*whatsmeow.Client, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return nil, err
	}
	if !cli.IsConnected() {
		return nil, ErrNotConnected
	}
	return cli, nil
}

// waitLogged waits until everything appended so far is folded, so a command
// answers after its rows moved.
func (c *Client) waitLogged(ctx context.Context) {
	if _, seq := c.core.Progress(); seq > 0 {
		_ = c.core.WaitFolded(ctx, seq)
	}
}

// the kinds of error a command answers with, one per wire code
var (
	ErrNotLoggedIn  = errors.New("not logged in")
	ErrNotConnected = errors.New("not connected")
	ErrNotFound     = errors.New("not found")
	ErrInvalid      = errors.New("invalid")
	ErrRejected     = errors.New("rejected")
	ErrExpired      = errors.New("expired")
	ErrGuarded      = errors.New("guarded")
	ErrIO           = errors.New("io")
)

// Errorf is an error that is one of the kinds above, for the commands to
// map onto the wire.
func Errorf(kind error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", kind, fmt.Sprintf(format, args...))
}
