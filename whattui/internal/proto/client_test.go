package proto

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordSink records every sink call in order, so a test can assert on the
// shape of a transaction and not just its contents.
type recordSink struct {
	mu    sync.Mutex
	calls []string
	items map[string]string
	ready chan struct{}
}

func newRecordSink() *recordSink {
	return &recordSink{items: map[string]string{}, ready: make(chan struct{}, 16)}
}

func (r *recordSink) Upsert(sort string, item json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var row struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(item, &row)
	r.calls = append(r.calls, "upsert:"+row.ID)
	r.items[row.ID] = sort
}

func (r *recordSink) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "remove:"+id)
	delete(r.items, id)
}

func (r *recordSink) Ready(exhausted, has bool) {
	r.mu.Lock()
	r.calls = append(r.calls, "ready")
	r.mu.Unlock()
	select {
	case r.ready <- struct{}{}:
	default:
	}
}

func (r *recordSink) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "reset")
	r.items = map[string]string{}
}

func (r *recordSink) BatchBegin() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "begin")
}

func (r *recordSink) BatchEnd() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "end")
}

func (r *recordSink) trace() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.calls, " ")
}

func (r *recordSink) waitReady(t *testing.T) {
	t.Helper()
	select {
	case <-r.ready:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for ready, trace so far: %q", r.trace())
	}
}

// connectedClient brings a client up to Ready against a fake daemon and hands
// back the state stream. OnState is wired before Start and never reassigned:
// the client reads it from its own goroutine, so a test that swapped it later
// would be racing.
func connectedClient(t *testing.T) (*Client, *fakeDaemon, chan State) {
	t.Helper()
	d := newFakeDaemon(t)
	c := New(d.path, "whattui-test")
	states := make(chan State, 64)
	c.OnState = func(s State, info *ServerInfo, err error) {
		select {
		case states <- s:
		default:
		}
	}
	c.Start()
	t.Cleanup(c.Stop)

	d.waitForConnection()
	d.helloOK(d.expect("hello"))
	waitState(t, states, Ready)
	return c, d, states
}

// waitState drains the state stream until want shows up.
func waitState(t *testing.T, states chan State, want State) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-states:
			if s == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for state %v", want)
		}
	}
}

func TestHelloIsTheFirstRequestAndCarriesTheProtocolVersion(t *testing.T) {
	d := newFakeDaemon(t)
	c := New(d.path, "whattui-test")
	c.Start()
	t.Cleanup(c.Stop)

	d.waitForConnection()
	req := d.nextRequest()
	if req["method"] != "hello" {
		t.Fatalf("first request was %v, want hello", req["method"])
	}
	params := req["params"].(map[string]any)
	if params["protocol"] != float64(ProtocolVersion) {
		t.Errorf("protocol = %v, want %d", params["protocol"], ProtocolVersion)
	}
	if params["client"] != "whattui-test" {
		t.Errorf("client = %v", params["client"])
	}
}

func TestSubscribeRoutesEventsToItsSink(t *testing.T) {
	c, d, _ := connectedClient(t)
	sink := newRecordSink()
	c.Subscribe("chats", Params{"limit": 2}, sink)

	req := d.expect("subscribe")
	if p := req["params"].(map[string]any); p["view"] != "chats" || p["limit"] != float64(2) {
		t.Fatalf("subscribe params = %v", p)
	}
	d.sendLines(map[string]any{"id": req["id"], "result": map[string]any{"sub": 7}})

	d.sendLines(
		map[string]any{"sub": 7, "event": "upsert", "sort": "a", "item": map[string]any{"id": "x"}},
		map[string]any{"sub": 7, "event": "upsert", "sort": "b", "item": map[string]any{"id": "y"}},
		map[string]any{"sub": 7, "event": "ready", "exhausted": true},
	)
	sink.waitReady(t)

	if got, want := sink.trace(), "begin upsert:x upsert:y end ready"; got != want {
		t.Errorf("trace = %q, want %q", got, want)
	}
}

func TestOneWindowFillIsOneTransaction(t *testing.T) {
	c, d, _ := connectedClient(t)
	sink := newRecordSink()
	c.Subscribe("chats", nil, sink)

	req := d.expect("subscribe")
	d.sendLines(map[string]any{"id": req["id"], "result": map[string]any{"sub": 1}})

	// Eighty items in one write is the case the batching exists for.
	var frames []any
	for i := 0; i < 80; i++ {
		frames = append(frames, map[string]any{
			"sub": 1, "event": "upsert", "sort": string(rune('a' + i%26)),
			"item": map[string]any{"id": string(rune('A' + i%26))},
		})
	}
	frames = append(frames, map[string]any{"sub": 1, "event": "ready"})
	d.sendLines(frames...)
	sink.waitReady(t)

	trace := sink.trace()
	if n := strings.Count(trace, "begin"); n != 1 {
		t.Errorf("got %d batches for one fill, want 1 (trace %q)", n, trace)
	}
	if !strings.HasSuffix(trace, "end ready") {
		t.Errorf("batch did not close before ready: %q", trace)
	}
}

func TestReadyAndResetAreNotBatched(t *testing.T) {
	c, d, _ := connectedClient(t)
	sink := newRecordSink()
	c.Subscribe("chats", nil, sink)
	req := d.expect("subscribe")
	d.sendLines(map[string]any{"id": req["id"], "result": map[string]any{"sub": 1}})

	d.sendLines(
		map[string]any{"sub": 1, "event": "upsert", "sort": "a", "item": map[string]any{"id": "x"}},
		map[string]any{"sub": 1, "event": "reset"},
		map[string]any{"sub": 1, "event": "upsert", "sort": "b", "item": map[string]any{"id": "y"}},
		map[string]any{"sub": 1, "event": "ready"},
	)
	sink.waitReady(t)

	if got, want := sink.trace(), "begin upsert:x end reset begin upsert:y end ready"; got != want {
		t.Errorf("trace = %q, want %q", got, want)
	}
}

func TestExtendBeforeTheSubIdLandsIsQueuedNotDropped(t *testing.T) {
	c, d, _ := connectedClient(t)
	sink := newRecordSink()
	sub := c.Subscribe("messages", Params{"chat_id": "c1"}, sink)

	req := d.expect("subscribe")
	// Extend while the subscribe is still in flight.
	sub.Extend(50, Older)

	d.sendLines(map[string]any{"id": req["id"], "result": map[string]any{"sub": 3}})

	ext := d.expect("extend")
	p := ext["params"].(map[string]any)
	if p["sub"] != float64(3) || p["count"] != float64(50) || p["direction"] != Older {
		t.Errorf("extend params = %v", p)
	}
}

func TestReconnectReissuesEveryLiveSubscription(t *testing.T) {
	c, d, _ := connectedClient(t)
	sink := newRecordSink()
	c.Subscribe("chats", Params{"filter": "all"}, sink)

	first := d.expect("subscribe")
	d.sendLines(map[string]any{"id": first["id"], "result": map[string]any{"sub": 1}})

	d.dropConnection()
	d.waitForConnection()
	d.helloOK(d.expect("hello"))

	again := d.expect("subscribe")
	p := again["params"].(map[string]any)
	if p["view"] != "chats" || p["filter"] != "all" {
		t.Fatalf("re-issued subscribe lost its params: %v", p)
	}
}

func TestEveryCallbackFiresExactlyOnceAcrossADisconnect(t *testing.T) {
	c, d, _ := connectedClient(t)

	var mu sync.Mutex
	calls := 0
	fired := make(chan struct{}, 4)
	c.Do("send.text", Params{"chat_id": "c1", "text": "oi"}, func(_ json.RawMessage, err *Error) {
		mu.Lock()
		calls++
		mu.Unlock()
		fired <- struct{}{}
	})
	d.expect("send.text")

	d.dropConnection()
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("callback never fired after the connection dropped")
	}

	// Ride out a reconnect and make sure nothing fires a second time.
	d.waitForConnection()
	d.helloOK(d.expect("hello"))
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("callback fired %d times, want exactly 1", calls)
	}
}

func TestAnOversizedFrameDropsTheConnection(t *testing.T) {
	_, d, states := connectedClient(t)

	// No newline, just a blob past the cap. The write races the client
	// hanging up on us, so it runs off on its own goroutine.
	go d.sendRaw([]byte(`{"pad":"` + strings.Repeat("x", maxLineBytes+16) + `"}`))

	waitState(t, states, Disconnected)
}

func TestUnparseableFrameDoesNotKillTheConnection(t *testing.T) {
	c, d, _ := connectedClient(t)
	sink := newRecordSink()
	c.Subscribe("chats", nil, sink)
	req := d.expect("subscribe")
	d.sendLines(map[string]any{"id": req["id"], "result": map[string]any{"sub": 1}})

	d.sendRaw([]byte("this is not json\n"))
	d.sendLines(map[string]any{"sub": 1, "event": "ready"})
	sink.waitReady(t)

	if c.State() != Ready {
		t.Errorf("state = %v after a junk frame, want Ready", c.State())
	}
}
