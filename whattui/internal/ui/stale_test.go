package ui

import (
	"strings"
	"syscall"
	"testing"

	"whattui/internal/proto"
)

// Everything in the chat list and the name over the transcript is the daemon's
// answer about right now. With the socket gone there is nobody to answer, and a
// list of chats that cannot move looks exactly like one that can.
func TestADeadSocketDrawsNoChatsAndNoChatName(t *testing.T) {
	a := stubApp(90, 24, 4, 6)
	cols, rows := a.vx.Window().Size()

	a.paint()
	if !strings.Contains(rowText(a, 0, cols), "contact 0") {
		t.Fatal("the fixture never drew a chat list to begin with")
	}

	a.mu.Lock()
	a.transport, a.lastErr = proto.Disconnected, syscall.ENOENT
	a.mu.Unlock()
	a.paint()

	list := a.layout().ChatList
	for row := 0; row < rows; row++ {
		text := rowText(a, row, list.Col+list.Width)
		if strings.Contains(text, "contact") {
			t.Fatalf("row %d still lists a chat: %q", row, text)
		}
	}
	header := rowText(a, a.layout().Header.Row, cols)
	if strings.Contains(header, "contact") {
		t.Fatalf("the header still names a chat: %q", header)
	}
	if !strings.Contains(header, "whattui") {
		t.Fatalf("the header says %q, want our own name", header)
	}
	// And the reader is told why the list is empty rather than left guessing.
	if !strings.Contains(rowText(a, 1, cols), "whatevrd") {
		t.Errorf("the empty list says nothing about the daemon: %q", rowText(a, 1, cols))
	}

	// The rows themselves are still held, so the socket coming back is a
	// redraw rather than a refetch.
	a.mu.Lock()
	a.transport, a.lastErr = proto.Ready, nil
	a.mu.Unlock()
	a.paint()
	if !strings.Contains(rowText(a, 0, cols), "contact 0") {
		t.Fatal("the list did not come back with the socket")
	}
}

// Unpairing the phone is the same thing from the other side. The daemon empties
// its store and every row in the list becomes a row about an account nobody is
// logged into, so the list says what is wrong instead of going on showing them.
func TestAnUnpairedPhoneDrawsNoChatsAndNoChatName(t *testing.T) {
	a := stubApp(90, 24, 4, 6)
	cols, rows := a.vx.Window().Size()

	a.paint()
	if !strings.Contains(rowText(a, 0, cols), "contact 0") {
		t.Fatal("the fixture never drew a chat list to begin with")
	}

	a.conn.Upsert("", mustJSON(proto.Connection{State: "need_login"}))
	a.paint()

	list := a.layout().ChatList
	for row := 0; row < rows; row++ {
		if text := rowText(a, row, list.Col+list.Width); strings.Contains(text, "contact") {
			t.Fatalf("row %d still lists a chat: %q", row, text)
		}
	}
	header := rowText(a, a.layout().Header.Row, cols)
	if strings.Contains(header, "contact") {
		t.Fatalf("the header still names a chat: %q", header)
	}
	if !strings.Contains(rowText(a, 1, cols), "not paired") {
		t.Errorf("the empty list says nothing about the phone: %q", rowText(a, 1, cols))
	}

	// And pairing again is a redraw: the rows were never thrown away.
	a.conn.Upsert("", mustJSON(proto.Connection{State: "online"}))
	a.paint()
	if !strings.Contains(rowText(a, 0, cols), "contact 0") {
		t.Fatal("the list did not come back with the account")
	}
}
