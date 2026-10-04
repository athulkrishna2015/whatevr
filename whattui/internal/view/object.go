package view

import (
	"sync"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// Object is an object view: one item, delivered through the same updates.
// connection, login, self and chat are object views.
type Object[T any] struct {
	extract func(*v2.Upsert) (T, bool)

	mu      sync.RWMutex
	value   T
	present bool
	ready   bool
	version uint64

	OnMismatch func(*v2.Upsert)
}

// NewObject returns an empty object view whose row extract takes out of an
// upsert.
func NewObject[T any](extract func(*v2.Upsert) (T, bool)) *Object[T] {
	return &Object[T]{extract: extract}
}

// Value is the current item, and whether one has arrived.
func (o *Object[T]) Value() (T, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.value, o.present
}

// IsReady reports whether the view has been filled since it was last emptied.
func (o *Object[T]) IsReady() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.ready
}

// Version bumps on every applied update.
func (o *Object[T]) Version() uint64 {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.version
}

// Apply takes one update whole.
func (o *Object[T]) Apply(u *v2.ViewUpdate, fresh bool) {
	var bad []*v2.Upsert
	var zero T
	o.mu.Lock()
	if u.GetReset() || fresh {
		o.value, o.present, o.ready = zero, false, false
	}
	for _, ch := range u.GetChanges() {
		switch ch.WhichChange() {
		case v2.Change_Upsert_case:
			v, ok := o.extract(ch.GetUpsert())
			if !ok {
				bad = append(bad, ch.GetUpsert())
				continue
			}
			o.value, o.present = v, true
		case v2.Change_Remove_case:
			o.value, o.present = zero, false
		}
	}
	if u.HasReady() {
		o.ready = true
	}
	o.version++
	onMismatch := o.OnMismatch
	o.mu.Unlock()
	if onMismatch != nil {
		for _, up := range bad {
			onMismatch(up)
		}
	}
}
