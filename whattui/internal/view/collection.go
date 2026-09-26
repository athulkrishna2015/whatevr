// Package view holds the two view models every whatevr view is rendered
// through. Between them they are the whole of protocol rule 3's client
// algorithm: keep a map of items by id, ordered by sort, apply upserts and
// removes, render.
//
// Nothing here sorts by any field, merges, deduplicates or caches. The daemon
// owns all of that. The only ordering is a bytewise comparison of the
// daemon-supplied sort string.
package view

import (
	"encoding/json"
	"sync"
)

// Item is one row of a collection view. Value is the decoded row; Raw is the
// bytes it came from, kept so a renderer can reach a field the struct does not
// model yet without the view having to know about it.
type Item[T any] struct {
	ID    string
	Sort  string
	Value T
	Raw   json.RawMessage
}

// A batch bigger than this is cheaper to rebuild and sort than to insert one
// item at a time, because every insertion shifts the tail.
const rebuildThreshold = 16

// Collection is a windowed collection view: the daemon's window, mirrored.
//
// It is a [proto.ViewSink]. Reads and writes are safe from different
// goroutines, and a reader never sees a half-applied batch: everything that
// arrived in one read is applied under one lock.
type Collection[T any] struct {
	mu      sync.RWMutex
	items   []Item[T]
	index   map[string]int
	reverse bool

	ready        bool
	exhausted    bool
	hasExhausted bool
	version      uint64

	inBatch bool
	pending []op[T]

	// OnDecodeError is called with the raw row when it will not decode. A row
	// that will not decode is the daemon's bug; dropping it quietly would make
	// that bug look like a missing message.
	OnDecodeError func(raw json.RawMessage, err error)
}

type op[T any] struct {
	remove bool
	item   Item[T]
}

// NewCollection returns an empty collection view.
func NewCollection[T any]() *Collection[T] {
	return &Collection[T]{index: make(map[string]int)}
}

// SetReverse renders the collection back to front. This is a presentation
// choice, not a reinterpretation of sort: the key stays opaque and bytewise
// compared, the comparison simply runs the other way, which is what lets a
// transcript put the live edge at row 0.
//
// Set it before the first item arrives.
func (c *Collection[T]) SetReverse(reverse bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reverse = reverse
	if len(c.items) > 0 {
		c.sortAll()
		c.reindex(0)
	}
}

// State is everything about the window that is not an item, handed to a
// reader along with them.
type State struct {
	Ready        bool
	Exhausted    bool
	HasExhausted bool
	Version      uint64
}

// Read runs fn over the items in view order, under a read lock. The slice is
// only valid for the call: do not retain it.
//
// Whatever fn needs to know about the window arrives in State. Calling back
// into the collection from inside fn takes the read lock a second time, and a
// second read lock behind a waiting writer is a deadlock, not a slow path.
func (c *Collection[T]) Read(fn func(items []Item[T], state State)) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fn(c.items, State{
		Ready:        c.ready,
		Exhausted:    c.exhausted,
		HasExhausted: c.hasExhausted,
		Version:      c.version,
	})
}

// Len is how many items the window holds.
func (c *Collection[T]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Get returns the item with this id.
func (c *Collection[T]) Get(id string) (Item[T], bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i, ok := c.index[id]
	if !ok {
		return Item[T]{}, false
	}
	return c.items[i], true
}

// IndexOf is an item's row in view order, or -1.
func (c *Collection[T]) IndexOf(id string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if i, ok := c.index[id]; ok {
		return i
	}
	return -1
}

// Ready reports whether the window has been fully populated at least once.
func (c *Collection[T]) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Exhausted reports whether the frontier last extended has nothing further
// locally. The second result is false when the daemon omitted the field, which
// is not the same as it saying no.
func (c *Collection[T]) Exhausted() (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.exhausted, c.hasExhausted
}

// Version bumps on every applied change, so a render loop can tell whether
// anything moved without diffing.
func (c *Collection[T]) Version() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}
