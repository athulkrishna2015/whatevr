package wa

import (
	"context"
	"sync"
	"sync/atomic"
)

// accountSession is the lifetime of one logged-in account. Every worker the
// account starts runs under it and every detached write borrows its context,
// so ending it cancels the work and waits for it before the tables are
// emptied. Detached writers used to outlive the wipe and keep writing rows
// into a database that had just been cleared.
type accountSession struct {
	gen    uint64
	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	ended bool
	wg    sync.WaitGroup
}

var accountSessionGen atomic.Uint64

func newAccountSession(parent context.Context) *accountSession {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &accountSession{gen: accountSessionGen.Add(1), ctx: ctx, cancel: cancel}
}

// alive reports whether this session still owns the account. Check it after
// every goroutine start, after every network round trip, and on each iteration
// of a loop that writes rows.
func (s *accountSession) alive() bool {
	return s != nil && s.ctx.Err() == nil
}

// detached is the context for work that has to outlive the message that
// started it but not the account. This is what context.WithoutCancel used to
// buy, minus the part where it also outlived the logout.
func (s *accountSession) detached() context.Context {
	if s == nil {
		return context.Background()
	}
	return s.ctx
}

// spawn runs fn under this session. Nothing starts once the session has ended.
func (s *accountSession) spawn(fn func(context.Context)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		fn(s.ctx)
	}()
}

// end cancels and drains. Idempotent.
func (s *accountSession) end() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ended = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
}
