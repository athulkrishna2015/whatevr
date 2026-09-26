package notify

import (
	"context"
	"testing"

	"whatevrd/internal/app"
)

// A daemon with no session bus has no worker. main builds a nil interface for
// that case, but a nil *Worker reaching an interface anywhere else used to
// segfault the whole daemon on the first incoming message, which is every
// headless machine.
func TestNilWorkerDoesNotNotify(t *testing.T) {
	var w *Worker
	w.NotifyMessage(context.Background(), app.Message{}, app.Chat{ID: "chat@s.whatsapp.net"}, Options{})
}
