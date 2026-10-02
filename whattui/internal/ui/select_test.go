package ui

import (
	"testing"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
)

// Nobody puts the pointer on the first character before pressing. A drag that
// starts beside the words has to pick them up when it reaches them, and it has
// to start from their first character rather than from the empty ground the
// press landed on.
func TestADragStartedBesideTheWordsStillSelectsThem(t *testing.T) {
	a := stubApp(100, 26, 4, 4)
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

// A chat row is a button. Dragging down a column of them would hand back the
// daemon's own summaries cut into pieces, so the list records no blocks and
// nothing in it can be taken.
func TestTheChatListIsNotSelectable(t *testing.T) {
	a := stubApp(100, 26, 8, 4)
	a.paint()

	list := a.layout().ChatList
	if list.Empty() {
		t.Fatal("no chat list on this frame")
	}
	for row := list.Row; row < list.Row+list.Height; row++ {
		for col := list.Col; col < list.Col+list.Width; col++ {
			if b, ok := a.blockAt(point{col: col, row: row}); ok {
				t.Fatalf("cell %d,%d in the chat list is inside block %+v", col, row, b)
			}
		}
	}

	// And a drag down the names selects nothing, rather than selecting the
	// cells it crossed.
	start := point{col: list.Col + 2, row: list.Row}
	a.onSelectPress(start)
	a.onSelectMotion(point{col: list.Col + 8, row: list.Row + 2})
	if a.sel.on {
		t.Fatal("a drag down the chat list made a selection")
	}
	if a.onSelectRelease() {
		t.Fatal("the drag was taken as a selection on release")
	}

	// The pointer says so before the drag starts: a hand over a row you can
	// open, never a beam over words you cannot take.
	if got := a.shapeFor(vaxis.Mouse{Col: start.col, Row: start.row}, 0); got != vaxis.MouseShapeClickable {
		t.Errorf("pointer over a chat row is %v, want the hand", got)
	}
	if got := a.shapeFor(vaxis.Mouse{Col: start.col, Row: start.row}, -1); got != vaxis.MouseShapeDefault {
		t.Errorf("pointer over the list off any row is %v, want the arrow", got)
	}
}

// A touchpad goes on sending notches after the fingers have left it. The
// pointer wandering onto the other pane mid-glide must not hand it the rest of
// the flick, stopping one pane and shoving another for one gesture.
func TestAGlideKeepsMovingThePaneItStartedIn(t *testing.T) {
	a := stubApp(100, 26, 60, 40)
	a.paint()
	l := a.layout()
	if l.ChatList.Empty() || l.Transcript.Empty() {
		t.Fatal("this frame has only one pane to scroll")
	}

	wheel := func(r layout.Rect) vaxis.Mouse {
		return vaxis.Mouse{
			Col: r.Col + 1, Row: r.Row + 1,
			Button: vaxis.MouseWheelUp, EventType: vaxis.EventPress,
		}
	}
	// A flick that starts in the transcript.
	a.onMouse(wheel(l.Transcript))
	scrolled := a.conversation.scroll
	if scrolled == 0 {
		t.Fatal("the first notch moved nothing")
	}

	// The rest of it arrives with the pointer over the chat list.
	for i := 0; i < 4; i++ {
		a.onMouse(wheel(l.ChatList))
	}
	if a.listTop != 0 {
		t.Errorf("the chat list scrolled to %d on a glide that started elsewhere", a.listTop)
	}
	if got := a.conversation.scroll; got <= scrolled {
		t.Errorf("the transcript stopped at %d when the pointer left it, want it still moving", got)
	}

	// A hand back on the pad takes the scroll at once, without waiting for the
	// glide to die: the notches speed up, and a glide only ever slows down.
	a.mu.Lock()
	a.wheelAt, a.wheelGap = time.Now(), 80*time.Millisecond
	a.mu.Unlock()
	before := a.conversation.scroll
	boost := wheel(l.ChatList)
	boost.Button = vaxis.MouseWheelDown
	a.onMouse(boost)
	if a.listTop == 0 {
		t.Error("scrolling again over the chat list did not take the scroll")
	}
	if a.conversation.scroll != before {
		t.Error("the boost moved the transcript as well")
	}

	// And a gesture after everything has stopped belongs to whatever it is
	// over, with nothing clever needed.
	a.mu.Lock()
	a.wheelAt, a.wheelGap = time.Now().Add(-time.Second), 0
	a.listTop = 0
	a.mu.Unlock()
	a.onMouse(wheel(l.Transcript))
	if a.conversation.scroll <= before {
		t.Error("a fresh gesture over the transcript did not move it")
	}
	if a.listTop != 0 {
		t.Error("a fresh gesture over the transcript moved the chat list as well")
	}
}

// A mouse event carries the size the terminal had when the pointer moved. A
// resize between the event and the frame is ordinary, and every reader of that
// position is reading cells: the pointer has to be pulled inside the screen
// before any of them see it.
func TestAPointerFromBeforeAResizeDoesNotCrash(t *testing.T) {
	a := stubApp(120, 40, 4, 6)
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
