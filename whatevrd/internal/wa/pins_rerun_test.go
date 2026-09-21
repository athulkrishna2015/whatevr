package wa

import (
	"context"
	"sync"
	"testing"
	"time"
)

// A recovery asked for while one is already running must still happen.
//
// One pass runs at a time, and a second request used to be dropped on the spot.
// On a fresh login that is the normal case rather than a rare race: the
// connect-time reconcile is already in flight, reading app state the device does
// not have yet, when the event saying the state has arrived fires. Dropping that
// request left the empty snapshot as the last word, and ReconcileChatPins is
// full authority, so the account finished its first sync with no pins at all.
func TestPinRecoveryAskedForWhileRunningRunsAgain(t *testing.T) {
	c := &Client{session: newAccountSession(context.Background())}
	defer c.session.end()

	var mu sync.Mutex
	runs := 0
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	c.startPinnedChatRecovery("test", func(context.Context) error {
		mu.Lock()
		runs++
		first := runs == 1
		mu.Unlock()
		if first {
			close(started)
			<-release // hold the first pass open so the second request collides
		} else {
			close(done)
		}
		return nil
	})

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first recovery never started")
	}

	// Asked for again while the first is still running.
	c.startPinnedChatRecovery("test", func(context.Context) error {
		t.Error("a second goroutine was started while one was already running")
		return nil
	})
	close(release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the request that arrived mid-pass was dropped instead of re-run")
	}

	mu.Lock()
	defer mu.Unlock()
	if runs != 2 {
		t.Fatalf("recovery ran %d times, want 2", runs)
	}
}
