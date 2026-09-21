//go:build whatevr_mock

package wamock

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/util/keys"

	"whatevrd/internal/app"
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
	Logger   *log.Logger
	Scenario string

	// AccountPhone is the number the mock account answers as, in plain digits.
	AccountPhone string
	// AccountName is the push name the account presents.
	AccountName string

	// ScanDelay is how long a published QR sits unscanned before the mock
	// phone picks it up. Zero pairs as soon as the code appears; a few seconds
	// is what you want when the QR screen itself is the thing being looked at.
	ScanDelay time.Duration

	// Login is the daemon's login event stream. The mock needs it to read the
	// QR it is meant to scan, because the adv secret exists nowhere else.
	Login LoginWatcher
}

// LoginWatcher is the slice of app.Daemon the mock depends on. Keeping it an
// interface means the fake server never reaches further into the daemon than
// the QR it has to scan.
type LoginWatcher interface {
	SubscribeLoginEvents() (<-chan app.LoginEvent, func())
}

// Server is the fake WhatsApp Web endpoint. It binds loopback TLS, points the
// process's HTTP transport at itself, and speaks the Noise + binary XMPP
// protocol whatsmeow expects. The daemon above it is unmodified.
type Server struct {
	opts  Options
	log   *log.Logger
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

	// keysReady closes once the client has uploaded the identity and signed
	// prekey the mock needs before it can encrypt anything.
	keysReady chan struct{}
	keysOnce  sync.Once

	mu          sync.Mutex
	sessions    map[*session]struct{}
	peers       map[string]*peer
	closed      bool
	paired      *pairedDevice
	liveSession *session
}

func New(opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = log.New(log.Writer(), "wamock: ", log.LstdFlags)
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
		log:       opts.Logger,
		ident:     ident,
		tlsID:     tlsID,
		rng:       rng,
		account:   account,
		keys:      &clientKeys{},
		keysReady: make(chan struct{}),
		sessions:  make(map[*session]struct{}),
		peers:     make(map[string]*peer),
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
	mux.HandleFunc("/ws/chat", s.handleWS)
	// whatsmeow scrapes a client_revision out of the web.whatsapp.com landing
	// page to decide the version it advertises. Serving it keeps the daemon
	// from retrying a 404 on every connect.
	mux.HandleFunc("/", s.handleRoot)
	s.http = &http.Server{Handler: mux}

	installCertPubKey(s.ident)
	installTransport(s.tlsID, ln.Addr().String())

	go func() {
		if err := s.http.Serve(ln); err != nil && !s.isClosed() {
			s.log.Printf("serve: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	s.log.Printf("fake WhatsApp server on %s (scenario %q, seed %d)", ln.Addr(), s.opts.Scenario, s.opts.Seed)
	return nil
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
		s.log.Printf("websocket accept: %v", err)
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
	}()

	sess.run(r.Context())
}

// accountJID is the address the mock account is reachable at.
func (s *Server) accountJID() types.JID {
	return types.JID{User: s.opts.AccountPhone, Server: types.DefaultUserServer}
}

// waitForQR blocks until the daemon publishes a pairing code. The mock plays
// the phone, and a phone cannot pair without seeing the QR.
func (s *Server) waitForQR(ctx context.Context) (string, error) {
	if s.opts.Login == nil {
		return "", errors.New("no login watcher wired, cannot read the qr")
	}
	events, cancel := s.opts.Login.SubscribeLoginEvents()
	defer cancel()

	deadline := time.NewTimer(qrWait)
	defer deadline.Stop()
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return "", errNoQR
			}
			if evt.Kind == app.LoginEventQR && evt.QRCode != "" {
				return evt.QRCode, nil
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

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.log.Printf("unhandled http %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><body><script>{"client_revision":%d,}</script></body></html>`, mockClientRevision)
}
