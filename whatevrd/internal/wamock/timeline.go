//go:build whatevr_mock

package wamock

import (
	"context"
	"sort"
	"time"
)

// timedAction is one scheduled scenario step, measured from the moment the
// frontend finished catching up rather than from process start. A scenario that
// wants a reply two seconds after the UI is usable means two seconds after the
// UI is usable.
type timedAction struct {
	after time.Duration
	fn    func()
}

// After schedules work once the daemon is connected and the backlog is
// delivered. Use it for anything a frontend should see happen, as opposed to
// anything it should find already there.
func (w *World) After(d time.Duration, fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timeline = append(w.timeline, timedAction{after: d, fn: fn})
}

// runTimeline fires the scheduled actions in order. It runs in its own
// goroutine for the life of the connection: a scenario that schedules something
// an hour out simply never gets there in a short run, which is fine.
func (w *World) runTimeline(ctx context.Context) {
	w.mu.Lock()
	actions := append([]timedAction(nil), w.timeline...)
	w.timeline = nil
	w.mu.Unlock()
	if len(actions) == 0 {
		return
	}
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].after < actions[j].after })

	start := time.Now()
	for _, action := range actions {
		wait := time.Until(start.Add(action.after))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
		action.fn()
	}
}
