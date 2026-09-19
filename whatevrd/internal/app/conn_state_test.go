package app

import (
	"sync"
	"testing"
)

// State, detail and retry metadata used to be five independent atomics with
// three writer goroutines, so a published event could carry the state from one
// moment and the detail from another: "Offline" alongside "Connected to
// WhatsApp". They are one value behind one lock now, written together and read
// together, so no reader can catch the connection half-updated.
func TestConnectionSnapshotIsNeverMixed(t *testing.T) {
	d := NewDaemon(Paths{})

	online := func() {
		d.SetConnection(StateOnline, "Connected to WhatsApp", 0, 0, false)
	}
	offline := func() {
		d.SetConnection(StateOffline, "Still offline. Check your internet connection.", 4, 1_700_000_000, true)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			if i%2 == 0 {
				online()
			} else {
				offline()
			}
		}
		close(stop)
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			state, detail, _, _, canReconnect := d.ConnectionSnapshot()
			switch state {
			case StateOnline:
				if detail != "Connected to WhatsApp" || canReconnect {
					t.Errorf("online state carried %q (canReconnect=%t)", detail, canReconnect)
					return
				}
			case StateOffline:
				if detail != "Still offline. Check your internet connection." || !canReconnect {
					t.Errorf("offline state carried %q (canReconnect=%t)", detail, canReconnect)
					return
				}
			}
		}
	}()
	wg.Wait()
}
