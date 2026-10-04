package whatsapp

import (
	"context"
	"slices"
	"sync"
)

// mediaFetches is how many downloads and streams run at once; the rest wait
const mediaFetches = 4

// admission lets at most n media fetches run, what someone asked for ahead
// of what the preferences fetch on their own.
type admission struct {
	mu          sync.Mutex
	free        int
	asked, auto []chan struct{}
}

func newAdmission(n int) *admission { return &admission{free: n} }

// take waits for a slot and returns its release, which runs at most once.
func (a *admission) take(ctx context.Context, asked bool) (func(), error) {
	a.mu.Lock()
	if a.free > 0 {
		a.free--
		a.mu.Unlock()
		return a.releaser(), nil
	}
	ch := make(chan struct{})
	if asked {
		a.asked = append(a.asked, ch)
	} else {
		a.auto = append(a.auto, ch)
	}
	a.mu.Unlock()
	select {
	case <-ch:
		return a.releaser(), nil
	case <-ctx.Done():
		a.mu.Lock()
		queued := slices.Contains(a.asked, ch) || slices.Contains(a.auto, ch)
		a.asked = slices.DeleteFunc(a.asked, func(c chan struct{}) bool { return c == ch })
		a.auto = slices.DeleteFunc(a.auto, func(c chan struct{}) bool { return c == ch })
		a.mu.Unlock()
		if !queued {
			// handed a slot just as ctx ended
			a.release()
		}
		return nil, ctx.Err()
	}
}

func (a *admission) releaser() func() {
	var once sync.Once
	return func() { once.Do(a.release) }
}

// release hands the slot to the next waiting, or frees it.
func (a *admission) release() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, q := range []*[]chan struct{}{&a.asked, &a.auto} {
		if len(*q) > 0 {
			ch := (*q)[0]
			*q = (*q)[1:]
			close(ch)
			return
		}
	}
	a.free++
}
