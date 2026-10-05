// Package conn owns the whatsmeow connection: it is the only thing that
// connects, tears down or says what state the connection is in.
//
// an attempt has to reach the server's success within Options.Success or it
// is torn down. online stays online while frames come in; a keepalive that
// times out with nothing heard, a probe that goes unanswered after a network
// change, a resume or a clock jump, or a long silence tears it down. failed
// attempts back off from MinBackoff to MaxBackoff with jitter, and any
// trigger (network up, resume, clock jump, a send, a manual reconnect) cuts
// the wait short.
package conn

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types/events"
)

// Kind is what the connection is doing.
type Kind string

const (
	// Starting is before the first attempt.
	Starting Kind = "starting"
	// NoNetwork is no route out. attempts still run at MaxBackoff in case
	// the route check is wrong.
	NoNetwork Kind = "no_network"
	// Connecting is an attempt in flight.
	Connecting Kind = "connecting"
	Online     Kind = "online"
	// Waiting is the pause after a failed attempt, Next says until when.
	Waiting Kind = "waiting"
	// NeedLogin is a device with no session: the login flow owns the screen.
	NeedLogin Kind = "need_login"
	// LoggedOut is the phone or the server ending the session.
	LoggedOut Kind = "logged_out"
	// Banned is a temporary ban, Next is when it ends if the server said.
	Banned Kind = "banned"
	// Outdated is the server refusing this client version.
	Outdated Kind = "outdated"
	// Replaced is another client taking the session. it stays down until a
	// reconnect is asked for, or two clients would take turns forever.
	Replaced Kind = "replaced"
)

// Status is one state with what goes with it. Manual says a reconnect is
// worth asking for.
type Status struct {
	Kind    Kind
	Since   time.Time
	Detail  string
	Attempt int
	Next    time.Time
	Manual  bool
	// Cause is what the last attempt or connection ended on, while connecting
	// or waiting
	Cause Cause
}

// Cause is how an attempt or a connection ended.
type Cause string

const (
	// CauseUnreachable is a dial that failed: no route, refused, dns.
	CauseUnreachable Cause = "unreachable"
	// CauseNoAnswer is a socket that never got to success.
	CauseNoAnswer Cause = "no_answer"
	// CauseRefused is the server saying no at login.
	CauseRefused Cause = "refused"
	// CauseSilent is a connection that went quiet: keepalive, probe, silence.
	CauseSilent Cause = "silent"
	// CauseClosed is a socket that closed.
	CauseClosed Cause = "closed"
)

// Reason is why the machine was kicked.
type Reason string

const (
	ReasonNetwork Reason = "network change"
	ReasonResume  Reason = "resume"
	ReasonClock   Reason = "clock jump"
	ReasonSend    Reason = "send"
	ReasonManual  Reason = "reconnect asked"
)

// Network says whether there is a route out and when that may have changed.
type Network interface {
	Up() bool
	Changes() <-chan struct{}
}

type Options struct {
	Log zerolog.Logger
	// Publish gets every status, in order, on the machine's goroutine.
	Publish func(Status)
	// Login runs the pairing flow for a device with no session. it owns the
	// state it shows while it runs and returns once pairing is done or failed.
	// ErrRetryNow from it skips the backoff.
	Login func(ctx context.Context, cli *whatsmeow.Client) error
	// Before runs ahead of each attempt.
	Before func(ctx context.Context)
	// Wall is the wall clock watched for jumps, time.Now when nil.
	Wall    func() time.Time
	Network Network

	Success    time.Duration
	MinBackoff time.Duration
	MaxBackoff time.Duration
	// Probe is how long a ping may take before the socket counts as dead.
	Probe time.Duration
	// Silence is how long online may go with nothing heard.
	Silence time.Duration
	// Jump is how far wall and monotonic time may drift apart between two
	// looks before it counts as a suspend or a clock change.
	Jump time.Duration
	// Tick is how often silence and the clock are looked at.
	Tick time.Duration
}

// ErrRetryNow is a login outcome that needs a new attempt at once, as an
// expired qr code.
var ErrRetryNow = errors.New("retry at once")

func (o *Options) defaults() {
	set := func(d *time.Duration, v time.Duration) {
		if *d == 0 {
			*d = v
		}
	}
	set(&o.Success, 30*time.Second)
	set(&o.MinBackoff, time.Second)
	set(&o.MaxBackoff, 60*time.Second)
	set(&o.Probe, 8*time.Second)
	set(&o.Silence, 75*time.Second)
	set(&o.Jump, 10*time.Second)
	set(&o.Tick, 2*time.Second)
	if o.Wall == nil {
		o.Wall = time.Now
	}
	if o.Publish == nil {
		o.Publish = func(Status) {}
	}
}

type event struct {
	cli *whatsmeow.Client
	evt any
}

type Machine struct {
	opts  Options
	start time.Time

	mu     sync.Mutex
	cli    *whatsmeow.Client
	status Status
	// attached wakes a machine waiting for its first client
	attached chan struct{}

	events chan event
	kicks  chan Reason
	// heard is the monotonic offset from start of the last inbound frame
	heard atomic.Int64
}

func New(opts Options) *Machine {
	opts.defaults()
	return &Machine{
		opts:     opts,
		start:    time.Now(),
		status:   Status{Kind: Starting, Since: opts.Wall()},
		attached: make(chan struct{}, 1),
		events:   make(chan event, 256),
		kicks:    make(chan Reason, 8),
	}
}

// Attach hands the machine a client. it turns whatsmeow's own reconnecting
// off and follows the newest client: one made after a logout replaces the
// last.
func (m *Machine) Attach(cli *whatsmeow.Client) {
	cli.EnableAutoReconnect = false
	cli.InitialAutoReconnect = false
	prev := cli.RawNodeHandler
	cli.RawNodeHandler = func(ctx context.Context, raw whatsmeow.RawNode) (*waBinary.Node, bool) {
		m.heard.Store(int64(time.Since(m.start)))
		if prev != nil {
			return prev(ctx, raw)
		}
		return nil, false
	}
	cli.AddEventHandler(func(evt any) {
		switch evt.(type) {
		case *events.Connected, *events.Disconnected, *events.LoggedOut, *events.StreamReplaced,
			*events.TemporaryBan, *events.ClientOutdated, *events.ConnectFailure,
			*events.KeepAliveTimeout, *events.KeepAliveRestored:
		default:
			return
		}
		select {
		case m.events <- event{cli, evt}:
		default:
			m.opts.Log.Warn().Type("event", evt).Msg("conn: event queue full, dropped")
		}
	})
	m.mu.Lock()
	m.cli = cli
	m.mu.Unlock()
	select {
	case m.attached <- struct{}{}:
	default:
	}
}

// Kick asks for a look now: a new attempt when down, a probe when up, a
// fresh connection when manual.
func (m *Machine) Kick(r Reason) {
	select {
	case m.kicks <- r:
	default:
	}
}

// Status is the current state.
func (m *Machine) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Machine) client() *whatsmeow.Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cli
}

func (m *Machine) set(s Status) {
	s.Since = m.opts.Wall()
	m.mu.Lock()
	prev := m.status
	if prev.Kind == s.Kind && prev.Detail == s.Detail && prev.Attempt == s.Attempt && prev.Next.Equal(s.Next) && prev.Manual == s.Manual && prev.Cause == s.Cause {
		m.mu.Unlock()
		return
	}
	if prev.Kind == s.Kind {
		s.Since = prev.Since
	}
	m.status = s
	m.mu.Unlock()
	ev := m.opts.Log.Info()
	if s.Kind == Online || s.Kind == Connecting {
		ev = m.opts.Log.Debug()
	}
	ev.Str("state", string(s.Kind)).Str("cause", string(s.Cause)).Str("detail", s.Detail).Int("attempt", s.Attempt).Time("next", s.Next).Msg("conn: state")
	m.opts.Publish(s)
}

// heardAgo is how long since the last inbound frame.
func (m *Machine) heardAgo() time.Duration {
	return time.Since(m.start) - time.Duration(m.heard.Load())
}

// Backoff is the wait before attempt n (n >= 1): doubling from min, capped
// at max, the upper half of it random so clients that failed together do not
// come back together.
func Backoff(n int, min, max time.Duration, r func() float64) time.Duration {
	d := min
	for i := 1; i < n && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d/2 + time.Duration(r()*float64(d/2))
}

// outcome is how one phase ended.
type outcome struct {
	next   phase
	detail string
	cause  Cause
	// fresh resets the backoff: conditions changed, the next try is a first
	fresh bool
}

type phase int

const (
	phaseConnect phase = iota
	phaseWait
	phaseLogin
	phaseHold
	phaseGone
	// phaseAdopt watches a socket whatsmeow brought up on its own
	phaseAdopt
)

// Run is the machine. it returns when ctx ends.
func (m *Machine) Run(ctx context.Context) {
	go m.watchClock(ctx)
	var netChanges <-chan struct{}
	if m.opts.Network != nil {
		netChanges = m.opts.Network.Changes()
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-netChanges:
				if !ok {
					return
				}
				m.Kick(ReasonNetwork)
			}
		}
	}()

	select {
	case <-ctx.Done():
		return
	case <-m.attached:
	}

	attempt := 0
	next := phaseConnect
	var detail string
	var cause Cause
	var hold Status
	for ctx.Err() == nil {
		var o outcome
		switch next {
		case phaseConnect:
			cli := m.client()
			if cli.Store.ID == nil {
				o = outcome{next: phaseLogin}
				break
			}
			if m.opts.Network != nil && !m.opts.Network.Up() {
				o = m.noNetwork(ctx, attempt)
				break
			}
			attempt++
			o, hold = m.connect(ctx, cli, attempt, detail, cause)
		case phaseLogin:
			o = m.login(ctx)
		case phaseWait:
			o = m.wait(ctx, attempt, detail, cause)
		case phaseHold:
			o = m.hold(ctx, hold)
		case phaseGone:
			o = m.gone(ctx)
		case phaseAdopt:
			cli := m.client()
			m.heard.Store(int64(time.Since(m.start)))
			o, hold = m.online(ctx, cli, cli.Disconnect)
		}
		if o.fresh {
			attempt = 0
		}
		next, detail, cause = o.next, o.detail, o.cause
	}
}

func (m *Machine) noNetwork(ctx context.Context, attempt int) outcome {
	until := m.opts.Wall().Add(m.opts.MaxBackoff)
	m.set(Status{Kind: NoNetwork, Detail: "No network", Attempt: attempt, Next: until, Manual: true})
	t := time.NewTimer(m.opts.MaxBackoff)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return outcome{}
		case e := <-m.events:
			if o, ok := m.idleEvent(e); ok {
				return o
			}
		case r := <-m.kicks:
			if r == ReasonManual || m.opts.Network.Up() {
				return outcome{next: phaseConnect, fresh: true}
			}
		case <-t.C:
			// the route check may be wrong; try anyway, as often as the cap
			return outcome{next: phaseConnect}
		}
	}
}

// connect is one attempt and, if it works, the whole time it stays online.
func (m *Machine) connect(ctx context.Context, cli *whatsmeow.Client, attempt int, why string, cause Cause) (outcome, Status) {
	detail := "Connecting to WhatsApp"
	if why != "" {
		detail = "Reconnecting: " + why
	}
	m.set(Status{Kind: Connecting, Detail: detail, Attempt: attempt, Cause: cause})
	if m.opts.Before != nil {
		m.opts.Before(ctx)
	}
	// whatever is queued came from an older socket
	m.drain()
	// the socket lives as long as this context: cancelling it is the one
	// teardown that also cuts a dial or a handshake short
	actx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	var connErr error
	go func() {
		connErr = cli.ConnectContext(actx)
		close(finished)
	}()
	teardown := func() {
		cancel()
		cli.Disconnect()
		// the next attempt must not start under this one's socket lock
		select {
		case <-finished:
		case <-time.After(m.opts.Success):
			m.opts.Log.Warn().Msg("conn: an attempt outlived its teardown")
		}
	}
	deadline := time.NewTimer(m.opts.Success)
	defer deadline.Stop()
	returned := finished

	// IsLoggedIn stays true after a socket dies, only this attempt's own
	// Connected says it worked
	for connected := false; !connected; {
		select {
		case <-ctx.Done():
			teardown()
			return outcome{}, Status{}
		case <-returned:
			returned = nil
			if connErr != nil && !errors.Is(connErr, whatsmeow.ErrAlreadyConnected) {
				teardown()
				return outcome{next: phaseWait, detail: describe(connErr), cause: CauseUnreachable}, Status{}
			}
			// the socket is up, success has to follow
		case e := <-m.events:
			if e.cli != cli {
				continue
			}
			if _, ok := e.evt.(*events.Connected); ok {
				connected = true
				continue
			}
			if o, h, ok := m.fatal(e.evt); ok {
				teardown()
				return o, h
			}
			if _, ok := e.evt.(*events.Disconnected); ok && !cli.IsConnected() {
				teardown()
				return outcome{next: phaseWait, detail: "Connection closed while logging in", cause: CauseNoAnswer}, Status{}
			}
		case r := <-m.kicks:
			if r == ReasonManual {
				teardown()
				return outcome{next: phaseConnect, fresh: true}, Status{}
			}
		case <-deadline.C:
			teardown()
			return outcome{next: phaseWait, detail: fmt.Sprintf("No answer from WhatsApp within %s", m.opts.Success), cause: CauseNoAnswer}, Status{}
		}
	}
	m.heard.Store(int64(time.Since(m.start)))
	return m.online(ctx, cli, teardown)
}

func (m *Machine) online(ctx context.Context, cli *whatsmeow.Client, teardown func()) (outcome, Status) {
	m.set(Status{Kind: Online, Detail: "Connected to WhatsApp"})
	tick := time.NewTicker(m.opts.Tick)
	defer tick.Stop()
	probe := make(chan error, 1)
	probing := false
	var probeWhy Reason
	lost := func(why string, cause Cause) (outcome, Status) {
		teardown()
		m.opts.Log.Info().Str("why", why).Msg("conn: connection lost")
		// a connection that worked is a first try again
		return outcome{next: phaseConnect, fresh: true, detail: why, cause: cause}, Status{}
	}
	for {
		select {
		case <-ctx.Done():
			teardown()
			return outcome{}, Status{}
		case e := <-m.events:
			if e.cli != cli {
				continue
			}
			switch e.evt.(type) {
			case *events.Disconnected:
				// whatsmeow sends these from its own goroutine; one for a socket
				// already replaced can come late
				if !cli.IsConnected() {
					return lost("Connection closed", CauseClosed)
				}
			case *events.KeepAliveTimeout:
				if m.heardAgo() > whatsmeow.KeepAliveResponseDeadline {
					return lost("Keepalive lost", CauseSilent)
				}
			}
			if o, h, ok := m.fatal(e.evt); ok {
				teardown()
				return o, h
			}
		case r := <-m.kicks:
			if r == ReasonManual {
				teardown()
				return outcome{next: phaseConnect, fresh: true}, Status{}
			}
			if !probing {
				probing, probeWhy = true, r
				go func() { probe <- cli.Ping(ctx, m.opts.Probe) }()
			}
		case err := <-probe:
			probing = false
			if err != nil && ctx.Err() == nil {
				return lost(fmt.Sprintf("No answer after %s", probeWhy), CauseSilent)
			}
		case <-tick.C:
			if !cli.IsConnected() {
				return lost("Socket closed", CauseClosed)
			}
			if ago := m.heardAgo(); ago > m.opts.Silence {
				return lost(fmt.Sprintf("Nothing heard for %s", ago.Round(time.Second)), CauseSilent)
			}
		}
	}
}

// fatal is an event that ends trying on its own: the session is over, the
// account is held, or another client took over.
func (m *Machine) fatal(evt any) (outcome, Status, bool) {
	switch e := evt.(type) {
	case *events.LoggedOut:
		return outcome{next: phaseGone, fresh: true}, Status{}, true
	case *events.StreamReplaced:
		s := Status{Kind: Replaced, Detail: "WhatsApp was opened on another computer", Manual: true}
		return outcome{next: phaseHold}, s, true
	case *events.ClientOutdated:
		s := Status{Kind: Outdated, Detail: "WhatsApp says this client is outdated. Update whatevr."}
		return outcome{next: phaseHold}, s, true
	case *events.TemporaryBan:
		s := Status{Kind: Banned, Detail: e.String()}
		if e.Expire > 0 {
			s.Next = m.opts.Wall().Add(e.Expire)
		}
		return outcome{next: phaseHold}, s, true
	case *events.ConnectFailure:
		return outcome{next: phaseWait, detail: fmt.Sprintf("WhatsApp refused the connection: %s", e.Reason), cause: CauseRefused}, Status{}, true
	}
	return outcome{}, Status{}, false
}

// idleEvent is an event met while not connecting. a socket whatsmeow brought
// up on its own (after pairing) is adopted and watched like any other: one
// nobody watches is how the old core sat "online" on a dead line.
func (m *Machine) idleEvent(e event) (outcome, bool) {
	if e.cli != m.client() {
		return outcome{}, false
	}
	if _, ok := e.evt.(*events.Connected); ok && e.cli.IsConnected() {
		return outcome{next: phaseAdopt, fresh: true}, true
	}
	return outcome{}, false
}

func (m *Machine) wait(ctx context.Context, attempt int, detail string, cause Cause) outcome {
	d := Backoff(max(attempt, 1), m.opts.MinBackoff, m.opts.MaxBackoff, rand.Float64)
	m.set(Status{Kind: Waiting, Detail: detail, Attempt: attempt, Next: m.opts.Wall().Add(d), Manual: true, Cause: cause})
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return outcome{}
		case e := <-m.events:
			if o, ok := m.idleEvent(e); ok {
				return o
			}
		case r := <-m.kicks:
			// a send is in a hurry, not a reason to think the next try works
			return outcome{next: phaseConnect, fresh: r != ReasonSend}
		case <-t.C:
			return outcome{next: phaseConnect}
		}
	}
}

// hold sits in a state only a person or time ends.
func (m *Machine) hold(ctx context.Context, s Status) outcome {
	m.set(s)
	var expire <-chan time.Time
	if !s.Next.IsZero() {
		t := time.NewTimer(s.Next.Sub(m.opts.Wall()))
		defer t.Stop()
		expire = t.C
	}
	for {
		select {
		case <-ctx.Done():
			return outcome{}
		case e := <-m.events:
			if o, ok := m.idleEvent(e); ok {
				return o
			}
		case r := <-m.kicks:
			if r == ReasonManual {
				return outcome{next: phaseConnect, fresh: true}
			}
		case <-expire:
			return outcome{next: phaseConnect, fresh: true}
		}
	}
}

// gone waits out a logout: the old session is being dropped, and a new
// client, or this one without an id, means the login flow can start.
func (m *Machine) gone(ctx context.Context) outcome {
	m.set(Status{Kind: LoggedOut, Detail: "Logged out"})
	t := time.NewTicker(m.opts.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return outcome{}
		case <-m.attached:
			return outcome{next: phaseConnect, fresh: true}
		case r := <-m.kicks:
			if r == ReasonManual {
				return outcome{next: phaseConnect, fresh: true}
			}
		case <-m.events:
		case <-t.C:
			if m.client().Store.ID == nil {
				return outcome{next: phaseConnect, fresh: true}
			}
		}
	}
}

// login waits for a session: the pairing flow when there is one, else a new
// client attached after a logout.
func (m *Machine) login(ctx context.Context) outcome {
	cli := m.client()
	if cli.Store.ID != nil {
		return outcome{next: phaseConnect, fresh: true}
	}
	if m.opts.Login == nil {
		m.set(Status{Kind: NeedLogin, Detail: "Logged out"})
		select {
		case <-ctx.Done():
		case <-m.attached:
		}
		return outcome{next: phaseConnect, fresh: true}
	}
	// published, the connection view is what tells a frontend to show the code
	m.set(Status{Kind: NeedLogin, Detail: "Waiting for a QR scan"})
	err := m.opts.Login(ctx, cli)
	switch {
	case err == nil:
		// pairing ends with whatsmeow reconnecting on its own; success follows
		return m.afterPairing(ctx, cli)
	case errors.Is(err, ErrRetryNow):
		return outcome{next: phaseLogin}
	}
	m.opts.Log.Warn().Err(err).Msg("conn: login")
	d := Backoff(2, m.opts.MinBackoff, m.opts.MaxBackoff, rand.Float64)
	select {
	case <-ctx.Done():
	case <-time.After(d):
	case <-m.kicks:
	}
	return outcome{next: phaseLogin}
}

func (m *Machine) afterPairing(ctx context.Context, cli *whatsmeow.Client) outcome {
	t := time.NewTimer(m.opts.Success)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return outcome{}
		case e := <-m.events:
			if _, ok := e.evt.(*events.Connected); ok && e.cli == cli {
				return outcome{next: phaseAdopt, fresh: true}
			}
		case <-t.C:
			cli.Disconnect()
			return outcome{next: phaseConnect, fresh: true}
		}
	}
}

func (m *Machine) drain() {
	for {
		select {
		case <-m.events:
		default:
			return
		}
	}
}

// watchClock kicks the machine when wall time and monotonic time part: the
// machine slept, or the clock was set. a socket from before either is suspect.
func (m *Machine) watchClock(ctx context.Context) {
	t := time.NewTicker(m.opts.Tick)
	defer t.Stop()
	lastWall, lastMono := m.opts.Wall().Round(0), time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		wall, mono := m.opts.Wall().Round(0), time.Now()
		drift := wall.Sub(lastWall) - mono.Sub(lastMono)
		lastWall, lastMono = wall, mono
		if drift > m.opts.Jump || drift < -m.opts.Jump {
			m.opts.Log.Info().Dur("drift", drift).Msg("conn: clock jumped")
			m.Kick(ReasonClock)
		}
	}
}

func describe(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "Connection attempt cancelled"
	case errors.Is(err, whatsmeow.ErrNotConnected):
		return "Not connected"
	}
	return fmt.Sprintf("Could not reach WhatsApp: %v", err)
}
