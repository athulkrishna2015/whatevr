package view

import (
	"bytes"
	"encoding/json"
	"sort"
)

// The ViewSink half of Collection. The client calls all of these on its own
// goroutine, one at a time.

// Upsert inserts or replaces the item with this item.id.
func (c *Collection[T]) Upsert(sortKey string, raw json.RawMessage) {
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || head.ID == "" {
		c.reportDecode(raw, err)
		return
	}

	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		c.reportDecode(raw, err)
		return
	}

	// Raw is handed on rather than aliased: the client reuses its read buffer,
	// so the bytes under this slice are the next frame by the time anything
	// renders it.
	item := Item[T]{ID: head.ID, Sort: sortKey, Value: value, Raw: bytes.Clone(raw)}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inBatch {
		c.pending = append(c.pending, op[T]{item: item})
		return
	}
	c.applyUpsert(item)
	c.version++
}

// Remove deletes the item with this id.
func (c *Collection[T]) Remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inBatch {
		c.pending = append(c.pending, op[T]{remove: true, item: Item[T]{ID: id}})
		return
	}
	c.applyRemove(id)
	c.version++
}

// Ready records that the window is fully populated for the last subscribe or
// extend.
func (c *Collection[T]) Ready(exhausted, hasExhausted bool) {
	c.mu.Lock()
	c.ready = true
	c.exhausted = exhausted
	c.hasExhausted = hasExhausted
	c.version++
	c.mu.Unlock()
}

// Reset discards the local copy. Fresh upserts follow, then Ready.
func (c *Collection[T]) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = nil
	c.index = make(map[string]int)
	c.pending = nil
	c.inBatch = false
	c.ready = false
	c.exhausted = false
	c.hasExhausted = false
	c.version++
}

// BatchBegin opens a transaction.
func (c *Collection[T]) BatchBegin() {
	c.mu.Lock()
	c.inBatch = true
	c.mu.Unlock()
}

// BatchEnd applies everything that arrived in one read, under one lock, so no
// reader ever sees half a window.
func (c *Collection[T]) BatchEnd() {
	c.mu.Lock()
	defer c.mu.Unlock()

	pending := c.pending
	c.pending = nil
	c.inBatch = false
	if len(pending) == 0 {
		return
	}

	// Ops apply in arrival order. That is what makes a remove followed by a
	// re-upsert of the same id resolve the way the wire meant it.
	if len(pending) <= rebuildThreshold {
		for _, o := range pending {
			if o.remove {
				c.applyRemove(o.item.ID)
			} else {
				c.applyUpsert(o.item)
			}
		}
		c.version++
		return
	}

	// A big batch is cheaper to settle in a map and rebuild once than to
	// insert into the middle of a slice item by item.
	byID := make(map[string]Item[T], len(c.items)+len(pending))
	for _, it := range c.items {
		byID[it.ID] = it
	}
	for _, o := range pending {
		if o.remove {
			delete(byID, o.item.ID)
			continue
		}
		byID[o.item.ID] = o.item
	}

	c.items = c.items[:0]
	for _, it := range byID {
		c.items = append(c.items, it)
	}
	c.sortAll()
	// The index is rebuilt, not patched: reindex only writes the ids that are
	// still here, so anything this batch removed would keep a stale row.
	c.index = make(map[string]int, len(c.items))
	c.reindex(0)
	c.version++
}

func (c *Collection[T]) reportDecode(raw json.RawMessage, err error) {
	c.mu.RLock()
	cb := c.OnDecodeError
	c.mu.RUnlock()
	if cb != nil {
		cb(raw, err)
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

// never swap places between renders.
// sortsBefore is the whole of the ordering: a bytewise comparison of the
// opaque sort string, with the id as a stable tiebreak so two items that sort
// equal never swap places between renders.
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
