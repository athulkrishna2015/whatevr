package whatsapp

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAskedFetchesGoBeforeAutoOnes(t *testing.T) {
	ctx := context.Background()
	a := newAdmission(1)
	first, err := a.take(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan string, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	wait := func(name string, asked bool) {
		defer wg.Done()
		release, err := a.take(ctx, asked)
		if err != nil {
			t.Error(err)
			return
		}
		order <- name
		release()
	}
	go wait("auto", false)
	time.Sleep(20 * time.Millisecond)
	go wait("asked", true)
	time.Sleep(20 * time.Millisecond)
	first()
	first() // a second release is nothing
	if got := <-order + " " + <-order; got != "asked auto" {
		t.Fatalf("ran %s", got)
	}
	wg.Wait()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.free != 1 {
		t.Fatalf("%d free after all released", a.free)
	}
}

func TestAWaitThatGivesUpHoldsNoSlot(t *testing.T) {
	a := newAdmission(1)
	held, _ := a.take(context.Background(), true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := a.take(ctx, false); err == nil {
		t.Fatal("took a slot that was held")
	}
	held()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.free != 1 || len(a.auto) != 0 {
		t.Fatalf("%d free, %d waiting", a.free, len(a.auto))
	}
}
