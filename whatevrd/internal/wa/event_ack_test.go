package wa

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
)

// The handler's bool decides whether whatsmeow acks. Answering false means the
// server delivers the event again, so anything that will not get better on a
// redelivery has to answer true or it loops forever. Only a failed store may
// answer false.
func TestHandleEventAcksWhatRedeliveryCannotFix(t *testing.T) {
	client := &Client{daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}

	// An event for a superseded account: its database is gone, and asking for
	// it again would only replay it into the wrong one.
	dead := newAccountSession(context.Background())
	dead.end()
	if !client.handleEvent(dead, &events.Blocklist{}) {
		t.Fatal("a superseded session refused the ack, which would redeliver forever")
	}

	// An event with no message in it stores nothing and never will.
	live := newAccountSession(context.Background())
	defer live.end()
	if !client.handleEvent(live, &events.Blocklist{}) {
		t.Fatal("a non-message event refused the ack")
	}
}
