package ui

import (
	"image/color"
	"strings"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/textrun"
)

// The scripts a cell grid cannot hold are drawn rather than typed: shaped at
// the terminal's own em and baseline, rasterised, and placed as a kitty
// graphic over blank cells. The cells still carry the background, the
// underline and the selection; only the ink is ours.
//
// Three functions are the whole contract, and everything that lays anything
// out already goes through them: width measures, clip cuts, print draws. They
// have to agree on the same cell count for the same string, which is why they
// all ask the same shaper the same question.

// width is how many cells a string takes. Ordinary text is measured by the
// terminal's own rules; a complex word is measured by what we are about to
// draw, because the terminal's answer for it is the wrong answer.
func (a *App) width(s string) int {
	if !a.shaping || !textrun.Complex(s) {
		return a.vx.RenderedWidth(s)
	}
	n := 0
	for _, p := range textrun.Split(s) {
		if !p.Complex {
			n += a.vx.RenderedWidth(p.Text)
			continue
		}
		if r := a.shaper.Shape(p.Text, false, false); r != nil {
			n += r.Cells()
			continue
		}
		n += a.vx.RenderedWidth(p.Text)
	}
	return n
}

// clip cuts a string to a cell width, never mid-grapheme and never leaving
// half of a wide one behind. A complex part is cut at a cluster boundary and
// reshaped, so what comes back is a correctly shaped shorter phrase rather
// than a correct phrase with its right hand side missing.
func (a *App) clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if a.width(s) <= width {
		return s
	}
	if !a.shaping || !textrun.Complex(s) {
		return a.clipPlain(s, width)
	}

	var b strings.Builder
	used := 0
	for _, p := range textrun.Split(s) {
		room := width - used
		if room <= 0 {
			break
		}
		if !p.Complex {
			cut := a.clipPlain(p.Text, room)
			b.WriteString(cut)
			used += a.vx.RenderedWidth(cut)
			continue
		}
		r := a.shaper.Shape(p.Text, false, false)
		if r == nil {
			cut := a.clipPlain(p.Text, room)
			b.WriteString(cut)
			used += a.vx.RenderedWidth(cut)
			continue
		}
		if r.Cells() <= room {
			b.WriteString(p.Text)
			used += r.Cells()
			continue
		}
		head := r.Prefix(room)
		b.WriteString(head)
		break
	}
	return b.String()
}

func (a *App) clipPlain(s string, width int) string {
	if a.vx.RenderedWidth(s) <= width {
		return s
	}
	out, used := make([]rune, 0, len(s)), 0
	for _, r := range s {
		w := a.vx.RenderedWidth(string(r))
		if used+w > width {
			break
		}
		out = append(out, r)
		used += w
	}
	return string(out)
}

// print writes a string clipped to the window width, from one column, and
// returns the column after it. Every pane goes through this, which is how a
// column of glyphs keeps one left edge.
//
// The advance is the terminal's own width rules rather than a rune count: a
// chat name full of emoji is not as many cells wide as it is long, and
// guessing is how a column loses its edge.
func (a *App) print(win vaxis.Window, col, row int, style vaxis.Style, s string) int {
	w, h := win.Size()
	// Below the window as well as above it. A message taller than the pane is
	// drawn with its top off one end and its tail off the other, and a
	// rasterised word is an image with a position of its own: the terminal
	// clamps one placed past the last row onto the last row rather than
	// dropping it, which is a phrase piling up at the bottom of the
	// transcript instead of scrolling out of it.
	if row < 0 || row >= h || col >= w || s == "" {
		return col
	}
	if !a.shaping || !textrun.Complex(s) {
		return a.printPlain(win, col, row, style, s)
	}
	for _, p := range textrun.Split(s) {
		if col >= w {
			break
		}
		if !p.Complex {
			col = a.printPlain(win, col, row, style, p.Text)
			continue
		}
		run := a.shaper.Shape(p.Text, style.Attribute&vaxis.AttrBold != 0, style.Attribute&vaxis.AttrItalic != 0)
		if run == nil {
			col = a.printPlain(win, col, row, style, p.Text)
			continue
		}
		span := minInt(run.Cells(), w-col)
		// Blank cells, not absent ones: the background, the underline and
		// anything the terminal does with a selection all still come from
		// the cell. Only the ink is ours.
		win.New(col, row, span, 1).Fill(vaxis.Cell{
			Character: vaxis.Character{Grapheme: " ", Width: 1},
			Style:     style,
		})
		oc, or := win.Origin()
		a.placements = append(a.placements, placement{
			win: win.New(col, row, span, 1), run: run, cells: span, ink: a.ink(style),
			col: oc + col, row: or + row,
		})
		col += span
	}
	return col
}

func (a *App) printPlain(win vaxis.Window, col, row int, style vaxis.Style, s string) int {
	w, _ := win.Size()
	if col >= w || s == "" {
		return col
	}
	// The advance comes back from the print rather than being measured again:
	// measuring is the expensive half of drawing a string, and doing it twice
	// for every string on screen is the whole frame's budget.
	return col + win.New(col, row, w-col, 1).PrintTruncate(0, vaxis.Segment{Text: s, Style: style})
}

// blank paints n cells of ground, and rule paints n cells of one single-width
// glyph. Both are cheaper than printing the same thing as a string: a string
// has to be measured a grapheme at a time, and a run of cells that are all the
// same cell does not.
func (a *App) blank(win vaxis.Window, col, row, n int, style vaxis.Style) int {
	return a.rule(win, col, row, n, " ", style)
}

func (a *App) rule(win vaxis.Window, col, row, n int, glyph string, style vaxis.Style) int {
	if n <= 0 {
		return col
	}
	win.New(col, row, n, 1).Fill(vaxis.Cell{
		Character: vaxis.Character{Grapheme: glyph, Width: 1},
		Style:     style,
	})
	w, _ := win.Size()
	return minInt(col+n, w)
}

// placement is one rasterised word waiting for the end of the frame. Held
// rather than drawn on the spot because an image is not a cell: the panes
// paint over each other freely, and a graphic placed mid-frame would sit under
// whatever the next pane blanked.
// occlude drops the rasterised words a later pane covered. A run is a kitty
// image placed above the cell background, so a panel drawn over one hides its
// text and not the image: the phrase behind a palette keeps showing through
// unless the placement itself goes.
func (a *App) occlude(r layout.Rect) {
	kept := a.placements[:0]
	for _, p := range a.placements {
		if p.col+p.cells > r.Col && p.col < r.Col+r.Width &&
			p.row >= r.Row && p.row < r.Row+r.Height {
			continue
		}
		kept = append(kept, p)
	}
	a.placements = kept
}

type placement struct {
	win   vaxis.Window
	run   *textrun.Run
	cells int
	ink   color.NRGBA
	// col and row are absolute, so a selection can ask the run what the blank
	// cells it left behind actually say.
	col, row int
}

// imgKey identifies a rasterised image. The pixel cell is part of it because a
// font size change has to rescale rather than reuse.
type imgKey struct {
	key  string
	w, h int
}

// flushRuns places every rasterised word this frame drew, and drops the images
// no frame has asked for since the last sweep.
func (a *App) flushRuns() {
	cellW, cellH := a.shaper.CellSize()
	if cellW <= 0 || cellH <= 0 {
		return
	}
	for _, p := range a.placements {
		if cols, rows := p.win.Size(); cols < 1 || rows < 1 {
			continue
		}
		img, key := p.run.Image(p.cells, false, p.cells < p.run.Cells(), p.ink)
		if img == nil {
			continue
		}
		k := imgKey{key: key, w: cellW, h: cellH}
		a.seen[k] = true
		kimg, ok := a.images[k]
		if !ok {
			kimg = a.vx.NewKittyPixels(img)
			a.images[k] = kimg
		}
		kimg.Draw(p.win)
	}
	// Swept on probation rather than every frame: a word that scrolls off and
	// back would otherwise be rasterised and uploaded again each time it
	// crossed the edge.
	if len(a.images) < imageCacheCap {
		return
	}
	for k, img := range a.images {
		if a.seen[k] {
			continue
		}
		img.Destroy()
		delete(a.images, k)
	}
	clear(a.seen)
}

// imageCacheCap is how many rasterised words are held before the ones no
// recent frame has drawn are dropped. A screenful is a few dozen.
const imageCacheCap = 256

// dropRuns throws every rasterised image away. The cell moved, so every one of
// them is the wrong size.
func (a *App) dropRuns() {
	for k, img := range a.images {
		img.Destroy()
		delete(a.images, k)
	}
	clear(a.seen)
	a.placements = a.placements[:0]
}

// ink is the colour a rasterised word is painted in. The rasteriser paints
// pixels and cannot name an ansi index, so a palette that is not already rgb
// falls back to the theme's own two inks rather than to a round trip from the
// render loop, which is a deadlock.
func (a *App) ink(style vaxis.Style) color.NRGBA {
	c := style.Foreground
	if style.Attribute&vaxis.AttrReverse != 0 {
		c = style.Background
	}
	if p := c.Params(); len(p) == 3 {
		return color.NRGBA{R: p[0], G: p[1], B: p[2], A: 0xff}
	}
	v := a.theme.InkText
	if style.Attribute&vaxis.AttrDim != 0 {
		v = a.theme.InkMuted
	}
	return color.NRGBA{R: v[0], G: v[1], B: v[2], A: 0xff}
}
