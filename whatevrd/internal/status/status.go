// Package status is every way the daemon is not working right now, each
// with the one part of the daemon allowed to say so. a problem is set and
// cleared by its owner only, so two parts can never argue over one state.
//
// some problems are pushed by their owner as they happen (the connection);
// the rest are read off the core when asked (sync domains, the outbox, the
// store's health), so they cannot go stale.
package status

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Kind is one way of not working.
type Kind string

const (
	// owner conn
	NoNetwork     Kind = "no_network"
	StuckLogin    Kind = "stuck_before_login"
	Refused       Kind = "server_refused"
	LoggedOut     Kind = "logged_out"
	Banned        Kind = "temporary_ban"
	Outdated      Kind = "client_outdated"
	Replaced      Kind = "stream_replaced"
	KeepaliveLost Kind = "keepalive_lost"
	// owner sync: read off the log
	AppStateOutOfSync Kind = "app_state_out_of_sync"
	HistoryStalled    Kind = "history_sync_stalled"
	WaitingOnPhone    Kind = "waiting_on_phone_resend"
	// owner outbox
	OutboxFailing Kind = "outbox_failing"
	// owner media, still the old core's under --core new
	MediaFailing Kind = "media_failing"
	// owner core
	StoreFailing Kind = "database_error"
)

// owners says who may set each kind.
var owners = map[Kind]string{
	NoNetwork: "conn", StuckLogin: "conn", Refused: "conn", LoggedOut: "conn", Banned: "conn",
	Outdated: "conn", Replaced: "conn", KeepaliveLost: "conn",
	AppStateOutOfSync: "sync", HistoryStalled: "sync", WaitingOnPhone: "sync",
	OutboxFailing: "outbox",
	MediaFailing:  "media",
	StoreFailing:  "core",
}

// Owner is who may set k.
func Owner(k Kind) string { return owners[k] }

// Problem is one kind of trouble as it stands.
type Problem struct {
	Kind   Kind
	Since  time.Time
	Detail string
	// Next is when the owner tries again, zero when it does not on its own
	Next time.Time
}

// Provider reads its owner's problems off something that has them.
type Provider func(ctx context.Context) ([]Problem, error)

type Board struct {
	mu        sync.Mutex
	pushed    map[Kind]Problem
	providers map[string]Provider
	// OnChange sees each pushed problem set or cleared, nil for cleared.
	OnChange func(k Kind, p *Problem)
}

func New() *Board {
	return &Board{pushed: map[Kind]Problem{}, providers: map[string]Provider{}}
}

// Set is owner saying p holds. Since is kept from the last Set of the same
// kind, so a problem that changes its detail does not look new.
func (b *Board) Set(owner string, p Problem) error {
	if owners[p.Kind] != owner {
		return fmt.Errorf("status: %s may not set %s, %s owns it", owner, p.Kind, owners[p.Kind])
	}
	b.mu.Lock()
	if old, ok := b.pushed[p.Kind]; ok && !old.Since.IsZero() {
		p.Since = old.Since
	}
	if p.Since.IsZero() {
		p.Since = time.Now()
	}
	b.pushed[p.Kind] = p
	cb := b.OnChange
	b.mu.Unlock()
	if cb != nil {
		cb(p.Kind, &p)
	}
	return nil
}

// Clear is owner saying k is over.
func (b *Board) Clear(owner string, k Kind) error {
	if owners[k] != owner {
		return fmt.Errorf("status: %s may not clear %s, %s owns it", owner, k, owners[k])
	}
	b.mu.Lock()
	_, had := b.pushed[k]
	delete(b.pushed, k)
	cb := b.OnChange
	b.mu.Unlock()
	if had && cb != nil {
		cb(k, nil)
	}
	return nil
}

// Only is owner saying exactly these of its kinds hold now, the rest of its
// kinds are over.
func (b *Board) Only(owner string, ps ...Problem) error {
	keep := map[Kind]bool{}
	for _, p := range ps {
		if err := b.Set(owner, p); err != nil {
			return err
		}
		keep[p.Kind] = true
	}
	for k, o := range owners {
		if o == owner && !keep[k] {
			if err := b.Clear(owner, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// Provide has owner's problems read through p when asked.
func (b *Board) Provide(owner string, p Provider) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.providers[owner] = p
}

// List is every problem now, oldest first. a provider that fails is a
// problem of the store.
func (b *Board) List(ctx context.Context) []Problem {
	b.mu.Lock()
	out := make([]Problem, 0, len(b.pushed))
	for _, p := range b.pushed {
		out = append(out, p)
	}
	providers := make(map[string]Provider, len(b.providers))
	for o, p := range b.providers {
		providers[o] = p
	}
	b.mu.Unlock()
	for owner, read := range providers {
		ps, err := read(ctx)
		if err != nil {
			out = append(out, Problem{Kind: StoreFailing, Since: time.Now(), Detail: fmt.Sprintf("%s: %v", owner, err)})
			continue
		}
		for _, p := range ps {
			if owners[p.Kind] == owner {
				out = append(out, p)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.Before(out[j].Since)
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
