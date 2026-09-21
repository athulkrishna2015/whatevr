package ui

import (
	"testing"

	"go.rockorager.dev/vaxis"
)

func arrow(code rune) vaxis.Key { return vaxis.Key{Keycode: code} }

// The gesture the whole thing hangs on: up on an empty composer is your last
// message, and down off the end gives the keyboard back.
func TestUpOnAnEmptyComposerPicksTheNewestMessage(t *testing.T) {
	a := stubApp(100, 26, 4, 8)
	a.paint()
	ids := a.conversation.messageIDs()
	if len(ids) < 3 {
		t.Fatalf("window holds %d pointable messages, want a few", len(ids))
	}

	a.onKey(arrow(vaxis.KeyUp))
	if got := a.cursor(); got != ids[0] {
		t.Fatalf("first press points at %q, want the newest %q", got, ids[0])
	}
	if a.focus != FocusTranscript {
		t.Fatalf("focus is %v after pointing at a message, want the transcript", a.focus)
	}

	a.onKey(arrow(vaxis.KeyUp))
	if got := a.cursor(); got != ids[1] {
		t.Fatalf("second press points at %q, want the one before at %q", got, ids[1])
	}
	a.onKey(arrow(vaxis.KeyDown))
	if got := a.cursor(); got != ids[0] {
		t.Fatalf("down points at %q, want back at %q", got, ids[0])
	}

	// Past the newest there is nothing to point at, which is where you were
	// about to type.
	a.onKey(arrow(vaxis.KeyDown))
	if got := a.cursor(); got != "" {
		t.Fatalf("down off the end still points at %q", got)
	}
	if a.focus != FocusComposer {
		t.Fatalf("focus is %v after letting go, want the composer", a.focus)
	}
}

// The cursor is one message of a run, the same way the pointer is, and it says
// so in a glyph as well as in colour.
func TestTheCursorLightsOneMessageAndMarksIt(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	a.onKey(arrow(vaxis.KeyUp))
	a.paint()

	id := a.cursor()
	var at, other messageAt
	for _, m := range a.messages {
		if m.id == id {
			at = m
		} else if other.id == "" {
			other = m
		}
	}
	if at.id == "" || other.id == "" {
		t.Fatalf("frame drew %d messages, want the cursor and another", len(a.messages))
	}

	if bg := a.vx.Cell(at.at.Col+1, at.at.Row).Background; bg != a.theme.BackgroundActive {
		t.Errorf("the message under the cursor has ground %v, want %v", bg, a.theme.BackgroundActive)
	}
	if bg := a.vx.Cell(other.at.Col+1, other.at.Row).Background; bg == a.theme.BackgroundActive {
		t.Error("the whole run lit up, not the message the cursor is on")
	}
	if g := a.vx.Cell(at.at.Col, at.at.Row).Grapheme; g != "▎" {
		t.Errorf("the margin beside the cursor says %q, want the mark", g)
	}
}

// The window follows the cursor. Penning it into the rows that happen to be
// visible would make the arrows stop working halfway up a conversation.
func TestTheTranscriptScrollsToFollowTheCursor(t *testing.T) {
	a := stubApp(100, 26, 4, 40)
	a.paint()

	for i := 0; i < 25; i++ {
		a.onKey(arrow(vaxis.KeyUp))
		a.paint()
	}
	c := a.conversation
	if c.scroll == 0 {
		t.Fatal("the cursor walked back through the window and nothing scrolled")
	}

	viewport := a.transcriptPage()
	above, height, ok := c.messageAbove(a.cursor())
	if !ok {
		t.Fatal("the cursor is on a message the layout does not hold")
	}
	top := viewport + c.scroll - above
	if top < 0 || top+height > viewport {
		t.Fatalf("the cursor sits at rows %d..%d of a pane %d tall", top, top+height, viewport)
	}
}

// A cursor and a draft never share the screen, which is what keeps a bare
// letter and the escape key unambiguous.
func TestTypingLetsGoOfTheCursor(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	a.onKey(arrow(vaxis.KeyUp))
	if a.cursor() == "" {
		t.Fatal("nothing was pointed at to let go of")
	}
	a.onKey(key('h'))
	if got := a.cursor(); got != "" {
		t.Fatalf("typing left the cursor on %q", got)
	}
	if a.composer.String() != "h" {
		t.Fatalf("draft is %q, want the letter that was typed", a.composer.String())
	}
}

// Escape pops exactly one level, and the cursor is a level.
func TestEscapeDropsTheCursorBeforeThePane(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	a.onKey(arrow(vaxis.KeyUp))

	a.onKey(arrow(vaxis.KeyEsc))
	if got := a.cursor(); got != "" {
		t.Fatalf("escape left the cursor on %q", got)
	}
	if a.focus != FocusTranscript {
		t.Fatalf("escape left the transcript as well as the cursor, focus is %v", a.focus)
	}
	a.onKey(arrow(vaxis.KeyEsc))
	if a.focus != FocusComposer {
		t.Fatalf("the second escape left focus at %v, want the composer", a.focus)
	}
}

// With no cursor the arrows are what they always were. Reading a transcript
// row by row is not the same job as walking it message by message.
func TestWithoutACursorTheArrowsStillScroll(t *testing.T) {
	a := stubApp(100, 26, 4, 40)
	a.paint()
	a.setFocus(FocusTranscript)

	a.onKey(arrow(vaxis.KeyUp))
	if got := a.conversation.scroll; got != 1 {
		t.Fatalf("scroll is %d after one arrow with no cursor, want 1", got)
	}
	if got := a.cursor(); got != "" {
		t.Fatalf("scrolling pointed at %q", got)
	}
}

// Everything the keyboard can do the pointer can do too.
func TestClickingAMessagePointsAtIt(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	at := a.messages[0]

	press := vaxis.Mouse{Col: at.at.Col + 2, Row: at.at.Row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress}
	a.onMouse(press)
	release := press
	release.EventType = vaxis.EventRelease
	a.onMouse(release)

	if got := a.cursor(); got != at.id {
		t.Fatalf("a click on %q pointed at %q", at.id, got)
	}
}

// The window is the daemon's. A message that leaves it is not ours to go on
// pointing at, and an action against a row that is gone is an error nobody
// asked for.
func TestACursorOnAMessageThatLeavesTheWindowLetsGo(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	a.onKey(arrow(vaxis.KeyUp))
	id := a.cursor()
	if id == "" {
		t.Fatal("nothing was pointed at")
	}

	a.conversation.msgs.Remove(id)
	a.paint()

	if got := a.cursor(); got != "" {
		t.Fatalf("the cursor still points at %q, which the window no longer holds", got)
	}
}
