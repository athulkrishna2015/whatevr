package protocol

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// eventEnvelope is the daemon→frontend event shape on the wire for events that
// carry no sort key (reset, remove, ready). Upserts use upsertEnvelope so their
// `sort` is always present on the wire — see emitUpsert.
type eventEnvelope struct {
	Sub       int64           `json:"sub"`
	Event     string          `json:"event"`
	Item      json.RawMessage `json:"item,omitempty"`
	ID        string          `json:"id,omitempty"`
	Exhausted bool            `json:"exhausted,omitempty"`
}

// upsertEnvelope is the wire shape for upserts. `sort` deliberately carries no
// omitempty: the grammar requires every upsert to carry a sort key, so an empty
// key must still be marshalled (and is caught by the assertion in emitUpsert)
// rather than silently dropped into a sort-less, grammar-violating frame.
type upsertEnvelope struct {
	Sub   int64           `json:"sub"`
	Event string          `json:"event"`
	Sort  string          `json:"sort"`
	Item  json.RawMessage `json:"item"`
}

func resetLine(sub int64) []byte {
	line, _ := json.Marshal(eventEnvelope{Sub: sub, Event: "reset"})
	return line
}

// overloadedLine is the terminal frame sent when a connection's outbound queue
// overflows its connection-level cap: an error response the peer may still read,
// after which the connection is closed.
func overloadedLine() []byte {
	line, _ := json.Marshal(response{ID: nullID, Error: errorf(CodeInternal, "connection outbound queue overflowed; closing")})
	return line
}

// frameSink is where a subscription's event frames go; *conn implements it
// on top of its outbound queue. The returned reset reports that the sink
// overflowed, dropped this subscription's queue, and already emitted
// `reset` — the caller must rebuild from scratch.
type frameSink interface {
	sendEvent(sub int64, itemKey string, line []byte) (reset bool)
}

// sentItem records the version of an item the client last received, for
// change detection during recompute. value is the Item.Data it was marshalled
// from, so an unchanged item is recognised without marshalling it again; sum
// is the hash of the bytes sent, for a value that changed but marshals the
// same. a big window keeps every row, the bytes themselves cost too much.
type sentItem struct {
	sort  string
	sum   [sha256.Size]byte
	value any
}

// same reports whether data would marshal to the same body. View items are
// plain structs holding scalars, slices and pointers to more of the same, so a
// deep comparison is both correct and far cheaper than marshalling: it
// allocates nothing and stops at the first difference. `==` is not an option —
// items carrying slices (reactions, mentions) would panic.
func (s sentItem) same(data any) bool {
	return s.value != nil && reflect.DeepEqual(s.value, data)
}

// subscription keeps one client's copy of one view window correct: it
// re-reads the session's items whenever the view invalidates it, diffs
// against what the client already holds, and emits upserts/removes (and
// `ready` after subscribe/extend fills).
type subscription struct {
	id int64
	// ctx only carries the logger with conn, sub and view
	ctx  context.Context
	sink frameSink
	sess ViewSession
	// dir is sess when it manages its own two-frontier window (anchored
	// messages); nil for a plain prefix session. Set once at subscribe.
	dir DirectionalSession

	mu           sync.Mutex
	window       int // 0 = unbounded
	pendingReady bool
	dirty        bool
	running      bool
	started      bool
	closed       bool

	// sent maps item id → last emitted version. Only the run goroutine
	// touches it (the running flag guarantees a single runner).
	sent map[string]sentItem
}

func newSubscription(id int64, sink frameSink, window int) *subscription {
	return &subscription{id: id, ctx: context.Background(), sink: sink, window: window}
}

// kick schedules a window recomputation; views call it (via the invalidate
// callback) whenever their contents may have changed. Safe from any
// goroutine; cheap when a recomputation is already pending.
func (s *subscription) kick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = true
	s.maybeRunLocked()
}

// start begins delivery: the initial fill runs asynchronously, so calling
// start only after the subscribe response is enqueued guarantees the wire
// order response → upserts → ready. Before start, kicks accumulate into
// the first fill instead of emitting.
func (s *subscription) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
	s.dirty = true
	s.pendingReady = true
	s.maybeRunLocked()
}

// validateExtend rejects a direction the window's shape cannot honor. Only a
// prefix (live-edge) session is constrained: its newer edge is the live edge,
// so `newer` is meaningless there. A directional session accepts both
// directions (nothing-left-to-reach is a no-op, surfaced via Exhausted).
func (s *subscription) validateExtend(direction string) *Error {
	if s.dir == nil && direction == "newer" {
		return errorf(CodeInvalidParams, "cannot extend this window newer; its newer edge is the live edge, where items arrive on their own")
	}
	return nil
}

// applyExtend grows the window and schedules a fill that ends in `ready`. Like
// start, the caller enqueues the extend response first. Readies coalesce
// across rapid extends; PROTOCOL.md defines ready as covering the *latest*
// subscribe/extend, so clients may not count them. A directional session owns
// its window, so growth routes to ExtendWindow; a prefix session grows its
// single-integer window here (only `older` reaches this path — `newer` was
// rejected in validateExtend).
func (s *subscription) applyExtend(direction string, count int) {
	if s.dir != nil {
		s.dir.ExtendWindow(direction, count)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == nil && s.window > 0 {
		s.window += count
	}
	s.pendingReady = true
	s.dirty = true
	s.maybeRunLocked()
}

// close ends delivery and releases the view session. Idempotent; safe from
// any goroutine. The session is closed outside s.mu so a view holding its
// own lock in Close can never deadlock against a concurrent invalidate.
func (s *subscription) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	sess := s.sess
	s.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

func (s *subscription) maybeRunLocked() {
	if !s.started || s.running || s.closed {
		return
	}
	s.running = true
	go s.run()
}

// run drains dirtiness: each pass recomputes the window once, then loops if
// more kicks arrived meanwhile. A sink reset wipes the sent map so the next
// pass re-emits the whole window and closes with ready, exactly the
// "fresh upserts follow, then ready" contract of `reset`.
func (s *subscription) run() {
	s.mu.Lock()
	for s.dirty && !s.closed {
		s.dirty = false
		window := s.window
		ready := s.pendingReady
		s.pendingReady = false
		s.mu.Unlock()

		exhausted, reset := s.recompute(window)
		if !reset && ready {
			reset = s.emitReady(exhausted)
		}

		s.mu.Lock()
		if reset {
			s.sent = nil
			s.dirty = true
			s.pendingReady = true
		}
	}
	s.running = false
	s.mu.Unlock()
}

// fetchItems reads the window, letting a session that can fail say so. A plain
// ViewSession has no way to report one, so its empty answer is taken at face
// value exactly as before.
func (s *subscription) fetchItems(max int) ([]Item, error) {
	if f, ok := s.sess.(FallibleSession); ok {
		return f.ItemsErr(max)
	}
	return s.sess.Items(max), nil
}

// recompute pulls the current window from the session and emits the
// difference against what the client holds. Fetching one item beyond the
// window tells us whether there is anything left to extend into.
func (s *subscription) recompute(window int) (exhausted, reset bool) {
	start := time.Now()
	var items []Item
	var upserted, removed []string
	defer func() { logRecompute(s.ctx, len(items), upserted, removed, start) }()
	// A session that owns its window answers Items(0) with the whole of it.
	fetch := 0
	if s.dir == nil && window > 0 {
		fetch = window + 1
	}
	items, err := s.fetchItems(fetch)
	if err != nil {
		// The window could not be read, so nothing is known about it. Running the
		// diff here would compare the client's rows against an empty answer and
		// remove every one of them, which is how one store error blanked an open
		// chat. Leave s.sent alone: what was delivered stays on screen, and the
		// next invalidation recomputes against a store that may answer.
		zerolog.Ctx(s.ctx).Warn().Err(err).Int("kept", len(s.sent)).Msg("keeping delivered items after a failed read")
		return exhausted, false
	}
	if s.dir != nil {
		// Exhaustion is per the frontier last extended. No prefix trim.
		exhausted = s.dir.Exhausted()
	} else {
		exhausted = true
		if window > 0 && len(items) > window {
			items = items[:window]
			exhausted = false
		}
	}

	next := make(map[string]sentItem, len(items))
	for _, it := range items {
		if _, dup := next[it.ID]; dup {
			zerolog.Ctx(s.ctx).Error().Str("item", it.ID).Msg("view session yielded a duplicate item id")
			continue
		}
		// Most recomputes change nothing: an unrelated avatar landing re-reads
		// the whole window only to find every row identical. Marshalling each
		// one just to byte-compare it made that no-op cost proportional to the
		// window (a 1024-member roster re-marshalled per avatar event), so skip
		// the marshal entirely when the source value is unchanged.
		prev, hadPrev := s.sent[it.ID]
		if hadPrev && prev.sort == it.Sort && prev.same(it.Data) {
			next[it.ID] = prev
			continue
		}
		body, err := json.Marshal(it.Data)
		if err != nil {
			zerolog.Ctx(s.ctx).Error().Err(err).Str("item", it.ID).Msg("marshal view item")
			continue
		}
		cur := sentItem{sort: it.Sort, sum: sha256.Sum256(body), value: it.Data}
		next[it.ID] = cur
		if hadPrev && prev.sort == it.Sort && prev.sum == cur.sum {
			continue
		}
		upserted = append(upserted, it.ID)
		if s.emitUpsert(it.ID, it.Sort, body) {
			return exhausted, true
		}
	}
	for id := range s.sent {
		if _, ok := next[id]; ok {
			continue
		}
		removed = append(removed, id)
		if s.emitRemove(id) {
			return exhausted, true
		}
	}
	s.sent = next
	return exhausted, false
}

func (s *subscription) emitUpsert(id, sort string, body []byte) (reset bool) {
	if sort == "" {
		// A sort-less upsert is a grammar violation the frontend engine cannot
		// place. It means a view returned an Item with an empty Sort — a daemon
		// bug. Log it loudly; the id-as-sort fallback keeps the frame well-formed
		// so one buggy view cannot desync the whole subscription.
		zerolog.Ctx(s.ctx).Error().Str("item", id).Msg("view emitted an upsert with an empty sort, falling back to the id")
		sort = id
	}
	line, _ := json.Marshal(upsertEnvelope{Sub: s.id, Event: "upsert", Sort: sort, Item: body})
	return s.sink.sendEvent(s.id, id, line)
}

func (s *subscription) emitRemove(id string) (reset bool) {
	line, _ := json.Marshal(eventEnvelope{Sub: s.id, Event: "remove", ID: id})
	return s.sink.sendEvent(s.id, id, line)
}

func (s *subscription) emitReady(exhausted bool) (reset bool) {
	line, _ := json.Marshal(eventEnvelope{Sub: s.id, Event: "ready", Exhausted: exhausted})
	return s.sink.sendEvent(s.id, "", line)
}
