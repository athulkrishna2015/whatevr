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
// Quieter than the words on purpose: a reaction is somebody agreeing, and a
// screenful of them at full strength would read louder than the conversation
// they are about.
//
// The one this account put there is underlined, and the underline is doing the
// work rather than helping. An emoji is drawn by the font in the font's own
// colours: a foreground the accent and a weight bold say nothing at all about a
// 🔥, and the count beside it is the only cell either of them reaches. A rule
// under the cells is the one mark a terminal can put on a glyph it does not get
// to colour.
func (a *App) drawPills(pane vaxis.Window, b block, col, row int, ground vaxis.Color) int {
	if len(b.reacts) == 0 {
		return row
	}
	for i, p := range b.reacts {
		if i > 0 {
			col = a.blank(pane, col, row, pillGap, vaxis.Style{Background: ground})
		}
		style := vaxis.Style{Foreground: a.theme.TextMuted, Background: ground}
		if p.mine {
			style = vaxis.Style{
				Foreground: a.theme.Accent, Background: ground, Attribute: vaxis.AttrBold,
				UnderlineStyle: vaxis.UnderlineSingle, UnderlineColor: a.theme.Accent,
			}
		}
		col = a.print(pane, col, row, style, p.text)
	}
	return row + 1
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
