package ui

import (
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
)

// sessions is every session.update the frontend made, in order.
func sessions(calls []sentRequest) []sentRequest {
	var out []sentRequest
	for _, call := range calls {
		if call.method == "session_update" {
			out = append(out, call)
		}
	}
	return out
}

// The daemon suppresses a notification for the chat the focused frontend has
// open, and routes a notification click to the frontend that is in front. Both
// are decided by what this window last said about itself, so a terminal in a
// background tab that still claims the focus takes the notifications for that
// chat away from every other window, and the click with them.
func TestLeavingTheTerminalTellsTheDaemonItIsNotInFrontAnyMore(t *testing.T) {
	a := stubApp(90, 24, 4, 6)
	calls := records(a)

	a.handle(vaxis.FocusOut{})
	said := sessions(*calls)
	if len(said) != 1 {
		t.Fatalf("leaving the terminal made %d session updates, want one", len(said))
	}
	if got := said[0].req.GetSessionUpdate().GetFocused(); got != false {
		t.Errorf("the update says focused=%v, want false", got)
	}
	if got := said[0].req.GetSessionUpdate().GetActiveChatId(); got != a.activeChat {
		t.Errorf("the update names chat %v, want the open one %q", got, a.activeChat)
	}

	// A terminal reports focus per switch, not per change: the same answer
	// twice is not news, and the daemon is not told twice.
	a.handle(vaxis.FocusOut{})
	if len(sessions(*calls)) != 1 {
		t.Errorf("the same focus event twice made %d updates, want one", len(sessions(*calls)))
	}

	a.handle(vaxis.FocusIn{})
	said = sessions(*calls)
	if len(said) != 2 {
		t.Fatalf("coming back made %d updates in total, want two", len(said))
	}
	if got := said[1].req.GetSessionUpdate().GetFocused(); got != true {
		t.Errorf("coming back says focused=%v, want true", got)
	}
}

// The session belongs to the connection, so a daemon that has just come back
// has never heard of this window.
func TestAReconnectedDaemonIsToldWhereThisWindowStands(t *testing.T) {
	a := stubApp(90, 24, 4, 6)
	calls := records(a)

	a.onTransport(proto.Ready, nil, nil)
	said := sessions(*calls)
	if len(said) != 1 {
		t.Fatalf("a fresh connection made %d session updates, want one", len(said))
	}
	if s := said[0].req.GetSessionUpdate(); s.GetFocused() != true || s.GetActiveChatId() != a.activeChat {
		t.Errorf("the update says %v, want this window's own state", s)
	}
}
