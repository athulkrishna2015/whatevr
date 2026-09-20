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

// bubblePadX is the space between a bubble's border and its words, per side.
// A container reports its content plus its own padding, so this is counted
// into the width rather than eaten out of it.
const bubblePadX = 1

// Widths here are all cells, measured by the terminal's own rules rather than
// counted in runes. A chat full of emoji is the normal case, not the edge
// case, and an emoji is not one cell wide: counting runes is what puts a
// border one column short of where it belongs.

// bubble is one message, laid out but not yet drawn.
type bubble struct {
	// scale draws the body at this many cells per glyph. Only ever more than
	// one for a message that is nothing but emoji, where the size is the
	// meaning rather than decoration.
	scale       int
	header      string
	headerStyle vaxis.Style
	quote       string
	body        []line
	footer      string
	outgoing    bool
	// inner is the content width, not counting padding or border.
	inner int
}

// Width is what the bubble occupies on screen: its content, plus its own
// padding, plus its border.
func (b bubble) Width() int { return b.inner + 2*bubblePadX + 2 }

// Height is the border, the optional header and quote, the body, and the
// footer when it did not fit beside the last line of text.
func (a *App) bubbleHeight(b bubble) int {
	n := 2 + len(b.body)*b.scale
	if b.header != "" {
		n++
	}
	if b.quote != "" {
		n++
	}
	if !a.footerFitsInline(b) {
		n++
	}
	return n
}

// footerFitsInline says whether the time and ticks can sit at the end of the
// last line of text rather than claiming a row of their own. One space of gap
// is the minimum that still reads as two things.
func (a *App) footerFitsInline(b bubble) bool {
	if len(b.body) == 0 || b.footer == "" {
		return false
	}
	last := a.lineWidth(b.body[len(b.body)-1])
	return last+1+a.width(b.footer) <= b.inner
}

// layoutBubble wraps a message into a bubble no wider than max. The width is
// the content's own, so a short message gets a short bubble rather than being
// stretched to a column.
func (a *App) layoutBubble(header, quote, body, footer string, max int, headerStyle vaxis.Style, outgoing bool) bubble {
	// The border and padding come out of the budget before the words do.
	innerMax := max - 2*bubblePadX - 2
	if innerMax < 8 {
		innerMax = 8
	}

	b := bubble{header: header, headerStyle: headerStyle, footer: footer, outgoing: outgoing, scale: 1}
	if quote != "" {
		b.quote = "│ " + quote
	}
	b.body = a.wrapSpans(body, innerMax, true)

	for _, l := range b.body {
		b.inner = maxInt(b.inner, a.lineWidth(l))
	}
	b.inner = maxInt(b.inner, a.width(b.header))
	b.inner = maxInt(b.inner, a.width(b.quote))
	b.inner = minInt(b.inner, innerMax)
	// A footer that cannot tuck in beside the last line gets a row of its
	// own, and a row of its own has to be wide enough to hold it. Checked
	// after the content width is known, because whether it tucks in depends
	// on that width.
	if !a.footerFitsInline(b) {
		b.inner = minInt(maxInt(b.inner, a.width(b.footer)), innerMax)
	}

	// Truncate anything that still overruns, so the border never breaks.
	b.header = a.clip(b.header, b.inner)
	b.quote = a.clip(b.quote, b.inner)
	return b
}

// draw paints the bubble with its top-left at col,row.
func (a *App) drawBubble(pane vaxis.Window, b bubble, col, row int) {
	bx := boxFor(a.caps)
	inner := b.inner
	total := b.Width()

	fillColor := a.theme.BubbleIn
	if b.outgoing {
		fillColor = a.theme.BubbleOut
	}
	// A rasterised bubble carries its own ground, and the cells inside it keep
	// the pane's. Filling them with the bubble colour as well would tint the
	// image a second time, over a ground it was already mixed against.
	painted := a.painted()
	if painted {
		fillColor = a.theme.Background
		a.paintBubble(pane, b, col, row, total)
	}
	border := vaxis.Style{Foreground: a.theme.Border, Background: fillColor}
	text := vaxis.Style{Foreground: a.theme.Text, Background: fillColor}
	faint := vaxis.Style{Foreground: a.theme.TextFaint, Background: fillColor}

	// The border is a row and a column of cells whatever the tier. Painted,
	// the image draws the edge and the cells are only kept clean; typed, the
	// glyphs are the edge. Identical geometry either way.
	vertical, horizontal := bx.vertical, bx.horizontal
	if painted {
		vertical, horizontal = " ", " "
	}
	rule := func(left, right string) string {
		if painted {
			return strings.Repeat(" ", total)
		}
		return left + strings.Repeat(horizontal, total-2) + right
	}
	a.print(pane, col, row, border, rule(bx.topLeft, bx.topRight))
	r := row + 1

	line := func(s string, style vaxis.Style) {
		a.print(pane, col, r, border, vertical)
		a.print(pane, col+1, r, style, " "+a.pad(s, inner)+" ")
		a.print(pane, col+total-1, r, border, vertical)
		r++
	}

	if b.header != "" {
		hs := b.headerStyle
		hs.Background = fillColor
		line(b.header, hs)
	}
	if b.quote != "" {
		line(b.quote, faint)
	}

	if b.scale > 1 {
		// The body is drawn as multicell characters. The block is claimed
		// whatever the terminal can do: without the scale key vaxis paints
		// the reserved cells and draws the glyph small in the top left, so
		// the bubble is the same size either way and nothing shifts.
		for _, l := range b.body {
			a.print(pane, col, r, border, vertical)
			a.print(pane, col+1, r, text, " ")
			slot := pane.New(col+bubblePadX+1, r, b.inner, b.scale)
			slot.PrintScaled(0, vaxis.Segment{
				Text:  lineText(l),
				Style: text,
				Size:  vaxis.Scaled(b.scale, 0),
			})
			for pad := 0; pad < b.scale; pad++ {
				a.print(pane, col, r+pad, border, vertical)
				a.print(pane, col+total-1, r+pad, border, vertical)
			}
			r += b.scale
		}
		if b.footer != "" {
			line(a.padLeft(b.footer, inner), faint)
		}
		a.print(pane, col, r, border, rule(bx.bottomLeft, bx.bottomRight))
		return
	}

	a.noteBlock(pane, col+bubblePadX+1, r, inner, len(b.body))
	for i, l := range b.body {
		a.print(pane, col, r, border, vertical)
		c := a.print(pane, col+1, r, text, " ")
		c = a.printLine(pane, c, r, text, l)
		if i == len(b.body)-1 && a.footerFitsInline(b) {
			// The time tucks in at the end of the last line, which is where
			// a chat app puts it and where it costs no row.
			gap := inner - a.lineWidth(l) - a.width(b.footer)
			c = a.print(pane, c, r, text, strings.Repeat(" ", gap))
			a.print(pane, c, r, faint, b.footer+" ")
		} else {
			a.print(pane, c, r, text, strings.Repeat(" ", maxInt(inner-a.lineWidth(l), 0))+" ")
		}
		a.print(pane, col+total-1, r, border, vertical)
		r++
	}

	if !a.footerFitsInline(b) && b.footer != "" {
		line(a.padLeft(b.footer, inner), faint)
	}

	a.print(pane, col, r, border, rule(bx.bottomLeft, bx.bottomRight))
}

// paintBubble reserves the bubble's own cells for its chrome. The square
// corner is on the side the message came from, which is the tail a cell grid
// is too coarse to draw.
func (a *App) paintBubble(pane vaxis.Window, b bubble, col, row, total int) {
	fill, edge := a.theme.PaintIn, a.theme.PaintInEdge
	anchor := paint.Left
	if b.outgoing {
		fill, edge = a.theme.PaintOut, a.theme.PaintOutEdge
		anchor = paint.Right
	}
	radius := a.bubbleRadius()
	a.paintRect(pane, col, row, total, a.bubbleHeight(b), func(pw, ph int) paint.Spec {
		return paint.Bubble{W: pw, H: ph, Fill: fill, Edge: edge, Radius: radius, Anchor: anchor}
	})
}

// bubbleRadius is how round a corner is, in pixels. Two fifths of a cell:
// enough to read as a curve at twenty pixels a row, not so much that a one
// line bubble turns into a pill.
func (a *App) bubbleRadius() int {
	_, ch := a.cellPix()
	if r := ch * 2 / 5; r > 3 {
		return r
	}
	return 3
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
