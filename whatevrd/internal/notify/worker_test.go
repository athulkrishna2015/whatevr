package notify

import (
	"testing"

	"whatevrd/internal/live"
)

// without a session bus there is no worker, and a nil *Worker that reaches an
// interface used to take the daemon down on the first message
func TestNilWorkerDoesNotNotify(t *testing.T) {
	var w *Worker
	w.Show(live.Notification{ID: "chat"})
	w.Close("chat")
}
