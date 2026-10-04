package proto

import (
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	gproto "google.golang.org/protobuf/proto"
)

type stateEvent struct {
	state State
	err   error
}

// start connects a client to d and waits for the handshake.
func start(t *testing.T, d *fakeDaemon) (*Client, chan stateEvent) {
	t.Helper()
	c, states := dial(t, d)
	until(t, states, Ready)
	return c, states
}

// dial starts a client on d without waiting for anything.
func dial(t *testing.T, d *fakeDaemon) (*Client, chan stateEvent) {
	t.Helper()
	c := New(d.path, "test")
	states := make(chan stateEvent, 256)
	c.OnState = func(s State, _ *v2.HelloResult, err error) { states <- stateEvent{s, err} }
	c.Start()
	t.Cleanup(c.Stop)
	return c, states
}

// until is the first state change to s, skipping the ones on the way.
func until(t *testing.T, states chan stateEvent, s State) stateEvent {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-states:
			if e.state == s {
				return e
			}
		case <-timeout:
			t.Fatalf("the client never got to %v", s)
		}
	}
}

// shortDeadlines gives every request but hello d, for the life of the test.
// set before the client starts and put back after it stops, so no goroutine
// of it ever sees the swap.
func shortDeadlines(t *testing.T, d time.Duration) {
	t.Helper()
	deadlineFor = func(r *v2.Request) time.Duration {
		if r.HasHello() {
			return deadline(r)
		}
		return d
	}
	t.Cleanup(func() { deadlineFor = deadline })
}

func text(s string) *v2.Request {
	req := &v2.Request{}
	req.SetSendText(v2.SendText_builder{ChatId: "c", Text: s}.Build())
	return req
}

func subscribe() *v2.Subscribe {
	return v2.Subscribe_builder{Limit: 10, Messages: v2.MessagesView_builder{ChatId: "c", Latest: &v2.Latest{}}.Build()}.Build()
}

// answers collects what each callback heard and how often.
type answers struct {
	mu   sync.Mutex
	got  map[string][]*Error
	resp map[string][]*v2.Response
	ch   chan string
}

func newAnswers() *answers {
	return &answers{got: map[string][]*Error{}, resp: map[string][]*v2.Response{}, ch: make(chan string, 256)}
}

func (a *answers) cb(name string) ResponseFunc {
	return func(r *v2.Response, err *Error) {
		a.mu.Lock()
		a.got[name] = append(a.got[name], err)
		a.resp[name] = append(a.resp[name], r)
		a.mu.Unlock()
		a.ch <- name
	}
}

func (a *answers) wait(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-a.ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("%d of %d callbacks ran", i, n)
		}
	}
}

func (a *answers) of(name string) []*Error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*Error(nil), a.got[name]...)
}

type applied struct {
	u     *v2.ViewUpdate
	fresh bool
}

type sink struct {
	mu  sync.Mutex
	got []applied
	ch  chan struct{}
}

func newSink() *sink { return &sink{ch: make(chan struct{}, 256)} }

func (s *sink) Apply(u *v2.ViewUpdate, fresh bool) {
	s.mu.Lock()
	s.got = append(s.got, applied{u, fresh})
	s.mu.Unlock()
	s.ch <- struct{}{}
}

func (s *sink) wait(t *testing.T, n int) []applied {
	t.Helper()
	for {
		s.mu.Lock()
		got := append([]applied(nil), s.got...)
		s.mu.Unlock()
		if len(got) >= n {
			return got
		}
		select {
		case <-s.ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("%d of %d updates applied", len(got), n)
		}
	}
}

func (s *sink) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func TestHelloGoesFirstAndSaysWhoIsAsking(t *testing.T) {
	d := newFakeDaemon(t)
	d.hello = nil
	c, states := dial(t, d)

	req := d.next()
	if !req.HasHello() {
		t.Fatalf("the first request is %v, want hello", req.WhichMethod())
	}
	if h := req.GetHello(); h.GetProtocol() != 2 || h.GetClient() != "test" {
		t.Errorf("hello says %v, want protocol 2 from test", h)
	}
	if c.State() == Ready || c.Has("messages") {
		t.Error("ready before hello was answered")
	}
	d.send(v2.Frame_builder{Response: helloOK(req)}.Build())
	until(t, states, Ready)
	if !c.Has("messages") || c.Has("contacts") {
		t.Errorf("features read wrong: messages %v contacts %v", c.Has("messages"), c.Has("contacts"))
	}
	if c.ServerInfo().GetDaemon() != "fake" {
		t.Errorf("server info %v", c.ServerInfo())
	}
}

func TestAnotherProtocolIsADisconnect(t *testing.T) {
	d := newFakeDaemon(t)
	d.hello = func(req *v2.Request) *v2.Response {
		r := helloOK(req)
		r.GetHello().SetProtocol(1)
		return r
	}
	_, states := dial(t, d)
	e := until(t, states, Disconnected)
	for e.err == nil {
		e = until(t, states, Disconnected)
	}
	if !strings.Contains(e.err.Error(), "protocol 1") {
		t.Errorf("disconnected with %v, want the protocol named", e.err)
	}
}

// updates route by sub, the first after a subscribe is fresh and no other
// is, and ready is heard after the update carrying it is in the sink
func TestUpdatesReachTheirSubscription(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	s := newSink()
	readyAt := make(chan int, 1)
	sub := c.Subscribe(subscribe(), s, Hooks{OnReady: func(exhausted bool) {
		if !exhausted {
			t.Error("ready lost exhausted")
		}
		readyAt <- s.len()
	}})

	req := d.next()
	if !gproto.Equal(req.GetSubscribe(), subscribe()) {
		t.Fatalf("subscribed with %v", req.GetSubscribe())
	}
	d.subscribed(req, 7)
	d.update(v2.ViewUpdate_builder{Sub: 99})
	d.update(v2.ViewUpdate_builder{Sub: 7, Reset: true})
	d.update(v2.ViewUpdate_builder{Sub: 7, Ready: v2.Ready_builder{Exhausted: true}.Build()})

	select {
	case n := <-readyAt:
		if n != 2 {
			t.Errorf("ready heard with %d updates applied, want 2", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ready never heard")
	}
	got := s.wait(t, 2)
	if len(got) != 2 || !got[0].fresh || got[1].fresh {
		t.Errorf("applied %v, want two, the first fresh", got)
	}
	if !sub.Active() {
		t.Error("an answered subscription is not active")
	}
}

func TestAnExtendAskedEarlyGoesOnceTheSubIsKnown(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	sub := c.Subscribe(subscribe(), newSink(), Hooks{})
	sub.Extend(10, v2.Direction_DIRECTION_OLDER)

	req := d.next()
	d.quiet()
	d.subscribed(req, 3)
	ext := d.next().GetExtend()
	if ext.GetSub() != 3 || ext.GetCount() != 10 || ext.GetDirection() != v2.Direction_DIRECTION_OLDER {
		t.Errorf("extend %v, want 10 older on sub 3", ext)
	}
}

func TestAReconnectIssuesEverySubscriptionAgain(t *testing.T) {
	d := newFakeDaemon(t)
	c, states := start(t, d)
	s := newSink()
	sub := c.Subscribe(subscribe(), s, Hooks{})
	d.subscribed(d.next(), 1)
	d.update(v2.ViewUpdate_builder{Sub: 1})
	s.wait(t, 1)

	d.drop()
	until(t, states, Disconnected)
	until(t, states, Ready)
	if sub.Active() {
		t.Error("a subscription kept the dead connection's sub")
	}
	req := d.next()
	if !gproto.Equal(req.GetSubscribe(), subscribe()) {
		t.Fatalf("after reconnect the client asked %v", req)
	}
	d.subscribed(req, 2)
	d.update(v2.ViewUpdate_builder{Sub: 1})
	d.update(v2.ViewUpdate_builder{Sub: 2})
	got := s.wait(t, 2)
	time.Sleep(20 * time.Millisecond)
	if s.len() != 2 || !got[1].fresh {
		t.Errorf("after reconnect applied %d, fresh %v; want the new sub's alone and fresh", s.len(), got[1].fresh)
	}
}

// a lost connection answers everything owed once, and a deadline after it
// does not answer again
func TestEveryCallbackRunsOnceAcrossADisconnect(t *testing.T) {
	shortDeadlines(t, 200*time.Millisecond)
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	a := newAnswers()
	for _, n := range []string{"a", "b", "c"} {
		c.Do(text(n), a.cb(n))
		d.next()
	}
	d.drop()
	a.wait(t, 3)
	time.Sleep(300 * time.Millisecond)
	for _, n := range []string{"a", "b", "c"} {
		if got := a.of(n); len(got) != 1 || got[0] != errLost {
			t.Errorf("%s heard %v, want lost once", n, got)
		}
	}
}

func TestAnOversizedFrameDropsTheConnection(t *testing.T) {
	d := newFakeDaemon(t)
	_, states := start(t, d)
	d.write(binary.AppendUvarint(nil, maxFrameBytes+1))
	if e := until(t, states, Disconnected); !errors.Is(e.err, errFrameTooBig) {
		t.Errorf("disconnected with %v, want the frame named too big", e.err)
	}
}

// a frame that does not decode is the daemon's bug, and the connection goes
// on past it
func TestAnUndecodableFrameIsSkipped(t *testing.T) {
	d := newFakeDaemon(t)
	c, states := start(t, d)
	a := newAnswers()
	c.Do(text("x"), a.cb("x"))
	req := d.next()
	d.write([]byte{3, 0xff, 0xff, 0xff})
	d.answer(req, nil)
	a.wait(t, 1)
	if got := a.of("x"); got[0] != nil {
		t.Errorf("the answer after junk came as %v", got[0])
	}
	select {
	case e := <-states:
		t.Errorf("junk changed the state to %v (%v)", e.state, e.err)
	default:
	}
}

func TestAFrameInPiecesIsOneFrame(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	a := newAnswers()
	c.Do(text("x"), a.cb("x"))
	req := d.next()

	// long enough that the length prefix is two bytes, and those split too
	msg := strings.Repeat("m", 300)
	r := v2.Response_builder{Id: req.GetId(), Error: v2.Error_builder{Message: msg}.Build()}.Build()
	b, err := encode(v2.Frame_builder{Response: r}.Build())
	if err != nil {
		t.Fatal(err)
	}
	for i := range b {
		d.write(b[i : i+1])
		if i < 4 {
			time.Sleep(time.Millisecond)
		}
	}
	a.wait(t, 1)
	if got := a.of("x"); got[0] == nil || got[0].Message != msg {
		t.Errorf("heard %v, want the whole message", got[0])
	}
}

func TestAnswersMayComeInAnyOrder(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	a := newAnswers()
	c.Do(text("a"), a.cb("a"))
	ra := d.next()
	c.Do(text("b"), a.cb("b"))
	rb := d.next()
	for _, r := range []*v2.Request{rb, ra} {
		name := r.GetSendText().GetText()
		d.answer(r, func(resp *v2.Response) { resp.SetError(v2.Error_builder{Message: name}.Build()) })
	}
	a.wait(t, 2)
	for _, n := range []string{"a", "b"} {
		if got := a.of(n); len(got) != 1 || got[0].Message != n {
			t.Errorf("%s heard %v", n, got)
		}
	}
}

func TestADeadlineSaysNoAndTheLateAnswerGoesNowhere(t *testing.T) {
	shortDeadlines(t, 50*time.Millisecond)
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	a := newAnswers()
	c.Do(text("x"), a.cb("x"))
	req := d.next()
	a.wait(t, 1)
	d.answer(req, nil)
	// the answer is written after the timeout; another request answered
	// behind it proves it was read
	c.Do(text("y"), a.cb("y"))
	d.answer(d.next(), nil)
	a.wait(t, 1)
	if got := a.of("x"); len(got) != 1 || got[0] != errTimeout {
		t.Errorf("x heard %v, want one timeout", got)
	}
}

// no more than 32 written and unanswered; the next goes out when one is
// answered
func TestTheWriterHoldsBackPastThirtyTwoOwed(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	for i := 0; i < maxAwaiting+5; i++ {
		c.Do(text("x"), nil)
	}
	var out []*v2.Request
	for i := 0; i < maxAwaiting; i++ {
		out = append(out, d.next())
	}
	d.quiet()
	d.answer(out[0], nil)
	d.next()
	d.quiet()
}

func TestPastTheQueueARequestIsRefusedOnTheSpot(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	for i := 0; i < maxAwaiting; i++ {
		c.Do(text("x"), nil)
		d.next()
	}
	for i := 0; i < maxQueued; i++ {
		c.Do(text("x"), func(_ *v2.Response, err *Error) {
			if err == errBusy {
				t.Error("a request inside the queue was refused")
			}
		})
	}
	var heard *Error
	c.Do(text("x"), func(_ *v2.Response, err *Error) { heard = err })
	if heard != errBusy {
		t.Errorf("one past the queue heard %v by the time Do returned, want busy", heard)
	}
	heard = nil
	c.Subscribe(subscribe(), newSink(), Hooks{OnFailed: func(err *Error) { heard = err }})
	if heard != errBusy {
		t.Errorf("a subscribe past the queue heard %v by the time it returned, want busy", heard)
	}
}

func TestPastTheQueuedBytesARequestIsRefused(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	for i := 0; i < maxAwaiting; i++ {
		c.Do(text("x"), nil)
		d.next()
	}
	big := strings.Repeat("b", 9<<20)
	var first, second *Error
	c.Do(text(big), func(_ *v2.Response, err *Error) { first = err })
	c.Do(text(big), func(_ *v2.Response, err *Error) { second = err })
	if first == errBusy || second != errBusy {
		t.Errorf("9 MiB then 9 MiB heard %v and %v, want the second refused", first, second)
	}
}

// a daemon that stops reading wedges the writer; the write timeout turns that
// into a lost connection instead of a frozen one
func TestAWedgedDaemonIsALostConnection(t *testing.T) {
	writeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { writeTimeout = 10 * time.Second })
	d := newFakeDaemon(t)
	d.stall = true
	c, states := start(t, d)
	a := newAnswers()
	c.Do(text(strings.Repeat("w", 8<<20)), a.cb("w"))
	until(t, states, Disconnected)
	a.wait(t, 1)
	if got := a.of("w"); got[0] != errLost {
		t.Errorf("the stuck write heard %v, want lost", got[0])
	}
}

func TestWithNoDaemonARequestIsRefusedOnTheSpot(t *testing.T) {
	c := New(t.TempDir()+"/none.sock", "test")
	var heard *Error
	c.Do(text("x"), func(_ *v2.Response, err *Error) { heard = err })
	if heard != errOffline {
		t.Errorf("heard %v by the time Do returned, want offline", heard)
	}

	c.Start()
	t.Cleanup(c.Stop)
	heard = nil
	c.Do(text("x"), func(_ *v2.Response, err *Error) { heard = err })
	if heard != errOffline {
		t.Errorf("while dialing heard %v, want offline", heard)
	}
}

// a sub nobody will route is handed back rather than left running in the
// daemon
func TestASubscribeAnswerNobodyWantsIsUnsubscribed(t *testing.T) {
	t.Run("late", func(t *testing.T) {
		shortDeadlines(t, 50*time.Millisecond)
		d := newFakeDaemon(t)
		c, _ := start(t, d)
		failed := make(chan *Error, 1)
		c.Subscribe(subscribe(), newSink(), Hooks{OnFailed: func(err *Error) { failed <- err }})
		req := d.next()
		if err := <-failed; err != errTimeout {
			t.Fatalf("failed with %v, want timeout", err)
		}
		d.subscribed(req, 9)
		if u := d.next().GetUnsubscribe(); u.GetSub() != 9 {
			t.Errorf("want unsubscribe 9, got %v", u)
		}
	})
	t.Run("closed", func(t *testing.T) {
		d := newFakeDaemon(t)
		c, _ := start(t, d)
		sub := c.Subscribe(subscribe(), newSink(), Hooks{})
		req := d.next()
		sub.Close()
		d.quiet()
		d.subscribed(req, 5)
		if u := d.next().GetUnsubscribe(); u.GetSub() != 5 {
			t.Errorf("want unsubscribe 5, got %v", u)
		}
	})
}

func TestNothingIsHeardAfterStop(t *testing.T) {
	d := newFakeDaemon(t)
	c, _ := start(t, d)
	var stopped atomic.Bool
	calls := 0
	c.Do(text("x"), func(_ *v2.Response, err *Error) {
		calls++
		if stopped.Load() {
			t.Error("a callback ran after Stop returned")
		}
	})
	req := d.next()
	s := newSink()
	c.Subscribe(subscribe(), s, Hooks{})
	d.subscribed(d.next(), 1)

	c.Stop()
	stopped.Store(true)
	if calls != 1 {
		t.Errorf("the owed callback ran %d times by Stop, want once", calls)
	}
	n := s.len()
	d.answer(req, nil)
	d.update(v2.ViewUpdate_builder{Sub: 1})
	time.Sleep(50 * time.Millisecond)
	if s.len() != n {
		t.Error("an update was applied after Stop")
	}
}
