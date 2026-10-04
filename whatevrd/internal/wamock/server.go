//go:build whatevr_mock

package wamock

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/util/keys"
)

const (
	defaultAccountPhone = "911000000001"
	defaultAccountName  = "whatevr mock"
	qrWait              = 30 * time.Second
)

// Options configures a mock server. Seed pins every key and identifier the
// server generates, so two runs of the same scenario produce the same bytes.
type Options struct {
	Seed     int64
	Scenario string

	// AccountPhone is the number the mock account answers as, in plain digits.
	AccountPhone string
	// AccountName is the push name the account presents.
	AccountName string

	// ScanDelay is how long a published QR sits unscanned before the mock
	// phone picks it up. Zero pairs as soon as the code appears; a few seconds
	// is what you want when the QR screen itself is the thing being looked at.
	ScanDelay time.Duration

	// HistoryDelay spreads the history sync out, one chunk every this long. It
	// overrides whatever the scenario asked for, so a sync that normally
	// finishes before the first frame can be watched.
	HistoryDelay time.Duration

	// OlderDelay is how long the phone takes to answer a request for older
	// history, so the wait for it can be looked at.
	OlderDelay time.Duration

	// Now pins the clock every relative scenario timestamp hangs off. Zero
	// means the real one, which is what you want when looking at the thing; a
	// fixed instant is what you want when comparing frames byte for byte.
	Now time.Time

	// Control is where the quiescence socket is bound. Empty means no control
	// socket, which is the normal case for a human running a scenario.
	Control string

	// Login is the daemon's login event stream. The mock needs it to read the
	// QR it is meant to scan, because the adv secret exists nowhere else.
	Login LoginWatcher

	// Capture plays back a capture directory instead of a scenario, Segment
	// says which daemon run of it. Speed paces it against the recorded clock
	// (0 is as fast as the gates allow) and Gate is how long a push waits for
	// the client to catch up. Socket is the daemon's protocol socket, where
	// the recorded frontend requests go.
	Capture string
	Segment int
	Speed   float64
	Gate    time.Duration
	Socket  string
}

// LoginWatcher is where the mock reads the daemon's QR codes: the mock plays
// the phone, and the adv secret is in the QR and nowhere else.
type LoginWatcher interface {
	// QRCodes hears every QR the daemon shows from now on, until stop
	QRCodes() (codes <-chan string, stop func())
}

// Server is the fake WhatsApp Web endpoint. It binds loopback TLS, points the
// process's HTTP transport at itself, and speaks the Noise + binary XMPP
// protocol whatsmeow expects. The daemon above it is unmodified.
type Server struct {
	opts  Options
	log   zerolog.Logger
	ident *serverIdentity
	tlsID *tlsIdentity
	rng   *seededRand

	ln   net.Listener
	http *http.Server

	// account is the primary device's identity keypair: the "phone" that signs
	// a companion device into the account during pairing.
	account *keys.KeyPair
	keys    *clientKeys

	// world is what the scenario built: contacts, chats and the messages that
	// are meant to already be there.
	world *World

	// media is everything the mock is hosting over http: history sync blobs,
	// avatars, and later the attachments themselves.
	media *mediaStore

	// settings is the account state that is not conversation: privacy, blocks,
	// the about line.
	settings *accountSettings

	// avatars are the generated profile pictures, kept so a re-fetch is
	// answered with the same id.
	avatars *avatarCache

	// stickers is the sticker store: packs, their contents and tray images,
	// built on the first request rather than at boot.
	stickers *stickerCatalogue

	// appState is the server half of the app state sync: the key and the
	// patches the client validates against.
	appState *mockAppState

	// quiet counts what is still in flight, so a test can ask whether the mock
	// has finished talking rather than guessing with a sleep.
	quiet *quiescence

	// control is the quiescence socket, bound only when a harness asks for it.
	control net.Listener

	// keysReady closes once the client has uploaded the identity and signed
	// prekey the mock needs before it can encrypt anything.
	keysReady chan struct{}
	keysOnce  sync.Once

	// replay is set when the run plays a capture
	replay *replay

	// unacked is every message stanza the client has not acked yet, in send
	// order, with the session it went out on. a real server hands them over
	// again on the next connection. held is what came in while nothing was
	// connected, for the next login.
	ackMu   sync.Mutex
	unacked []unacked
	held    []*Msg

	mu          sync.Mutex
	sessions    map[*session]struct{}
	faults      faults
	peers       map[string]*peer
	closed      bool
	paired      *pairedDevice
	liveSession *session
}

func New(ctx context.Context, opts Options) (*Server, error) {
	if !opts.Now.IsZero() {
		SetBootTime(opts.Now)
	}
	now := time.Now()
	rng := newSeededRand(opts.Seed)
	ident, err := newServerIdentity(rng, now)
	if err != nil {
		return nil, fmt.Errorf("build server identity: %w", err)
	}
	tlsID, err := newTLSIdentity(rng, now)
	if err != nil {
		return nil, fmt.Errorf("build tls identity: %w", err)
	}
	account, err := genKeyPair(rng)
	if err != nil {
		return nil, fmt.Errorf("build account identity: %w", err)
	}
	if opts.AccountPhone == "" {
		opts.AccountPhone = defaultAccountPhone
	}
	if opts.AccountName == "" {
		opts.AccountName = defaultAccountName
	}
	srv := &Server{
		opts:      opts,
		log:       zerolog.Ctx(ctx).With().Str("module", "wamock").Logger(),
		ident:     ident,
		tlsID:     tlsID,
		rng:       rng,
		account:   account,
		keys:      &clientKeys{},
		keysReady: make(chan struct{}),
		sessions:  make(map[*session]struct{}),
		peers:     make(map[string]*peer),
		media:     newMediaStore(),
		settings:  newAccountSettings(),
		avatars:   newAvatarCache(),
		stickers:  newStickerCatalogue(),
		appState:  newMockAppState(rng),
		quiet:     newQuiescence(),
	}
	if opts.Capture != "" {
		r, err := loadReplay(srv)
		if err != nil {
			return nil, fmt.Errorf("load capture: %w", err)
		}
		srv.replay = r
		srv.opts.AccountPhone = r.pn.User
		if r.acct.PushName != "" {
			srv.opts.AccountName = r.acct.PushName
		}
		srv.quiet.hold = r.holding
	}
	srv.world = newWorld(srv)
	if scenario, ok := Lookup(opts.Scenario); ok && scenario.Build != nil {
		scenario.Build(srv.world)
	}
	return srv, nil
}

// Start binds the listener and redirects the process at it. It must run before
// the daemon constructs its whatsmeow client, because whatsmeow snapshots
// http.DefaultTransport in NewClient.
func (s *Server) Start(ctx context.Context) error {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{s.tlsID.server},
	})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc(websocketPath, s.handleWS)
	mux.HandleFunc(mediaPathPrefix, s.handleMedia)
	mux.HandleFunc(avatarPathPrefix, s.handleMedia)
	mux.HandleFunc("/mms/", s.handleMMS)
	mux.HandleFunc("/sticker", s.handleStickerStore)
	// whatsmeow scrapes a client_revision out of the web.whatsapp.com landing
	// page to decide the version it advertises. Serving it keeps the daemon
	// from retrying a 404 on every connect.
	mux.HandleFunc("/", s.handleRoot)
	var handler http.Handler = mux
	var hosts []string
	if r := s.replay; r != nil {
		hosts = r.hosts()
		handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == websocketPath || !r.serveHTTP(w, req) {
				mux.ServeHTTP(w, req)
			}
		})
	}
	s.http = &http.Server{Handler: s.countHTTP(handler)}

	installCertPubKey(s.ident)
	installTransport(s.tlsID, ln.Addr().String(), hosts, s.dialFault)

	go func() {
		if err := s.http.Serve(ln); err != nil && !s.isClosed() {
			s.log.Error().Err(err).Msg("serve")
		}
	}()
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	if s.replay != nil {
		go s.replay.run(ctx)
	}
	s.log.Info().Stringer("addr", ln.Addr()).Str("scenario", s.opts.Scenario).Int64("seed", s.opts.Seed).Msg("fake WhatsApp server up")
	return nil
}

// countHTTP keeps the quiescence tracker honest about downloads. A history
// chunk or an attachment the daemon is still fetching is work in flight, and a
// frame taken while it lands is not the frame a rerun would take.
func (s *Server) countHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The websocket is a request that never returns, so counting it would
		// mean the mock is busy for as long as anybody is connected.
		if r.URL.Path == websocketPath {
			next.ServeHTTP(w, r)
			return
		}
		s.quiet.addHTTP(1)
		defer s.quiet.addHTTP(-1)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	sessions := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.close(websocket.StatusGoingAway, "server shutting down")
	}
	if s.control != nil {
		_ = s.control.Close()
	}
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.http.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The client sends Origin: https://web.whatsapp.com, which is not the
		// Host we are actually serving.
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionContextTakeover,
	})
	if err != nil {
		s.log.Warn().Err(err).Msg("websocket accept")
		return
	}
	conn.SetReadLimit(frameMaxSize)

	sess := newSession(s, conn)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		sess.close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	s.sessions[sess] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.clearLive(sess)
		s.mu.Lock()
		delete(s.sessions, sess)
		s.mu.Unlock()
		s.orphaned(sess)
	}()

	sess.run(r.Context())
}

// accountJID is the address the mock account is reachable at.
func (s *Server) accountJID() types.JID {
	return types.JID{User: s.opts.AccountPhone, Server: types.DefaultUserServer}
}

// lidOf is lidFor, except that a replay's account has the lid the capture
// gave it: a made up one would be a second lid for the same number.
func (s *Server) lidOf(j types.JID) types.JID {
	if r := s.replay; r != nil && !r.lid.IsEmpty() && j.User == r.pn.User {
		lid := r.lid.ToNonAD()
		lid.Device = j.Device
		return lid
	}
	return lidFor(j)
}

// waitForQR blocks until the daemon publishes a pairing code. The mock plays
// the phone, and a phone cannot pair without seeing the QR.
func (s *Server) waitForQR(ctx context.Context) (string, error) {
	if s.opts.Login == nil {
		return "", errors.New("no login watcher wired, cannot read the qr")
	}
	codes, stop := s.opts.Login.QRCodes()
	defer stop()

	deadline := time.NewTimer(qrWait)
	defer deadline.Stop()
	for {
		select {
		case code, ok := <-codes:
			if !ok {
				return "", errNoQR
			}
			if code != "" {
				return code, nil
			}
		case <-deadline.C:
			return "", errNoQR
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func (s *Server) notePaired(dev pairedDevice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paired = &dev
}

// forgetPairing drops the linked device and everything that belonged to it.
// A logout wipes the daemon's database, so the account it comes back to has to
// be a fresh one: the old Signal sessions are keyed to a client that no longer
// exists, and the world's backlog was already spent on the previous link.
func (s *Server) forgetPairing() {
	s.mu.Lock()
	s.paired = nil
	s.peers = make(map[string]*peer)
	s.keys = &clientKeys{}
	s.keysReady = make(chan struct{})
	s.keysOnce = sync.Once{}
	s.appState = newMockAppState(s.rng)
	s.avatars = newAvatarCache()
	s.stickers = newStickerCatalogue()
	world := newWorld(s)
	s.world = world
	s.mu.Unlock()

	if scenario, ok := Lookup(s.opts.Scenario); ok && scenario.Build != nil {
		scenario.Build(world)
	}
}

// Paired reports the device a completed pairing produced, if any.
func (s *Server) Paired() (pairedDevice, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paired == nil {
		return pairedDevice{}, false
	}
	return *s.paired, true
}

// mockClientRevision is the build number the mock claims to be. It is pinned
// rather than current: a scenario should not change behaviour because the real
// WhatsApp shipped a release.
const mockClientRevision = 1023000000

// websocketPath is where the client connects. socket.URL is a const in
// whatsmeow, so the path is fixed too.
const websocketPath = "/ws/chat"

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.log.Warn().Str("method", r.Method).Str("path", r.URL.Path).Msg("unhandled http")
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><body><script>{"client_revision":%d,}</script></body></html>`, mockClientRevision)
}
