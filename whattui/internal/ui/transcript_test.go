package ui

import (
	"fmt"
	"testing"
	"time"

	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/textrun"
)

// A message taller than the pane has to be readable a row at a time. Scrolling
// by message meant one notch of the wheel took the whole thing off screen.
func TestScrollingMovesOneRowAtATimeThroughATallMessage(t *testing.T) {
	a := benchApp(80, 24, 4, 0)
	c := a.conversation
	c.msgs.Reset()
	long := ""
	for i := 0; i < 40; i++ {
		long += fmt.Sprintf("line %d of a message that is taller than the pane it is in. ", i)
	}
	c.msgs.Upsert("00000000000000000000", mustJSON(proto.MessageRow{
		ID: "tall", Kind: "text", Direction: "incoming", Text: long,
		Sender: proto.Sender{ID: "x", Name: "someone"},
	}))
	c.msgs.Ready(true, true)

	a.paint()
	if c.contentRows < 30 {
		t.Fatalf("content is %d rows, want a message taller than the pane", c.contentRows)
	}

	before := a.transcriptRowsText()
	a.scrollTranscript(1)
	a.paint()
	if got := c.scroll; got != 1 {
		t.Fatalf("scroll = %d after one row, want 1", got)
	}
	if after := a.transcriptRowsText(); after == before {
		t.Fatal("one row of scroll changed nothing on screen")
	}

	// And it stops at the top rather than running off into nothing.
	for i := 0; i < 200; i++ {
		a.scrollTranscript(1)
	}
	if got, want := c.scroll, c.maxScroll(a.transcriptPage()); got != want {
		t.Fatalf("scroll = %d at the top, want %d", got, want)
	}
}

// transcriptRowsText is what the transcript pane currently says, for tests
// that care that something moved rather than what.
func (a *App) transcriptRowsText() string {
	l := a.layout()
	out := ""
	for row := l.Transcript.Row; row < l.Transcript.Row+l.Transcript.Height; row++ {
		for col := l.Transcript.Col; col < l.Transcript.Col+l.Transcript.Width; col++ {
			out += a.vx.Cell(col, row).Grapheme
		}
		out += "\n"
	}
	return out
}

// The layout cache exists to not re-wrap what has not changed, and it must not
// go stale when the daemon does change something.
func TestAChangedMessageIsLaidOutAgain(t *testing.T) {
	a := benchApp(80, 24, 4, 0)
	c := a.conversation
	c.msgs.Reset()
	c.msgs.Upsert("00000000000000000001", mustJSON(proto.MessageRow{
		ID: "m", Kind: "text", Direction: "incoming", Text: "one line",
		Sender: proto.Sender{ID: "x", Name: "someone"},
	}))
	c.msgs.Ready(true, true)
	a.paint()
	first := c.cache["m"].height

	c.msgs.Upsert("00000000000000000001", mustJSON(proto.MessageRow{
		ID: "m", Kind: "text", Direction: "incoming",
		Text:   "one line\ntwo lines\nthree lines\nfour lines",
		Sender: proto.Sender{ID: "x", Name: "someone"},
	}))
	a.paint()
	if got := c.cache["m"].height; got <= first {
		t.Fatalf("height after the edit = %d, was %d", got, first)
	}
}

func TestResizingRelaysTheWholeTranscript(t *testing.T) {
	a := benchApp(120, 40, 4, 20)
	a.paint()
	wide := a.conversation.contentRows

	a.vx.Resize(vaxisResize(50, 40))
	a.paint()
	if narrow := a.conversation.contentRows; narrow <= wide {
		t.Fatalf("content is %d rows at 50 columns and %d at 120", narrow, wide)
	}
}

// A message taller than the pane hangs off both ends of it. Text off the end
// is clipped by the terminal, but a rasterised word is an image with a
// position, and one placed past the last row is clamped onto the last row
// rather than dropped: the phrase piles up at the bottom of the transcript
// instead of scrolling out of it.
func TestNothingIsPlacedOutsideTheTranscript(t *testing.T) {
	a := benchApp(100, 26, 4, 0)
	a.caps = term.Caps{Tier: term.TierShm, RGB: true}
	a.shaper = textrun.New(textrun.Options{})
	a.shaper.SetCellSize(10, 21)
	deadline := time.Now().Add(30 * time.Second)
	for !a.shaper.Begin() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.shaper.Begin() {
		t.Skip("no usable font index on this machine")
	}

	c := a.conversation
	c.msgs.Reset()
	body := ""
	for i := 0; i < 60; i++ {
		body += "नमस्ते सर, Khatabook के इंस्टेंट लोन के साथ अपने बिजनेस के सपनों को हकीकत बनाएँ। "
	}
	c.msgs.Upsert("00000000000000000000", mustJSON(proto.MessageRow{
		ID: "tall", Kind: "text", Direction: "incoming", Text: body,
		Sender: proto.Sender{ID: "x", Name: "Khatabook"},
	}))
	c.msgs.Ready(true, true)

	pane := a.layout().Transcript
	a.paint()
	if c.maxScroll(pane.Height) < 8 {
		t.Fatalf("content is %d rows in a pane of %d, want something to scroll",
			c.contentRows, pane.Height)
	}
	for scroll := 0; scroll <= c.maxScroll(pane.Height)+4; scroll++ {
		a.paint()
		for _, p := range a.placements {
			if p.row < pane.Row || p.row >= pane.Row+pane.Height {
				t.Fatalf("scroll %d placed a run on row %d, outside rows %d..%d",
					scroll, p.row, pane.Row, pane.Row+pane.Height-1)
			}
			if p.col < pane.Col || p.col+p.cells > pane.Col+pane.Width {
				t.Fatalf("scroll %d placed a run at columns %d..%d, outside %d..%d",
					scroll, p.col, p.col+p.cells, pane.Col, pane.Col+pane.Width)
			}
		}
		a.scrollTranscript(1)
	}
}

// A run is an image over the cell background, so a panel drawn on top of one
// hides the text under it and not the picture. Anything that covers the
// transcript has to take the placements with it.
func TestAModalTakesTheRunsUnderItWithIt(t *testing.T) {
	a := benchApp(100, 26, 4, 0)
	a.caps = term.Caps{Tier: term.TierShm, RGB: true}
	a.shaper = textrun.New(textrun.Options{})
	a.shaper.SetCellSize(10, 21)
	deadline := time.Now().Add(30 * time.Second)
	for !a.shaper.Begin() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.shaper.Begin() {
		t.Skip("no usable font index on this machine")
	}

	c := a.conversation
	c.msgs.Reset()
	for i := 0; i < 12; i++ {
		c.msgs.Upsert(fmt.Sprintf("%020d", i), mustJSON(proto.MessageRow{
			ID: fmt.Sprintf("m%d", i), Kind: "text", Direction: "incoming",
			Text:   "नमस्ते सर, आपके बिजनेस के सपनों को हकीकत बनाएँ।",
			Sender: proto.Sender{ID: "x", Name: "Khatabook"},
		}))
	}
	c.msgs.Ready(true, true)

	a.paint()
	if len(a.placements) == 0 {
		t.Fatal("nothing was rasterised, so there is nothing to cover")
	}

	a.openModal(modalPalette)
	a.paint()
	rect := a.modal.rect
	if rect.Width == 0 || rect.Height == 0 {
		t.Fatal("the palette claimed no room")
	}
	for _, p := range a.placements {
		if p.col+p.cells > rect.Col && p.col < rect.Col+rect.Width &&
			p.row >= rect.Row && p.row < rect.Row+rect.Height {
			t.Fatalf("a run at %d,%d (%d cells) shows through the palette at %+v",
				p.col, p.row, p.cells, rect)
		}
	}
}
