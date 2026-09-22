package ui

import (
	"strings"

	"go.rockorager.dev/vaxis"

	"whattui/internal/paint"
	"whattui/internal/term"
)

// box is the set of glyphs a container is drawn with. Only the glyphs change
// between tiers; the cells they occupy do not, which is what keeps a layout
// identical on a terminal that can draw and one that cannot.
type box struct {
	topLeft, topRight, bottomLeft, bottomRight string
	horizontal, vertical                       string
}

var (
	roundedBox = box{"╭", "╮", "╰", "╯", "─", "│"}
	asciiBox   = box{"+", "+", "+", "+", "-", "|"}
)

func boxFor(caps term.Caps) box {
	if caps.Tier <= term.TierPlain {
		return asciiBox
	}
	return roundedBox
}

// Widths here are all cells, measured by the terminal's own rules rather than
// counted in runes. A chat full of emoji is the normal case, not the edge
// case, and an emoji is not one cell wide: counting runes is what puts a
// column one short of where it belongs.

// block is one message, laid out but not yet drawn: its wrapped lines and the
// time that goes at the end of them.
//
// A message is not a shape of its own. The shape is the run it belongs to,
// which is why nothing here knows which side of the transcript it is on or how
// wide the column it lands in turns out to be.
type block struct {
	// scale draws the body at this many cells per glyph. Only ever more than
	// one for a message that is nothing but emoji, where the size is the
	// meaning rather than decoration.
	scale int
	quote string
	body  []line
	// stamp is the time and the delivery state. It stands in the gutter
	// beside the message rather than at the end of its last line: a column of
	// times down the edge of the transcript is a column you can read, and a
	// time tucked after the words is a time that lands somewhere new on every
	// message.
	stamp string
	// muted draws the words quietly. A message nobody can read any more is not
	// a message anybody said.
	muted bool
	// reacts is what people put on the message, on a row of its own under the
	// words. Under rather than after them, because a reaction is about the
	// whole message: a strip that follows the last word moves every time the
	// message is rewrapped, and lands in the middle of the column on a short
	// last line.
	reacts []pill
	width  int
}

// rows is how tall the block is. The words and nothing else: the time is in
// the gutter, the shape is the run's, and what is true about the message is
// drawn in space the message was never using.
func (b block) rows() int {
	n := len(b.body) * b.scale
	if b.quote != "" {
		n++
	}
	if len(b.reacts) > 0 {
		n++
	}
	return n
}

// layoutBlock wraps a message into a column no wider than max.
func (a *App) layoutBlock(quote, body, stamp string, max int) block {
	if max < 8 {
		max = 8
	}
	b := block{stamp: stamp, scale: 1}
	if quote != "" {
		b.quote = "│ " + quote
	}
	b.body = a.wrapSpans(body, max, true)

	for _, l := range b.body {
		b.width = maxInt(b.width, a.lineWidth(l))
	}
	b.width = minInt(maxInt(b.width, a.width(b.quote)), max)
	b.quote = a.clip(b.quote, b.width)
	return b
}

// drawBlock paints one message down a column, and answers the row after it.
func (a *App) drawBlock(pane vaxis.Window, b block, col, row, width int, ground vaxis.Color) int {
	text := vaxis.Style{Foreground: a.theme.Text, Background: ground}
	if b.muted {
		text = vaxis.Style{
			Foreground: a.theme.TextMuted, Background: ground, Attribute: vaxis.AttrItalic,
		}
	}
	faint := vaxis.Style{Foreground: a.theme.TextFaint, Background: ground}

	if b.quote != "" {
		a.print(pane, col, row, faint, b.quote)
		row++
	}

	if b.scale > 1 {
		// The block is claimed whatever the terminal can do: without the
		// scale key vaxis paints the reserved cells and draws the glyph small
		// in the top left, so nothing moves.
		for _, l := range b.body {
			pane.New(col, row, width, b.scale).PrintScaled(0, vaxis.Segment{
				Text:  lineText(l),
				Style: text,
				Size:  vaxis.Scaled(b.scale, 0),
			})
			row += b.scale
		}
		return a.drawPills(pane, b, col, row, ground)
	}

	a.noteBlock(pane, col, row, width, len(b.body))
	for _, l := range b.body {
		a.printLine(pane, col, row, text, l)
		row++
	}
	return a.drawPills(pane, b, col, row, ground)
}

// drawPills writes the reaction strip under a message, and answers the row
// after it.
//
// Each reaction gets a shape of its own, because without one a strip of emoji
// under a message is a line of the message: the same ground, the same column,
// nothing saying where the words stop and what people thought of them starts. A
// chip is the shape every other client uses for exactly that reason.
//
// The one this account put there wears the material an outgoing message wears
// and is underlined besides. The underline is not decoration: an emoji is drawn
// by the font in the font's own colours, so a foreground and a weight say
// nothing at all about a 🔥, and a rule under the cells is the one mark a
// terminal can put on a glyph it does not get to colour.
func (a *App) drawPills(pane vaxis.Window, b block, col, row int, ground vaxis.Color) int {
	if len(b.reacts) == 0 {
		return row
	}
	for _, p := range b.reacts {
		width := a.cells(p)
		chip := a.drawChip(pane, col, row, width, p.mine, ground)
		style := vaxis.Style{Foreground: a.theme.TextMuted, Background: chip}
		if p.mine {
			style = vaxis.Style{Foreground: a.theme.Text, Background: chip, Attribute: vaxis.AttrBold}
		}
		a.print(pane, col+pillPad, row, style, p.text)
		col += width + pillGap
	}
	return row + 1
}

// drawChip is the rounded chrome behind one reaction, and answers the cell
// colour its glyphs stand on.
//
// Where there are pixels the shape is drawn and the cells keep the page's own
// ground, so what falls outside the curve is the page rather than a square
// corner. Where there are not, the same cells are filled flat: the chip loses
// its corners and keeps its size, its colour and its place, which is the whole
// of the tier rule.
func (a *App) drawChip(pane vaxis.Window, col, row, width int, mine bool, ground vaxis.Color) vaxis.Color {
	fill, edge, cell := a.theme.PaintChip, a.theme.PaintChipEdge, a.theme.Chip
	if mine {
		fill, edge, cell = a.theme.PaintChipMine, a.theme.PaintChipMineEdge, a.theme.ChipMine
	}
	if a.painted() {
		a.blank(pane, col, row, width, vaxis.Style{Background: ground})
		cw, _ := a.cellPix()
		a.paintRect(pane, col, row, width, 1, func(pw, ph int) paint.Spec {
			// Air enough to read as a chip and no more: most of a column back
			// at the sides, so the glyph is not marooned in a box, and a
			// sliver top and bottom so the shape keeps off the words above it
			// and the message below. More off the top than the bottom, because
			// the glyph it is drawn around stands on a baseline rather than in
			// the middle of its cell.
			// An emoji is drawn down to the bottom of its cell, so a chip with
			// air under it is a chip the glyph hangs out of. It takes its air
			// off the top instead, where the row above holds letters rather
			// than pictures and their descenders stop well short of it, and
			// stands on the bottom of its own row, where the next row's letters
			// start well below.
			x, top, bottom := maxInt(cw*3/4, 3), maxInt(ph/7, 2), 0
			// A corner taken off, not a side rounded away. The radius every
			// other shape here uses is most of this one's height, and a shape
			// as round as it is tall is a lozenge: a quarter of the height is
			// the same curve read at the size a chip actually is.
			return paint.Chip{
				W: pw, H: ph, InsetX: x, InsetTop: top, InsetBottom: bottom,
				Fill: fill, Edge: edge,
				Radius: maxInt(minInt(a.bubbleRadius(), (ph-top-bottom)/4), 2),
			}
		})
		return ground
	}
	a.blank(pane, col, row, width, vaxis.Style{Background: cell})
	return cell
}

func (a *App) pad(s string, width int) string {
	if n := width - a.width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return a.clip(s, width)
}

func (a *App) padLeft(s string, width int) string {
	if n := width - a.width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return a.clip(s, width)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
