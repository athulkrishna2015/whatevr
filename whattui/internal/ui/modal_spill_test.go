package ui

import (
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/theme"
)

// A two cell emoji one column left of a panel paints its second half inside
// the panel, on the border. The panel has to blank it from its own side.
func TestAPanelBlanksWhatSpillsIntoIt(t *testing.T) {
	a := benchApp(100, 26, 4, 0)
	win := a.vx.Window()
	r := layout.Rect{Col: 10, Row: 2, Width: 20, Height: 5}

	style := vaxis.Style{Foreground: a.theme.Text}
	for row := r.Row; row < r.Row+r.Height; row++ {
		win.New(r.Col-1, row, 2, 1).Fill(vaxis.Cell{
			Character: vaxis.Character{Grapheme: "🧪", Width: 2},
			Style:     style,
		})
	}
	a.guardSpill(win, r)

	for row := r.Row; row < r.Row+r.Height; row++ {
		if c := a.vx.Cell(r.Col-1, row); c.Grapheme != " " {
			t.Fatalf("row %d still holds %q, which reaches into the panel", row, c.Grapheme)
		}
		if c := a.vx.Cell(r.Col-1, row); c.Style != style {
			t.Fatalf("row %d lost the style under the glyph", row)
		}
	}
	// A narrow glyph is nobody's business but its own.
	win.New(r.Col-1, r.Row, 1, 1).Fill(vaxis.Cell{
		Character: vaxis.Character{Grapheme: "x", Width: 1},
		Style:     style,
	})
	a.guardSpill(win, r)
	if c := a.vx.Cell(r.Col-1, r.Row); c.Grapheme != "x" {
		t.Fatalf("a one cell glyph was blanked: %q", c.Grapheme)
	}
}

// A panel is dismissed the same way wherever it came from: escape, the two
// keys a terminal user reaches for, or a click on anything that is not it.
func TestEveryWayOutOfAModal(t *testing.T) {
	for _, out := range []func(a *App){
		func(a *App) { a.onKey(vaxis.Key{Keycode: vaxis.KeyEsc}) },
		func(a *App) { a.onKey(vaxis.Key{Keycode: 'c', Modifiers: vaxis.ModCtrl}) },
		func(a *App) { a.onKey(vaxis.Key{Keycode: 'g', Modifiers: vaxis.ModCtrl}) },
		func(a *App) {
			a.onMouse(vaxis.Mouse{Col: 0, Row: 0, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
		},
	} {
		a := benchApp(100, 26, 2, 0)
		a.openModal(modalPalette)
		a.paint()
		out(a)
		if a.modal.kind != modalNone {
			t.Errorf("the palette survived a dismissal, kind %d", a.modal.kind)
		}
	}
}

// Everything behind a panel fades, cells and rasterised words alike.
func TestAModalDimsWhatIsBehindIt(t *testing.T) {
	a := benchApp(100, 26, 4, 0)
	// A real palette, because indexed colours are the terminal's own and the
	// fade is computed rather than guessed.
	a.theme = theme.Derive(vaxis.RGBColor(0x10, 0x11, 0x14), vaxis.RGBColor(0xe6, 0xe6, 0xe6))
	a.paint()
	col, row := -1, -1
	for r := 0; r < 26 && col < 0; r++ {
		for c := 0; c < 30; c++ {
			cell := a.vx.Cell(c, r)
			if cell.Grapheme != "" && cell.Grapheme != " " && len(cell.Style.Foreground.Params()) == 3 {
				col, row = c, r
				break
			}
		}
	}
	if col < 0 {
		t.Fatal("the chat list drew no coloured text to dim")
	}
	before := a.vx.Cell(col, row)
	if before.Style.Attribute&vaxis.AttrDim != 0 {
		t.Fatal("the chat list is already dim with no panel open")
	}

	a.openModal(modalPalette)
	a.paint()
	after := a.vx.Cell(col, row)
	if after.Style.Attribute&vaxis.AttrDim == 0 {
		t.Error("a cell behind the panel was not dimmed")
	}
	if after.Style.Foreground == before.Style.Foreground {
		t.Error("a cell behind the panel kept its full ink")
	}
	r := a.modal.rect
	inside := a.vx.Cell(r.Col+2, r.Row+2)
	if inside.Style.Attribute&vaxis.AttrDim != 0 {
		t.Error("the panel dimmed itself")
	}
}
