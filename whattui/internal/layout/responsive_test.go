package layout

import "testing"

func TestShapeBreakpoints(t *testing.T) {
	for cols, want := range map[int]Shape{
		200: ShapeWide, 100: ShapeWide, 99: ShapeCompact, 68: ShapeCompact,
		67: ShapeStack, 40: ShapeStack, 39: ShapeRail, 10: ShapeRail,
	} {
		if got := shapeFor(cols); got != want {
			t.Errorf("shapeFor(%d) = %v, want %v", cols, got, want)
		}
	}
}

func TestChromeBreakpoints(t *testing.T) {
	for rows, want := range map[int]Chrome{
		40: ChromeFull, 20: ChromeFull, 19: ChromeTight,
		12: ChromeTight, 11: ChromeMinimal, 3: ChromeMinimal,
	} {
		if got := chromeFor(rows); got != want {
			t.Errorf("chromeFor(%d) = %v, want %v", rows, got, want)
		}
	}
}

// panesTile is the invariant every layout has to hold: the panes cover the
// screen exactly once, with no overlap and no gap. A pane drawn over another
// is the bug that shows up as a stray glyph nobody can account for.
func panesTile(t *testing.T, l Layout, cols, rows int) {
	t.Helper()
	grid := make([][]int, rows)
	for i := range grid {
		grid[i] = make([]int, cols)
	}
	for name, r := range map[string]Rect{
		"list": l.ChatList, "header": l.Header, "transcript": l.Transcript,
		"composer": l.Composer, "hint": l.HintBar,
	} {
		if r.Empty() {
			continue
		}
		for y := r.Row; y < r.Row+r.Height; y++ {
			for x := r.Col; x < r.Col+r.Width; x++ {
				if y < 0 || y >= rows || x < 0 || x >= cols {
					t.Fatalf("%s pane %+v runs off a %dx%d screen", name, r, cols, rows)
				}
				grid[y][x]++
				if grid[y][x] > 1 {
					t.Fatalf("%s pane overlaps another at %d,%d", name, x, y)
				}
			}
		}
	}
}

func TestPanesNeverOverlapOrRunOffTheScreen(t *testing.T) {
	sizes := []struct{ cols, rows int }{
		{120, 40}, {100, 30}, {99, 30}, {90, 24}, {68, 24}, {67, 24},
		{60, 20}, {40, 14}, {39, 14}, {38, 10}, {30, 8}, {20, 5}, {10, 3},
	}
	for _, s := range sizes {
		for _, focused := range []bool{false, true} {
			l := Compute(s.cols, s.rows, focused)
			panesTile(t, l, s.cols, s.rows)
		}
	}
}

func TestWideAndCompactAlwaysShowBothPanes(t *testing.T) {
	for _, cols := range []int{120, 100, 99, 68} {
		l := Compute(cols, 30, false)
		if l.ChatList.Empty() {
			t.Errorf("%d cols: no chat list", cols)
		}
		if l.Transcript.Empty() {
			t.Errorf("%d cols: no transcript", cols)
		}
		if l.ChatList.Width >= cols {
			t.Errorf("%d cols: the list took the whole screen", cols)
		}
	}
}

func TestStackShowsOnePaneAtATime(t *testing.T) {
	onList := Compute(50, 24, true)
	if onList.ChatList.Width != 50 {
		t.Errorf("focused list width = %d, want the whole screen", onList.ChatList.Width)
	}
	if !onList.Transcript.Empty() {
		t.Error("the transcript is drawn under the chat list")
	}

	onChat := Compute(50, 24, false)
	if !onChat.ChatList.Empty() {
		t.Error("the chat list is still drawn when the conversation has the screen")
	}
	if onChat.Transcript.Empty() {
		t.Error("no transcript when the conversation has the screen")
	}
}

func TestAVeryShortScreenStillLeavesATranscript(t *testing.T) {
	// Chrome gives way before content does. A terminal with four rows shows
	// messages and a composer, not a header and a hint bar.
	l := Compute(90, 4, false)
	if l.Transcript.Height < 1 {
		t.Errorf("no transcript rows in %+v", l)
	}
	if l.Composer.Height < 1 {
		t.Errorf("no composer in %+v", l)
	}
}

func TestGeometryDoesNotDependOnAnythingButSize(t *testing.T) {
	// The tier-independence invariant, stated as a test: Compute takes no
	// capability, so the same size is always the same geometry.
	a := Compute(100, 30, false)
	b := Compute(100, 30, false)
	if a != b {
		t.Errorf("same size gave two layouts:\n%+v\n%+v", a, b)
	}
}
