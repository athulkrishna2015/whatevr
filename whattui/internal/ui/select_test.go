package ui

import (
	"testing"

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
