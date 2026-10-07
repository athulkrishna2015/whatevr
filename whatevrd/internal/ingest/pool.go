package ingest

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// pool runs one kind of network job, at most size at once, each key once
// while it is queued or running. a job that errors is retried with backoff
// by the worker that ran it; a job that can never work must log its failure
// as an input and return nil, or it is retried forever.
type pool[T any] struct {
	name string
	size int
	// gap is the pause a worker takes after each job: servers that rate
	// limit get asked at a pace
	gap time.Duration
	key func(T) string
	run func(context.Context, T) error

	mu      sync.Mutex
	queue   []T
	known   map[string]bool
	running int
	wake    chan struct{}
	// drained says a job finished, to the one waiting in below
	drained chan struct{}
}

func newPool[T any](name string, size int, gap time.Duration, key func(T) string, run func(context.Context, T) error) *pool[T] {
	return &pool[T]{name: name, size: size, gap: gap, key: key, run: run,
		known: map[string]bool{}, wake: make(chan struct{}, size), drained: make(chan struct{}, 1)}
}

func (p *pool[T]) start(ctx context.Context) {
	for range p.size {
		go p.worker(ctx)
	}
}

// add queues t unless its key is queued or running. false is a duplicate.
func (p *pool[T]) add(t T) bool {
	k := p.key(t)
	p.mu.Lock()
	if p.known[k] {
		p.mu.Unlock()
		return false
	}
	p.known[k] = true
	p.queue = append(p.queue, t)
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return true
}

// pending is how many jobs are queued or running.
func (p *pool[T]) pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queue) + p.running
}

func (p *pool[T]) next() (T, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var t T
	if len(p.queue) == 0 {
		return t, false
	}
	t = p.queue[0]
	p.queue = p.queue[1:]
	p.running++
	return t, true
}

func (p *pool[T]) done(t T) {
	p.mu.Lock()
	p.running--
	delete(p.known, p.key(t))
	p.mu.Unlock()
	select {
	case p.drained <- struct{}{}:
	default:
	}
}

// below waits until fewer than n jobs are queued or running, so whoever
// feeds the pool from the store holds a page at a time. false is ctx ending.
func (p *pool[T]) below(ctx context.Context, n int) bool {
	for p.pending() >= n {
		select {
		case <-p.drained:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

const (
	retryFirst = 2 * time.Second
	retryLast  = 5 * time.Minute
)

func (p *pool[T]) worker(ctx context.Context) {
	for {
		t, ok := p.next()
		if !ok {
			select {
			case <-p.wake:
				continue
			case <-ctx.Done():
				return
			}
		}
		if !p.work(ctx, t) {
			return
		}
		p.done(t)
		if !sleep(ctx, p.gap) {
			return
		}
	}
}

// work runs t until it works. false is ctx ending first.
func (p *pool[T]) work(ctx context.Context, t T) bool {
	retry := time.Duration(0)
	for {
		err := p.run(ctx, t)
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		retry = min(max(retry*2, retryFirst), retryLast)
		zerolog.Ctx(ctx).Warn().Err(err).Str("pool", p.name).Str("job", p.key(t)).Dur("retry", retry).Msg("ingest: job failed")
		if !sleep(ctx, retry) {
			return false
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
