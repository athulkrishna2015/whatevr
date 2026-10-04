package conn

import (
	"context"
	"github.com/rs/zerolog"
	"testing"
	"time"
)

func TestDarwinWatchersCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := WatchNetwork(ctx, zerolog.Nop())
	if n == nil {
		t.Fatal("no network observer")
	}
	done := make(chan struct{})
	go func() { WatchSleep(ctx, zerolog.Nop(), func() { t.Error("unexpected wake") }); close(done) }()
	cancel()
	select {
	case <-n.(*darwinNetwork).done:
	case <-time.After(3 * time.Second):
		t.Fatal("network observer did not stop")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("power observer did not stop")
	}
}
