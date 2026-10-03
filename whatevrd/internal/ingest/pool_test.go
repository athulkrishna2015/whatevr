package ingest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolRunsEachKeyOnceAndNeverMoreThanItsSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var now, most atomic.Int32
	var mu sync.Mutex
	ran := map[string]int{}
	var wg sync.WaitGroup
	p := newPool("test", 3, 0, func(s string) string { return s }, func(ctx context.Context, s string) error {
		n := now.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		now.Add(-1)
		mu.Lock()
		ran[s]++
		mu.Unlock()
		wg.Done()
		return nil
	})
	for i := range 20 {
		wg.Add(1)
		if !p.add(fmt.Sprint(i)) {
			t.Fatalf("job %d taken as a duplicate", i)
		}
		// a key still queued or running is the same job
		if p.add(fmt.Sprint(i)) {
			t.Fatalf("job %d queued twice", i)
		}
	}
	p.start(ctx)
	wg.Wait()
	if m := most.Load(); m > 3 || m < 2 {
		t.Errorf("at most %d ran at once, want up to 3", m)
	}
	for i := range 20 {
		if ran[fmt.Sprint(i)] != 1 {
			t.Errorf("job %d ran %d times", i, ran[fmt.Sprint(i)])
		}
	}
	// done jobs can come again: a later event may ask for the same thing
	wg.Add(1)
	if !p.add("0") {
		t.Fatal("a finished key refused")
	}
	wg.Wait()
}

func TestPoolRetriesAFailingJobWhileOthersGoOn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var tries atomic.Int32
	done := make(chan string, 2)
	p := newPool("test", 2, 0, func(s string) string { return s }, func(ctx context.Context, s string) error {
		if s == "flaky" && tries.Add(1) < 2 {
			return errors.New("network")
		}
		done <- s
		return nil
	})
	p.add("flaky")
	p.add("fine")
	p.start(ctx)
	first := <-done
	if first != "fine" {
		t.Errorf("%s finished first, the flaky job held the pool up", first)
	}
	select {
	case s := <-done:
		if s != "flaky" || tries.Load() != 2 {
			t.Errorf("%s after %d tries", s, tries.Load())
		}
	case <-time.After(retryFirst + 2*time.Second):
		t.Fatal("the failed job was never tried again")
	}
	// the worker marks it done just after run returns
	for end := time.Now().Add(time.Second); p.pending() != 0; time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("%d jobs left", p.pending())
		}
	}
}
