package ui

import (
	"testing"
	"time"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// A frame drawn while the daemon is writing must not stop the application. Go
// hands a waiting writer the lock ahead of a second reader, so a draw that
// asked the collection anything while it held a read lock deadlocked with the
// next batch: a frozen terminal that answers no key.
func TestDrawingWhileTheDaemonWritesNeverDeadlocks(t *testing.T) {
	a := benchApp(100, 26, 0, 0)
	a.chats = view.NewCollection[proto.ChatRow]()

	stop := make(chan struct{})
	writing := make(chan struct{})
	go func() {
		close(writing)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			a.chats.BatchBegin()
			a.chats.BatchEnd()
		}
	}()
	<-writing
	defer close(stop)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			a.paint()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("a draw and a daemon write deadlocked")
	}
}
