package ui

import (
	"fmt"
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whattui/internal/view"
)

// A frame drawn while the daemon is writing must not stop the application. Go
// hands a waiting writer the lock ahead of a second reader, so a draw that
// asked the collection anything while it held a read lock deadlocked with the
// next batch: a frozen terminal that answers no key.
func TestDrawingWhileTheDaemonWritesNeverDeadlocks(t *testing.T) {
	a := stubApp(100, 26, 0, 0)
	a.chats = view.NewCollection(view.Chat)

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
			update(a.chats, v2.ViewUpdate_builder{})
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

// The transcript is the one that actually froze. Drawing it holds the message
// collection open for the whole frame, so anything inside that frame which
// asks the collection a question of its own waits behind the daemon's next
// message, and the daemon's next message waits behind the frame.
func TestDrawingATranscriptWhileMessagesArriveNeverDeadlocks(t *testing.T) {
	a := stubApp(100, 26, 4, 20)
	c := a.conversation

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
			putMsg(c.msgs, fmt.Sprintf("%020d", 1000+i), v2.MessageRow_builder{
				Id: fmt.Sprintf("live%d", i), TextBody: &v2.Text{}, Text: "one more", Sender: person("x", "someone"),
			}.Build())
		}
	}()
	<-writing
	defer close(stop)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 400; i++ {
			a.paint()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("a transcript frame and an arriving message deadlocked")
	}
}
