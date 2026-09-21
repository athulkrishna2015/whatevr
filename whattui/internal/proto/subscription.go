package proto

import (
	"encoding/json"
	"sync"
)

// Direction is which frontier an extend grows.
const (
	Older = "older"
	Newer = "newer"
)

// Subscription is one live view. It owns the daemon-assigned sub id, routes
// events to its sink, and re-issues itself across reconnects, so a caller
// subscribes once and never thinks about the socket again.
type Subscription struct {
	client *Client
	view   string
	params Params
	sink   ViewSink

	mu       sync.Mutex
	subID    uint64
	haveID   bool
	closed   bool
	pending  []pendingExtend
	meta     map[string]any
	OnMeta   func(meta map[string]any)
	OnFailed func(err *Error)
	// OnExtendFailed fires when an extend is rejected. Without it a list that
	// cannot grow spins: the caller's in-flight guard never clears.
	OnExtendFailed func(direction string, err *Error)
}

type pendingExtend struct {
	count     int
	direction string
}

// View is the view name this subscribes to.
func (s *Subscription) View() string { return s.view }

// Meta is the subscribe result minus `sub`, e.g. anchor_id for a messages view
// anchored at unread. Empty until the response lands.
func (s *Subscription) Meta() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta
}

// Active reports whether the daemon has answered the subscribe and the
// subscription has not been closed.
func (s *Subscription) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.haveID && !s.closed
}

// Extend grows the window. An extend issued before the subscribe response
// lands is queued and flushed when the sub id arrives, so a caller never has
// to wait for one to send the other.
func (s *Subscription) Extend(count int, direction string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if !s.haveID {
		s.pending = append(s.pending, pendingExtend{count, direction})
		s.mu.Unlock()
		return
	}
	id := s.subID
	s.mu.Unlock()
	s.sendExtend(id, count, direction)
}

func (s *Subscription) sendExtend(subID uint64, count int, direction string) {
	s.client.Do("extend", Params{"sub": subID, "count": count, "direction": direction},
		func(_ json.RawMessage, err *Error) {
			if err != nil && s.OnExtendFailed != nil {
				s.OnExtendFailed(direction, err)
			}
		})
}

// Close unsubscribes. Safe to call more than once.
func (s *Subscription) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	id, have := s.subID, s.haveID
	s.mu.Unlock()

	s.client.forget(s)
	if have {
		s.client.Do("unsubscribe", Params{"sub": id}, nil)
	}
}

// adopt records the sub id the daemon assigned and flushes queued extends.
func (s *Subscription) adopt(subID uint64, meta map[string]any) []pendingExtend {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subID, s.haveID = subID, true
	s.meta = meta
	queued := s.pending
	s.pending = nil
	return queued
}

// orphan drops the sub id, which is what a reconnect does before re-issuing.
func (s *Subscription) orphan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subID, s.haveID = 0, false
}
