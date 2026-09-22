package ui

import (
	"strings"

	"go.rockorager.dev/vaxis"

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
	// mark is what happened to the message after it was sent: a star, an edit.
	// It goes after the words rather than into the gutter, because the gutter
	// is the width of the time and the ticks and every glyph put in there is a
	// glyph pressed against one of them.
	mark string
	// markCol is where the mark sits on the last row of the words, and markOwn
	// says it did not fit there and has a row to itself.
	markCol int
	markOwn bool
	// muted draws the words quietly. A message nobody can read any more is not
	// a message anybody said.
	muted bool
	width int
}

// rows is how tall the block is. The words and nothing else: the time is in
// the gutter and the shape is the run's.
func (b block) rows() int {
	n := len(b.body) * b.scale
	if b.quote != "" {
		n++
	}
	if b.mark != "" && b.markOwn {
		n++
	}
	return n
}

// layoutBlock wraps a message into a column no wider than max.
func (a *App) layoutBlock(quote, body, stamp, mark string, max int) block {
	if max < 8 {
		max = 8
	}
	b := block{stamp: stamp, mark: mark, scale: 1}
	if quote != "" {
		b.quote = "│ " + quote
	}
	b.body = a.wrapSpans(body, max, true)

	for _, l := range b.body {
		b.width = maxInt(b.width, a.lineWidth(l))
	}
	b.width = minInt(maxInt(b.width, a.width(b.quote)), max)
	b.quote = a.clip(b.quote, b.width)
	a.placeMark(&b, max)
	return b
}

// placeMark puts the mark a space after the last word, and on a row of its own
// when there is no room for it there. A mark that pushed the column wider would
// make a starred message a different shape from the same message unstarred.
func (a *App) placeMark(b *block, max int) {
	if b.mark == "" {
		return
	}
	last := 0
	if n := len(b.body); n > 0 {
		last = a.lineWidth(b.body[n-1])
	}
	if at := last + 1; at+a.width(b.mark) <= max {
		b.markCol = at
		b.width = minInt(maxInt(b.width, at+a.width(b.mark)), max)
		return
	}
	b.markOwn = true
	b.markCol = 0
	b.width = minInt(maxInt(b.width, a.width(b.mark)), max)
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
		if b.mark != "" {
			a.print(pane, col, row, faint, b.mark)
			row++
		}
		return row
	}

	a.noteBlock(pane, col, row, width, len(b.body))
	for i, l := range b.body {
		a.printLine(pane, col, row, text, l)
		if b.mark != "" && !b.markOwn && i == len(b.body)-1 {
			a.print(pane, col+b.markCol, row, faint, b.mark)
		}
		row++
	}
	if b.mark != "" && b.markOwn {
		a.print(pane, col, row, faint, b.mark)
		row++
	}
	return row
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
