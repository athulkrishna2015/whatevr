package views

import (
	"slices"
	"sync"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// previews keeps each chat's preview, the costly part of a chat row. a fold
// that touches a chat drops its preview, one that touches a person drops
// the previews that name them.
type previews struct {
	mu sync.Mutex
	// gen moves on every drop. a preview built from reads that began before
	// one is not kept: it may be older than the drop.
	gen    uint64
	byKey  map[string]previewEntry
	byAddr map[string]string
	// byPerson is the chats whose previews name an address, "self" for us
	byPerson map[string]map[string]bool
}

type previewEntry struct {
	addrs []string
	named map[string]bool
	p     *v2.ChatPreview
}

func newPreviews() *previews {
	return &previews{byKey: map[string]previewEntry{}, byAddr: map[string]string{}, byPerson: map[string]map[string]bool{}}
}

// begin is the generation a build starts from.
func (c *previews) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// get is key's preview, nil for a chat with nothing in it.
func (c *previews) get(key string) (*v2.ChatPreview, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byKey[key]
	return e.p, ok
}

// put keeps p for key, naming the people in named, unless something was
// dropped since gen.
func (c *previews) put(gen uint64, key string, addrs []string, named map[string]bool, p *v2.ChatPreview) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	c.dropKeyLocked(key)
	c.byKey[key] = previewEntry{addrs: addrs, named: named, p: p}
	for _, a := range addrs {
		c.byAddr[a] = key
	}
	for a := range named {
		if c.byPerson[a] == nil {
			c.byPerson[a] = map[string]bool{}
		}
		c.byPerson[a][key] = true
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

// dropPeople forgets the previews that name any of people. us changing
// drops them all: who we are decides which row a preview shows.
func (c *previews) dropPeople(people ...string) {
	if slices.Contains(people, "self") {
		c.dropAll()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	for _, a := range people {
		for k := range c.byPerson[a] {
			c.dropKeyLocked(k)
		}
	}
}

func (c *previews) dropAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	clear(c.byKey)
	clear(c.byAddr)
	clear(c.byPerson)
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
	for a := range e.named {
		delete(c.byPerson[a], key)
		if len(c.byPerson[a]) == 0 {
			delete(c.byPerson, a)
		}
	}
}
