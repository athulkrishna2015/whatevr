package wa

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
)

// Reading privacy settings is an IQ to the server when the cache is cold, and
// whatsmeow runs handlers one at a time. The handler has to hand the read off
// and return, and a burst of category changes has to collapse into one re-read
// rather than one goroutine each.
func TestPrivacySettingsChangeDoesNotBlockTheEventQueue(t *testing.T) {
	client := &Client{daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}
	client.eventGen.Store(1)

	var reads atomic.Int64
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	client.privacyFetch = func(context.Context) (app.PrivacySettings, error) {
		reads.Add(1)
		started <- struct{}{}
		<-release
		return app.PrivacySettings{}, nil
	}

	returned := make(chan struct{})
	go func() {
		client.handleEvent(1, &events.PrivacySettings{})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler is still sitting on the privacy read, so every event behind it is too")
	}

	// The read is in flight. Two more changes land while it is.
	<-started
	client.handleEvent(1, &events.PrivacySettings{})
	client.handleEvent(1, &events.PrivacySettings{})

	close(release)
	client.runWG.Wait()

	if got := reads.Load(); got != 2 {
		t.Fatalf("three changes caused %d reads, want 2 (the one in flight plus one catch-up)", got)
	}
}
