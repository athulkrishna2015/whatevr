// Package view holds the two view models every whatevr view is rendered
// through. Between them they are the whole of the protocol's client side:
// keep the items by id, ordered by sort, apply each update whole, render.
//
// Nothing here sorts by any field, merges, deduplicates or caches. The daemon
// owns all of that. The only ordering is a bytewise comparison of the
// daemon's sort bytes.
package view

import (
	"bytes"
	"sort"
	"sync"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// Item is one row of a collection view. Value is the row as the daemon sent
// it, and nobody writes to it: an update brings a new one.
type Item[T any] struct {
	ID   string
	Sort string
	// Rev moves on every upsert of the item, so a cache of what it drew
	// knows when to go again
	Rev   uint64
	Value T
}

// A batch bigger than this is cheaper to rebuild and sort than to insert one
// item at a time, because every insertion shifts the tail.
const rebuildThreshold = 16

// Collection is a windowed collection view: the daemon's window, mirrored.
//
// It is a [proto.ViewSink]. Reads and writes are safe from different
// goroutines, and a reader never sees half an update.
type Collection[T any] struct {
	extract func(*v2.Upsert) (T, bool)

	mu        sync.RWMutex
	items     []Item[T]
	index     map[string]int
	reverse   bool
	ready     bool
	exhausted bool
	version   uint64
	rev       uint64

	// OnReplaced hears a removed item that folded into another, after the
	// update is applied: whatever the frontend held on old moves to by
	OnReplaced func(old, by string)
	// OnMismatch hears an upsert carrying some other view's row. That is
	// the daemon's bug; dropping it quietly would make it look like a
	// missing row.
	OnMismatch func(*v2.Upsert)
}

// NewCollection returns an empty collection view whose rows extract takes
// out of an upsert, false when the upsert holds another kind.
func NewCollection[T any](extract func(*v2.Upsert) (T, bool)) *Collection[T] {
	return &Collection[T]{extract: extract, index: map[string]int{}}
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
	Ready bool
	// Exhausted is the last ready's: nothing further on the frontier last
	// extended
	Exhausted bool
	Version   uint64
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
	fn(c.items, State{Ready: c.ready, Exhausted: c.exhausted, Version: c.version})
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

// IsReady reports whether the window has been filled since it was last
// emptied.
func (c *Collection[T]) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Exhausted is the last ready's word on the frontier last extended.
func (c *Collection[T]) Exhausted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.exhausted
}

// Version bumps on every applied update, so a render loop can tell whether
// anything moved without diffing.
func (c *Collection[T]) Version() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// Apply takes one update whole, under one lock.
func (c *Collection[T]) Apply(u *v2.ViewUpdate, fresh bool) {
	var replaced [][2]string
	var bad []*v2.Upsert

	c.mu.Lock()
	if u.GetReset() || fresh {
		c.items = nil
		c.index = map[string]int{}
		c.ready, c.exhausted = false, false
	}
	type op struct {
		remove bool
		item   Item[T]
	}
	ops := make([]op, 0, len(u.GetChanges()))
	for _, ch := range u.GetChanges() {
		switch ch.WhichChange() {
		case v2.Change_Upsert_case:
			up := ch.GetUpsert()
			v, ok := c.extract(up)
			if !ok {
				bad = append(bad, up)
				continue
			}
			c.rev++
			ops = append(ops, op{item: Item[T]{ID: up.GetId(), Sort: string(up.GetSort()), Rev: c.rev, Value: v}})
		case v2.Change_Remove_case:
			rm := ch.GetRemove()
			ops = append(ops, op{remove: true, item: Item[T]{ID: rm.GetId()}})
			if by := rm.GetReplacedBy(); by != "" {
				replaced = append(replaced, [2]string{rm.GetId(), by})
			}
		}
	}
	// ops apply in arrival order, which is what makes a remove followed by
	// an upsert of the same id come out the way the wire meant it
	if len(ops) <= rebuildThreshold {
		for _, o := range ops {
			if o.remove {
				c.applyRemove(o.item.ID)
			} else {
				c.applyUpsert(o.item)
			}
		}
	} else {
		byID := make(map[string]Item[T], len(c.items)+len(ops))
		for _, it := range c.items {
			byID[it.ID] = it
		}
		for _, o := range ops {
			if o.remove {
				delete(byID, o.item.ID)
			} else {
				byID[o.item.ID] = o.item
			}
		}
		c.items = c.items[:0]
		for _, it := range byID {
			c.items = append(c.items, it)
		}
		c.sortAll()
		c.index = make(map[string]int, len(c.items))
		c.reindex(0)
	}
	if u.HasReady() {
		c.ready, c.exhausted = true, u.GetReady().GetExhausted()
	}
	c.version++
	onReplaced, onMismatch := c.OnReplaced, c.OnMismatch
	c.mu.Unlock()

	if onReplaced != nil {
		for _, r := range replaced {
			onReplaced(r[0], r[1])
		}
	}
	if onMismatch != nil {
		for _, up := range bad {
			onMismatch(up)
		}
	}
}

// applyUpsert places an item, moving it if its sort changed. Callers hold the
// write lock.
func (c *Collection[T]) applyUpsert(item Item[T]) {
	if at, ok := c.index[item.ID]; ok {
		if c.items[at].Sort == item.Sort {
			c.items[at] = item
			return
		}
		c.removeAt(at)
	}
	at := c.lowerBound(item)
	c.items = append(c.items, Item[T]{})
	copy(c.items[at+1:], c.items[at:])
	c.items[at] = item
	c.reindex(at)
}

func (c *Collection[T]) applyRemove(id string) {
	if at, ok := c.index[id]; ok {
		c.removeAt(at)
	}
}

func (c *Collection[T]) removeAt(at int) {
	delete(c.index, c.items[at].ID)
	c.items = append(c.items[:at], c.items[at+1:]...)
	c.reindex(at)
}

func (c *Collection[T]) reindex(from int) {
	for i := from; i < len(c.items); i++ {
		c.index[c.items[i].ID] = i
	}
}

// sortsBefore is the whole of the ordering: a bytewise comparison of the
// opaque sort, with the id as a stable tiebreak so two items that sort equal
// never swap places between renders.
func (c *Collection[T]) sortsBefore(a, b Item[T]) bool {
	if cmp := bytes.Compare([]byte(a.Sort), []byte(b.Sort)); cmp != 0 {
		if c.reverse {
			return cmp > 0
		}
		return cmp < 0
	}
	if c.reverse {
		return a.ID > b.ID
	}
	return a.ID < b.ID
}

func (c *Collection[T]) lowerBound(item Item[T]) int {
	return sort.Search(len(c.items), func(i int) bool {
		return !c.sortsBefore(c.items[i], item)
	})
}

func (c *Collection[T]) sortAll() {
	sort.SliceStable(c.items, func(i, j int) bool {
		return c.sortsBefore(c.items[i], c.items[j])
	})
}
