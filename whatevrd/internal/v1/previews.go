package v1

import "sync"

// previews keeps each chat's preview as v1 shows it, the costly part of a
// chat row. a fold that touches a chat drops its preview, one that touches a
// person drops them all (a group preview names its sender). only what the
// new core alone decides is kept: a preview the old store had a say in is
// built each time, as the old core tells the views of its changes itself.
type previews struct {
	mu sync.Mutex
	// gen moves on every drop. a preview built from reads that began before
	// one is not kept: it may be older than the drop.
	gen    uint64
	byKey  map[string]previewEntry
	byAddr map[string]string
}

type previewEntry struct {
	addrs []string
	p     preview
}

type preview struct {
	ok                   bool
	text                 string
	direction, msgStatus string
}

func newPreviews() *previews {
	return &previews{byKey: map[string]previewEntry{}, byAddr: map[string]string{}}
}

// begin is the generation a build starts from.
func (c *previews) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *previews) get(key string) (preview, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byKey[key]
	return e.p, ok
}

// put keeps p for key unless something was dropped since gen.
func (c *previews) put(gen uint64, key string, addrs []string, p preview) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	c.dropKeyLocked(key)
	c.byKey[key] = previewEntry{addrs: addrs, p: p}
	for _, a := range addrs {
		c.byAddr[a] = key
	}
}

// drop forgets the previews of the chats addrs are in, and of the chats
// keyed by them.
func (c *previews) drop(addrs ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	for _, a := range addrs {
		if k, ok := c.byAddr[a]; ok {
			c.dropKeyLocked(k)
		}
		c.dropKeyLocked(a)
	}
}

func (c *previews) dropAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	clear(c.byKey)
	clear(c.byAddr)
}

func (c *previews) dropKeyLocked(key string) {
	e, ok := c.byKey[key]
	if !ok {
		return
	}
	delete(c.byKey, key)
	for _, a := range e.addrs {
		if c.byAddr[a] == key {
			delete(c.byAddr, a)
		}
	}
}
