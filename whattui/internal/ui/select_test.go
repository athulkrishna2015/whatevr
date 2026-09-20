package ui

import (
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
)

// Nobody puts the pointer on the first character before pressing. A drag that
// starts beside the words has to pick them up when it reaches them, and it has
// to start from their first character rather than from the empty ground the
// press landed on.
func TestADragStartedBesideTheWordsStillSelectsThem(t *testing.T) {
	a := benchApp(100, 26, 4, 4)
	a.paint()

	words, ok := firstBlock(a)
	if !ok {
		t.Fatal("nothing on the frame is selectable")
	}
	// Three columns short of the words, on the same row.
	start := point{col: words.Col - 3, row: words.Row}
	if _, on := a.blockAt(start); on {
		t.Skip("there is no empty ground beside this block")
	}

	a.onSelectPress(start)
	if a.sel.on {
		t.Fatal("a press on empty ground already shows a selection")
	}
	if dirty := a.onSelectMotion(point{col: start.col + 1, row: start.row}); dirty {
		t.Fatal("a drag still over empty ground shows a selection")
	}

	into := point{col: words.Col + 2, row: words.Row}
	if !a.onSelectMotion(into) {
		t.Fatal("a drag that reached the words selected nothing")
	}
	if a.sel.from.col != words.Col {
		t.Errorf("selection starts at column %d, want the first character at %d", a.sel.from.col, words.Col)
	}
	if a.sel.to != into {
		t.Errorf("selection ends at %+v, want %+v", a.sel.to, into)
	}
	// And it stays in the block it found, however far the drag runs on.
	a.onSelectMotion(point{col: words.Col + words.Width + 20, row: words.Row})
	if a.sel.to.col != words.Col+words.Width-1 {
		t.Errorf("selection ran to column %d, past the block's last at %d",
			a.sel.to.col, words.Col+words.Width-1)
	}
}

// firstBlock is the first selectable run on the frame with empty ground beside
// it, which is where a drag that starts short of the words would begin.
func firstBlock(a *App) (layout.Rect, bool) {
	for _, b := range a.blocks {
		if b.Col <= 3 || b.Width <= 4 {
			continue
		}
		if _, taken := a.blockAt(point{col: b.Col - 3, row: b.Row}); taken {
			continue
		}
		return b, true
	}
	return layout.Rect{}, false
}

// A mouse event carries the size the terminal had when the pointer moved. A
// resize between the event and the frame is ordinary, and every reader of that
// position is reading cells: the pointer has to be pulled inside the screen
// before any of them see it.
func TestAPointerFromBeforeAResizeDoesNotCrash(t *testing.T) {
	a := benchApp(120, 40, 4, 6)
	a.paint()

	a.vx.Resize(vaxisResize(94, 24))
	a.paint()

	// Where the pointer was on the old screen, which is off the new one.
	if got := a.onScreen(vaxis.Mouse{Col: 200, Row: 99}); got.Col != 93 || got.Row != 23 {
		t.Errorf("a pointer at 200,99 on a 94x24 screen is %d,%d, want 93,23", got.Col, got.Row)
	}
	if got := a.onScreen(vaxis.Mouse{Col: -4, Row: -2}); got.Col != 0 || got.Row != 0 {
		t.Errorf("a pointer at -4,-2 is %d,%d, want 0,0", got.Col, got.Row)
	}

	for _, m := range []vaxis.Mouse{
		{Col: 110, Row: 35, EventType: vaxis.EventMotion},
		{Col: 110, Row: 35, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress},
		{Col: 200, Row: 99, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion},
		{Col: 200, Row: 99, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease},
		{Col: -3, Row: -1, EventType: vaxis.EventMotion},
	} {
		a.onMouse(m)
	}
	a.paint()
}
