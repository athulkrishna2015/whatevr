package ui

import (
	"testing"

	"whattui/internal/paint"
	"whattui/internal/term"
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
	typed := goldenApp(100, 30, term.TierColor)
	typed.paint()
	col, row, height, ok := ruleOf(typed)
	if !ok {
		t.Fatal("the typed tier drew no rule")
	}

	painted := goldenApp(100, 30, term.TierGraphics)
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
	a := goldenApp(120, 40, term.TierGraphics)
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
	a := goldenApp(120, 40, term.TierGraphics)
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
	a := goldenApp(100, 30, term.TierGraphics)
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
	a := goldenApp(100, 30, term.TierGraphics)
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
