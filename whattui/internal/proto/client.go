package proto

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// Reconnect delay. Fixed and short on purpose: the daemon is a local
	// process and a dropped socket is usually a restart to ride out, not a
	// remote service to back off from.
	reconnectDelay = time.Second

	// A single protocol object is never remotely this large. The cap stops a
	// wedged peer flooding us with an unframed blob.
	maxLineBytes = 8 << 20

	// How long the daemon gets to answer hello. A socket that connects and
	// then says nothing is otherwise a permanent wedge, because reconnect only
	// runs off a disconnect that is never coming.
	handshakeTimeout = 10 * time.Second

	// Ceilings on what one connection may owe. Every callback fires exactly
	// once, so an unanswered request has to be held until something answers
	// it, and holding an unbounded number of them is how a daemon that stops
	// replying turns into a frontend that grows without limit.
	maxInFlight = 4096
	maxQueued   = 1024
)

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

// ResponseFunc receives exactly one of result or err, exactly once, on the
// client's read goroutine.
type ResponseFunc func(result json.RawMessage, err *Error)

// Client is one connection to whatevrd: NDJSON framing, the hello handshake,
// id-correlated requests, sub-keyed event routing, and reconnect that re-issues
// every live subscription.
//
// Callbacks and sink methods all run on the client's own goroutine, one at a
// time. Everything else is safe to call from anywhere.
type Client struct {
	socketPath string
	name       string

	// OnState fires on every connection state change, on the client
	// goroutine. The UI shows a banner off this; it is the transport's state,
	// which is not the same thing as WhatsApp's.
	OnState func(state State, info *ServerInfo, err error)

	// OnDrain fires once after each batch of frames has been applied, which is
	// exactly when the UI should redraw.
	OnDrain func()

	// OnOpenChat and OnMediaStreamUpdate carry the connection-directed events,
	// the two that arrive with no sub.
	OnOpenChat          func(OpenChat)
	OnMediaStreamUpdate func(MediaStreamUpdate)

	mu       sync.Mutex
	conn     net.Conn
	state    State
	nextID   uint64
	pending  map[uint64]ResponseFunc
	queued   []queuedRequest
	subs     []*Subscription
	bySubID  map[uint64]*Subscription
	info     *ServerInfo
	writeErr error

	batched []ViewSink

	cancel context.CancelFunc
	done   chan struct{}
}

type queuedRequest struct {
	id  uint64
	req []byte
	cb  ResponseFunc
}

// DefaultSocketPath is where whatevrd listens.
func DefaultSocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join("/run/user", fmt.Sprint(os.Getuid()))
	}
	return filepath.Join(dir, "whatevr", "whatevrd.sock")
}

// New returns a client that has not connected yet. name identifies this
// frontend in hello.
func New(socketPath, name string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &Client{
		socketPath: socketPath,
		name:       name,
		pending:    make(map[uint64]ResponseFunc),
		bySubID:    make(map[uint64]*Subscription),
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

// Stop closes the connection and stops reconnecting. It waits for the client
// goroutine, so no callback runs after it returns.
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
func (c *Client) ServerInfo() *ServerInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}
