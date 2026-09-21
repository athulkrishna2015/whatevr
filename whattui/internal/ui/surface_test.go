package ui

import (
	"testing"
	"time"

	"whattui/internal/paint"
	"whattui/internal/term"
	"whattui/internal/textrun"
)

// ruleOf finds the first speaker rule in the transcript, in cells: the column
// it stands in, the row it starts at, and how far it runs.
func ruleOf(a *App) (col, row, height int, ok bool) {
	pane := a.layout().Transcript
	for c := pane.Col; c < pane.Col+pane.Width; c++ {
		for r := pane.Row; r < pane.Row+pane.Height; r++ {
			if g := a.vx.Cell(c, r).Grapheme; g != "▎" && g != "|" {
				continue
			}
			bottom := r
			for bottom+1 < pane.Row+pane.Height {
				if g := a.vx.Cell(c, bottom+1).Grapheme; g != "▎" && g != "|" {
					break
				}
				bottom++
			}
			return c, r, bottom - r + 1, true
		}
	}
	return 0, 0, 0, false
}

// A tier changes the ink and never the position. What the graphics tier draws
// as a hairline is exactly the column the tier below fills with a glyph, which
// is the whole promise of "geometry is tier independent".
func TestAPaintedRuleCoversTheCellsATypedOneWouldHave(t *testing.T) {
	typed := tierApp(100, 30, term.TierColor)
	typed.paint()
	col, row, height, ok := ruleOf(typed)
	if !ok {
		t.Fatal("the typed tier drew no rule")
	}

	painted := tierApp(100, 30, term.TierGraphics)
	painted.paint()
	if _, _, _, ok := ruleOf(painted); ok {
		t.Fatal("the painted tier drew rule glyphs as well as chrome")
	}
	for _, s := range painted.surfaces {
		if _, isRule := s.spec.(paint.Rule); !isRule {
			continue
		}
		if s.col == col && s.row == row {
			if s.w != 1 || s.h != height {
				t.Fatalf("the rule is %dx%d cells, the glyphs were 1x%d", s.w, s.h, height)
			}
			return
		}
	}
	t.Fatalf("nothing was painted at %d,%d, where the rule is", col, row)
}

// A panel is a hole in the frame. An image at a negative z is drawn over the
// cell background, so filling the panel does not cover a bubble under it: the
// placement has to go, and only the covered part of it.
func TestAPanelTakesTheChromeUnderItWithIt(t *testing.T) {
	a := tierApp(120, 40, term.TierGraphics)
	a.paint()
	before := len(a.surfaces)
	if before == 0 {
		t.Fatal("no chrome to cover")
	}

	a.openModal(modalPalette)
	a.paint()
	if len(a.surfaces) == 0 {
		t.Fatal("the palette took every bubble on the screen with it")
	}

	win := a.vx.Window()
	w, h := win.Size()
	panel := a.modalRect(w, h)
	for _, s := range a.surfaces {
		if s.row < panel.Row+panel.Height && s.row+s.h > panel.Row &&
			s.col < panel.Col+panel.Width && s.col+s.w > panel.Col {
			t.Fatalf("chrome at %d,%d %dx%d shows through the panel at %+v", s.col, s.row, s.w, s.h, panel)
		}
	}
}

// A panel dims what is behind it. Chrome is an image and an image ignores the
// dim attribute a cell carries, so it has to be repainted darker.
func TestChromeBehindAPanelFadesWithEverythingElse(t *testing.T) {
	a := tierApp(120, 40, term.TierGraphics)
	a.openModal(modalPalette)
	a.paint()
	if len(a.surfaces) == 0 {
		t.Fatal("no chrome on this frame")
	}
	for _, s := range a.surfaces {
		if _, ok := s.spec.(paint.Fade); !ok {
			t.Fatalf("chrome at %d,%d stayed bright behind the panel", s.col, s.row)
		}
	}
}

// A surface the pane cuts in half is the same upload placed at a different
// source rectangle. Cropping the pixels instead would rasterise and upload a
// new image for every row a transcript scrolls.
func TestChromeClippedByAPaneIsTheSameUpload(t *testing.T) {
	a := tierApp(100, 30, term.TierGraphics)
	a.paint()
	a.flushImages()

	ids := map[uint64]int{}
	clipped := 0
	for _, p := range a.vx.Snapshot().Placements() {
		if p.ZIndex != chromeZ {
			continue
		}
		ids[p.ImageID]++
		if p.SourceWidth > 0 {
			clipped++
		}
	}
	if clipped == 0 {
		t.Skip("nothing is clipped at this size")
	}
	for _, n := range ids {
		if n > 1 {
			return
		}
	}
	t.Fatal("every placement has an upload of its own")
}

// Boxes are the opt-in shape, and the words inside one never set the box's own
// colour as a cell background: the image already carries that ground, and a
// cell painted with it would tint the picture a second time.
func TestTextInABoxKeepsThePanesGround(t *testing.T) {
	a := tierApp(100, 30, term.TierGraphics)
	a.boxed = true
	a.paint()

	boxes := 0
	for _, s := range a.surfaces {
		if _, ok := s.spec.(paint.Bubble); !ok {
			continue
		}
		boxes++
		for row := s.row; row < s.row+s.h; row++ {
			for col := s.col; col < s.col+s.w; col++ {
				if bg := a.vx.Cell(col, row).Background; bg != a.theme.Background {
					t.Fatalf("cell %d,%d under a box has background %v, want the pane's %v",
						col, row, bg, a.theme.Background)
				}
			}
		}
	}
	if boxes == 0 {
		t.Fatal("no boxes on this frame")
	}
}

// A terminal can have graphics and still not scale text, and those are two
// different questions. Where there are pixels the letter belongs in the disc:
// drawn at the size of the circle, centred on it, and nothing typed into the
// cells underneath.
func TestADiscDrawsItsOwnLetter(t *testing.T) {
	a := tierApp(120, 40, term.TierGraphics)
	a.caps.TextScale = false
	a.shaper = shaperForTest(t)
	a.paint()

	for _, s := range a.surfaces {
		d, ok := s.spec.(paint.Disc)
		if !ok || s.h < 2 {
			continue
		}
		if d.Glyph == nil {
			t.Fatal("the disc has no letter in it, so the terminal was asked to draw one")
		}
		if s.w != 4 {
			t.Fatalf("disc is %d cells wide, want 4", s.w)
		}
		for row := s.row; row < s.row+s.h; row++ {
			for col := s.col; col < s.col+s.w; col++ {
				if c := a.vx.Cell(col, row); c.Grapheme != " " && c.Grapheme != "" {
					t.Fatalf("cell %d,%d under the disc holds %q as well", col, row, c.Grapheme)
				}
			}
		}
		return
	}
	t.Fatal("no two row disc on the frame")
}

// And with no pixels to draw into, the terminal draws the letter and the block
// it needs is claimed either way, so nothing moves when the font turns up.
func TestADiscWithoutPixelsTypesItsLetter(t *testing.T) {
	a := tierApp(120, 40, term.TierColor)
	a.paint()
	if len(a.surfaces) != 0 {
		t.Fatalf("a tier that cannot draw painted %d surfaces", len(a.surfaces))
	}
	for row := 0; row < 4; row++ {
		for col := 0; col < 6; col++ {
			if c := a.vx.Cell(col, row); c.Grapheme != " " && c.Grapheme != "" {
				return
			}
		}
	}
	t.Fatal("nothing was typed where the avatar should be")
}

func shaperForTest(t *testing.T) *textrun.Shaper {
	t.Helper()
	sh := textrun.New(textrun.Options{})
	sh.SetCellSize(10, 20)
	deadline := time.Now().Add(30 * time.Second)
	for !sh.Begin() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !sh.Begin() {
		t.Skip("no usable font index on this machine")
	}
	return sh
}

// A resize invalidates every image the terminal is holding: it was drawn for a
// cell that no longer exists, and terminals disagree about whether they tidy
// that up. The frame after one starts from nothing.
func TestAResizeDropsEveryGraphicTheTerminalHolds(t *testing.T) {
	a := tierApp(120, 40, term.TierGraphics)
	a.paint()
	a.flushImages()
	if len(a.vx.Snapshot().Placements()) == 0 {
		t.Fatal("nothing was placed to begin with")
	}

	a.handle(vaxisResize(94, 24))
	if got := len(a.vx.Snapshot().Placements()); got != 0 {
		t.Fatalf("%d placements survived the resize", got)
	}
}

// A font size change is the one that leaves ink at the wrong scale: the window
// stands still and every image in it was drawn for a cell that is now a
// different size.
func TestAFontSizeChangeThrowsAwayWhatWasDrawnForTheOldCell(t *testing.T) {
	a := tierApp(120, 40, term.TierGraphics)
	a.paint()
	a.flushImages()
	if len(a.images) == 0 {
		t.Fatal("nothing was rasterised to begin with")
	}

	// The same window, a bigger font: fewer, larger cells.
	a.handle(fontResize(80, 26, 15, 30))
	if len(a.images) != 0 {
		t.Fatalf("%d images survived a cell that changed size", len(a.images))
	}
	if got := len(a.vx.Snapshot().Placements()); got != 0 {
		t.Fatalf("%d placements survived the font size change", got)
	}
	// And the frame after it draws at the size the terminal is now.
	a.paint()
	a.flushImages()
	if w, h := a.cellPix(); w != 15 || h != 30 {
		t.Fatalf("cell is %dx%d, want 15x30", w, h)
	}
	if len(a.surfaces) == 0 {
		t.Fatal("the frame after a font size change drew no chrome")
	}
}
