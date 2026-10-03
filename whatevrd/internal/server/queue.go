package server

import (
	"container/list"
	"sync"
)

// maxQueuedChanges bounds the changes waiting in one subscription's updates.
// past it the consumer is not reading: its updates are dropped and the
// subscription sends a reset with what is there now. a var for tests.
var maxQueuedChanges = 8192

// maxQueuedFrames bounds responses and connection events waiting on one
// connection. they can't merge or reset, so past it the connection closes.
var maxQueuedFrames = 4096

// update is one subscription's changes waiting to be written. until a
// response is queued behind it, a newer update for the same subscription
// merges into it: only the newest version of an item matters.
type update struct {
	sub       uint64
	reset     bool
	order     []string
	changes   map[string][]byte
	ready     bool
	exhausted bool
}

type entry struct {
	frame      []byte
	closeAfter bool
	upd        *update
}

// queue is a connection's outbound frames in order: a subscribe or extend
// response is always written before the updates it causes.
type queue struct {
	mu      sync.Mutex
	entries list.List
	// open is each subscription's update that may still take merges
	open    map[uint64]*list.Element
	counts  map[uint64]int
	live    map[uint64]bool
	frames  int
	tripped bool
	signal  chan struct{}
}

func newQueue() *queue {
	return &queue{open: map[uint64]*list.Element{}, counts: map[uint64]int{}, live: map[uint64]bool{}, signal: make(chan struct{}, 1)}
}

// push queues a response or connection event. it seals every open update so
// nothing merges past it.
func (q *queue) push(frame []byte, closeAfter bool, overflow func() []byte) {
	q.mu.Lock()
	if q.tripped {
		q.mu.Unlock()
		return
	}
	clear(q.open)
	q.entries.PushBack(&entry{frame: frame, closeAfter: closeAfter})
	q.frames++
	if !closeAfter && q.frames > maxQueuedFrames {
		q.tripped = true
		q.entries.PushBack(&entry{frame: overflow(), closeAfter: true})
	}
	q.mu.Unlock()
	q.wake()
}

// pushUpdate queues u for its subscription, merged into a waiting one when it
// can be. false means the subscription fell too far behind: everything it had
// queued is gone, and it must send a reset.
func (q *queue) pushUpdate(u *update) bool {
	q.mu.Lock()
	defer q.wake()
	defer q.mu.Unlock()
	if !q.live[u.sub] {
		return true
	}
	if u.reset {
		q.purgeLocked(u.sub)
	} else if el, ok := q.open[u.sub]; ok {
		w := el.Value.(*entry).upd
		for _, id := range u.order {
			if _, had := w.changes[id]; !had {
				w.order = append(w.order, id)
				q.counts[u.sub]++
			}
			w.changes[id] = u.changes[id]
		}
		if u.ready {
			w.ready, w.exhausted = true, u.exhausted
		}
		if q.counts[u.sub] > maxQueuedChanges {
			q.purgeLocked(u.sub)
			return false
		}
		return true
	}
	el := q.entries.PushBack(&entry{upd: u})
	q.open[u.sub] = el
	q.counts[u.sub] += len(u.order)
	if !u.reset && q.counts[u.sub] > maxQueuedChanges {
		q.purgeLocked(u.sub)
		return false
	}
	return true
}

func (q *queue) addSub(sub uint64) {
	q.mu.Lock()
	q.live[sub] = true
	q.mu.Unlock()
}

// closeSub drops what sub queued and refuses its future updates, so nothing
// of it reaches the wire after the unsubscribe response.
func (q *queue) closeSub(sub uint64) {
	q.mu.Lock()
	delete(q.live, sub)
	q.purgeLocked(sub)
	q.mu.Unlock()
}

func (q *queue) purgeLocked(sub uint64) {
	var next *list.Element
	for el := q.entries.Front(); el != nil; el = next {
		next = el.Next()
		if u := el.Value.(*entry).upd; u != nil && u.sub == sub {
			q.entries.Remove(el)
		}
	}
	delete(q.open, sub)
	delete(q.counts, sub)
}

func (q *queue) pop() (*entry, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	el := q.entries.Front()
	if el == nil {
		return nil, false
	}
	e := q.entries.Remove(el).(*entry)
	if e.upd != nil {
		if q.open[e.upd.sub] == el {
			delete(q.open, e.upd.sub)
		}
		if q.counts[e.upd.sub] -= len(e.upd.order); q.counts[e.upd.sub] <= 0 {
			delete(q.counts, e.upd.sub)
		}
	} else if q.frames > 0 {
		q.frames--
	}
	return e, true
}

func (q *queue) wake() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// frames is u as whole Frames, split only when one would pass the frame
// limit: the first carries reset, the last ready.
func (u *update) frames() [][]byte {
	changes := make([][]byte, 0, len(u.order))
	for _, id := range u.order {
		changes = append(changes, u.changes[id])
	}
	var out [][]byte
	var part [][]byte
	size := frameHeadBudget
	first := true
	for _, c := range changes {
		n := changeBytes(c)
		if len(part) > 0 && size+n > maxFrameBytes {
			out = append(out, updateFrame(u.sub, first && u.reset, part, false, false))
			first, part, size = false, nil, frameHeadBudget
		}
		part = append(part, c)
		size += n
	}
	return append(out, updateFrame(u.sub, first && u.reset, part, u.ready, u.exhausted))
}
