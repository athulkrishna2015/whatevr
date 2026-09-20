package ui

import (
	"fmt"
	"testing"

	"whattui/internal/proto"
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
