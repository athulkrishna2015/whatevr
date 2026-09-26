package view

import (
	"encoding/json"
	"sync"
)

// Object is an object view: one item, delivered through the same upsert
// grammar. connection, login, self, preferences, sync, privacy and group are
// all object views.
type Object[T any] struct {
	mu      sync.RWMutex
	value   T
	raw     json.RawMessage
	present bool
	ready   bool
	version uint64

	OnDecodeError func(raw json.RawMessage, err error)
}

// NewObject returns an empty object view.
func NewObject[T any]() *Object[T] { return &Object[T]{} }

// Value is the current item, and whether one has arrived.
func (o *Object[T]) Value() (T, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.value, o.present
}

// Raw is the bytes the item arrived as, for reaching a field the struct does
// not model.
func (o *Object[T]) Raw() json.RawMessage {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.raw
}

// IsReady reports whether the view has been fully populated at least once.
func (o *Object[T]) IsReady() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.ready
}

// Version bumps on every applied change.
func (o *Object[T]) Version() uint64 {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.version
}

// Upsert replaces the item wholesale.
func (o *Object[T]) Upsert(_ string, raw json.RawMessage) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		o.mu.RLock()
		cb := o.OnDecodeError
		o.mu.RUnlock()
		if cb != nil {
			cb(raw, err)
		}
		return
	}
	o.mu.Lock()
	o.value, o.raw, o.present = value, append(json.RawMessage(nil), raw...), true
	o.version++
	o.mu.Unlock()
}

// Remove clears the item.
func (o *Object[T]) Remove(string) { o.clear() }

// Reset clears the item and un-readies the view.
func (o *Object[T]) Reset() {
	o.clear()
	o.mu.Lock()
	o.ready = false
	o.mu.Unlock()
}

// Ready records that the view is populated.
func (o *Object[T]) Ready(bool, bool) {
	o.mu.Lock()
	o.ready = true
	o.version++
	o.mu.Unlock()
}

// An object view is one item, so there is nothing for a transaction to
// protect: an upsert is already atomic under the lock.
func (o *Object[T]) BatchBegin() {}
func (o *Object[T]) BatchEnd()   {}

func (o *Object[T]) clear() {
	o.mu.Lock()
	defer o.mu.Unlock()
	var zero T
	o.value, o.raw, o.present = zero, nil, false
	o.version++
}
