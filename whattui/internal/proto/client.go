package proto

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

const (
	// Reconnect delay. Fixed and short on purpose: the daemon is a local
	// process and a dropped socket is usually a restart to ride out, not a
	// remote service to back off from.
	reconnectDelay = time.Second

	// How long the daemon gets to answer hello. A socket that connects and
	// then says nothing is otherwise a permanent wedge, because reconnect only
	// runs off a disconnect that is never coming.
	handshakeTimeout = 10 * time.Second

	// What one connection may owe. The daemon runs 32 requests at a time, so
	// past that the writer holds them back; past the queue a request is
	// refused rather than held without limit.
	maxAwaiting    = 32
	maxQueued      = 64
	maxQueuedBytes = 16 << 20
)

// a write the daemon has not taken by then is a wedged daemon. a var so a
// test can stall one without waiting it out
var writeTimeout = 10 * time.Second

// State is where the connection is.
type State int

const (
	Disconnected State = iota
	Connecting
	Handshaking
	Ready
)

func (s State) String() string {
	switch s {
	case Connecting:
		return "connecting"
	case Handshaking:
		return "handshaking"
	case Ready:
		return "ready"
	default:
		return "disconnected"
	}
}

// ResponseFunc receives exactly one of resp or err, exactly once. A daemon's
// answer, a deadline and a lost connection run one at a time; a request
// refused on the spot runs on the caller's goroutine, inside Do.
type ResponseFunc func(resp *v2.Response, err *Error)

// Client is one connection to whatevrd: framing, the hello handshake,
// id-correlated requests with deadlines, sub-keyed updates, and reconnect
// that re-issues every live subscription.
//
// Everything is safe to call from anywhere.
type Client struct {
	socketPath string
	name       string

	// OnState fires on every connection state change. The UI shows a banner
	// off this; it is the socket's state, not WhatsApp's.
	OnState func(state State, info *v2.HelloResult, err error)

	// OnDrain fires once the frames in hand are applied, which is exactly
	// when the UI should redraw.
	OnDrain func()

	// the connection events, the two that arrive with no sub
	OnOpenChat          func(*v2.OpenChat)
	OnMediaStreamUpdate func(*v2.MediaStreamUpdate)

	// serial makes the daemon's answers and the deadlines take turns
	serial sync.Mutex

	nextID atomic.Uint64

	mu    sync.Mutex
	conn  net.Conn
	state State
	info  *v2.HelloResult
	// calls is every request not answered yet, queue the ones not written
	calls       map[uint64]*call
	queue       []*call
	queuedBytes int
	written     int
	wake        chan struct{}
	subs        []*Subscription
	bySub       map[uint64]*Subscription

	cancel context.CancelFunc
	done   chan struct{}
}

type call struct {
	id      uint64
	body    []byte
	cb      ResponseFunc
	timer   *time.Timer
	written bool
	// late hears an answer that came after the deadline
	late func(*v2.Response)
}

// New returns a client that has not connected yet. socketPath "" is the
// default; name identifies this frontend in hello.
func New(socketPath, name string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &Client{
		socketPath: socketPath,
		name:       name,
		calls:      map[uint64]*call{},
		bySub:      map[uint64]*Subscription{},
		wake:       make(chan struct{}, 1),
	}
}

// Start connects and keeps reconnecting until Stop. It returns immediately.
func (c *Client) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	c.mu.Unlock()

	go func() {
		defer close(done)
		for ctx.Err() == nil {
			c.session(ctx)
			select {
			case <-ctx.Done():
			case <-time.After(reconnectDelay):
			}
		}
	}()
}

// Stop closes the connection and stops reconnecting. It waits for the
// session, so no callback runs after it returns.
func (c *Client) Stop() {
	c.mu.Lock()
	cancel, done, conn := c.cancel, c.done, c.conn
	c.cancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if done != nil {
		<-done
	}
}

// State is the current connection state.
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// ServerInfo is what hello answered with, nil until the handshake completes.
func (c *Client) ServerInfo() *v2.HelloResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

// Has reports whether the daemon serves a view or method, by its oneof arm
// name. false until the handshake completes.
func (c *Client) Has(feature string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info != nil && slices.Contains(c.info.GetFeatures(), feature)
}

// SocketPath is where this client dials, resolved.
func (c *Client) SocketPath() string { return c.socketPath }

// NotRunning reports whether a transport error means there is nothing
// listening, as opposed to something listening that went wrong. The two need
// different words: one is a service to start, the other is a bug to report.
func NotRunning(err error) bool {
	return errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, fs.ErrNotExist)
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
