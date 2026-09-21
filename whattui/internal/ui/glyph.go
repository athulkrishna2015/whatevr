package ui

import (
	"image"
	"image/color"
	"image/draw"
	"strconv"
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
	// Measured on what will actually be drawn. A control character that print
	// drops but width counted is a column that reserves a cell nothing fills,
	// which is how a list loses its right hand edge.
	s = safe(s)
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
	s = safe(s)
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
	// Nothing untrusted becomes a cell without coming through here; see
	// safetext.go for what goes and why.
	if s = safe(s); s == "" {
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
			win: win.New(col, row, span, 1), run: run, cells: span, span: span,
			ink: a.ink(style), col: oc + col, row: or + row,
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
	kept := a.occluded[:0]
	for _, p := range a.placements {
		if p.row < r.Row || p.row >= r.Row+r.Height ||
			p.col+p.span <= r.Col || p.col >= r.Col+r.Width {
			kept = append(kept, p)
			continue
		}
		// Only the covered columns go. A phrase is one image across many
		// cells, and dropping all of it because a panel landed on its tail
		// takes the half nobody covered with it.
		if p.col < r.Col {
			kept = append(kept, p.slice(0, r.Col-p.col))
		}
		if end := r.Col + r.Width; p.col+p.span > end {
			kept = append(kept, p.slice(end-p.col, p.col+p.span-end))
		}
	}
	a.occluded = a.placements[:0]
	a.placements = kept
}

type placement struct {
	win vaxis.Window
	run *textrun.Run
	// cells is the whole image the layout gave the run; from and span are the
	// part of it still visible after whatever was drawn on top.
	cells int
	from  int
	span  int
	ink   color.NRGBA
	// col and row are absolute, so a selection can ask the run what the blank
	// cells it left behind actually say.
	col, row int
}

// guardSpill blanks a wide grapheme that would paint its second half under a
// panel. A terminal draws a two cell character from the cell it starts in, so
// an emoji one column left of a panel reaches inside it and lands on the
// border, and filling the panel cannot undo that from the other side.
func (a *App) guardSpill(win vaxis.Window, r layout.Rect) {
	if r.Col < 1 {
		return
	}
	for row := r.Row; row < r.Row+r.Height; row++ {
		c := a.vx.Cell(r.Col-1, row)
		if c.Width < 2 {
			continue
		}
		c.Character = vaxis.Character{Grapheme: " ", Width: 1}
		win.New(r.Col-1, row, 1, 1).Fill(c)
	}
}

// slice is the same run showing only span cells of itself, starting from cells
// in. The window moves with it, so the image lands where the visible part of
// the phrase actually is.
func (p placement) slice(from, span int) placement {
	q := p
	q.win = p.win.New(from, 0, span, 1)
	q.from = p.from + from
	q.span = span
	q.col = p.col + from
	return q
}

// glyphKey is one letter drawn at one size in one colour, which is all a
// rasterised initial is.
type glyphKey struct {
	text string
	em   int
	ink  color.NRGBA
}

func (k glyphKey) String() string {
	return k.text + "\x00" + strconv.Itoa(k.em) + "\x00" +
		strconv.Itoa(int(k.ink.R)) + "," + strconv.Itoa(int(k.ink.G)) + "," + strconv.Itoa(int(k.ink.B))
}

// imgKey identifies a rasterised image. The pixel cell is part of it because a
// font size change has to rescale rather than reuse.
type imgKey struct {
	key  string
	w, h int
}

// flushImages places everything this frame rasterised, chrome first so a
// bubble is uploaded before the words inside it, and drops the images no
// frame has asked for since the last sweep.
func (a *App) flushImages() {
	a.flushSurfaces()
	a.flushRuns()
	a.sweepImages()
}

// flushRuns places every rasterised word this frame drew.
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
		if p.span < p.cells {
			key += "\x00" + strconv.Itoa(p.from) + ":" + strconv.Itoa(p.span)
		}
		k := imgKey{key: key, w: cellW, h: cellH}
		a.seen[k] = true
		kimg, ok := a.images[k]
		if !ok {
			if p.span < p.cells {
				img = cropCells(img, p.from, p.span, cellW)
			}
			kimg = a.vx.NewKittyPixels(img)
			a.images[k] = kimg
		}
		kimg.Draw(p.win)
	}
}

// sweepImages drops what no recent frame drew. On probation rather than every
// frame: a word that scrolls off and back would otherwise be rasterised and
// uploaded again each time it crossed the edge.
func (a *App) sweepImages() {
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

// dropImages throws every rasterised image away. The cell moved, so every one
// of them is the wrong size.
func (a *App) dropImages() {
	for k, img := range a.images {
		img.Destroy()
		delete(a.images, k)
	}
	clear(a.seen)
	clear(a.glyphs)
	a.placements = a.placements[:0]
	a.surfaces = a.surfaces[:0]
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

// cropCells copies the columns of a rasterised phrase that are still visible.
// The pixels are copied rather than sub-imaged because what reads them next
// wants a buffer that starts at its own first pixel.
func cropCells(img *image.NRGBA, from, span, cellW int) *image.NRGBA {
	x0 := from * cellW
	x1 := minInt(x0+span*cellW, img.Bounds().Dx())
	if x0 >= x1 {
		return img
	}
	out := image.NewNRGBA(image.Rect(0, 0, x1-x0, img.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), img, image.Pt(img.Bounds().Min.X+x0, img.Bounds().Min.Y), draw.Src)
	return out
}
