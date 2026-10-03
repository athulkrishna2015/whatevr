package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// the race detector's test: sessions are walked while a hello lands
func TestSessionsWhileHelloLands(t *testing.T) {
	s, c := start(t, &fakeList{})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				s.Sessions()
			}
		}
	}()
	c.hello()
	close(stop)
	<-done
	if n := len(s.Sessions()); n != 1 {
		t.Fatalf("%d sessions after hello", n)
	}
}

func TestDisconnectCancelsCommands(t *testing.T) {
	s, c := start(t, &fakeList{})
	started, cancelled := make(chan struct{}), make(chan struct{})
	s.Handle(protoreflect.FieldNumber(v2.Request_ChatArchive_case), func(ctx context.Context, _ *Session, _ *v2.Request) (*v2.Response, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, nil
	})
	c.hello()
	c.send(func(r *v2.Request) { r.SetChatArchive(&v2.ChatArchive{}) })
	<-started
	c.nc.Close()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("a command outlived its connection")
	}
}

// past maxInFlight running commands the connection stops reading until one
// is done
func TestInFlightCapHoldsReads(t *testing.T) {
	s, c := start(t, &fakeList{})
	var running atomic.Int32
	release := make(chan struct{})
	s.Handle(protoreflect.FieldNumber(v2.Request_ChatArchive_case), func(ctx context.Context, _ *Session, _ *v2.Request) (*v2.Response, error) {
		running.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	c.hello()
	for range maxInFlight + 1 {
		c.send(func(r *v2.Request) { r.SetChatArchive(&v2.ChatArchive{}) })
	}
	waitFor := func(n int32) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); running.Load() != n; {
			if time.Now().After(deadline) {
				t.Fatalf("%d running, want %d", running.Load(), n)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor(maxInFlight)
	time.Sleep(50 * time.Millisecond)
	if n := running.Load(); n != maxInFlight {
		t.Fatalf("%d running past the cap of %d", n, maxInFlight)
	}
	release <- struct{}{}
	waitFor(maxInFlight + 1)
	close(release)
}
