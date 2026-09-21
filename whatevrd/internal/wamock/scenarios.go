//go:build whatevr_mock

package wamock

import (
	"fmt"
	"sort"
	"sync"
)

// Scenario builds the world a mock run serves. Stage 0 has nothing to build
// yet, so the registry exists mainly so --mock validates its argument and
// --mock-list has something to print.
type Scenario struct {
	Name        string
	Description string

	// Configure adjusts the server before it binds.
	Configure func(o *Options)
}

var (
	registryMu sync.Mutex
	registry   = map[string]Scenario{}
)

// Register adds a scenario. Scenario packages call it from init.
func Register(s Scenario) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[s.Name]; exists {
		panic(fmt.Sprintf("wamock: scenario %q registered twice", s.Name))
	}
	registry[s.Name] = s
}

func Lookup(name string) (Scenario, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	s, ok := registry[name]
	return s, ok
}

func List() []Scenario {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]Scenario, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func init() {
	Register(Scenario{
		Name:        "empty",
		Description: "a freshly paired account with no chats",
	})
}
