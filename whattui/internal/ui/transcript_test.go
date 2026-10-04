package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"

	"whattui/internal/term"
	"whattui/internal/textrun"
)

// A message taller than the pane has to be readable a row at a time. Scrolling
// by message meant one notch of the wheel took the whole thing off screen.
func TestScrollingMovesOneRowAtATimeThroughATallMessage(t *testing.T) {
	a := stubApp(80, 24, 4, 0)
	c := a.conversation
	reset(c.msgs)
	long := ""
	for i := 0; i < 40; i++ {
		long += fmt.Sprintf("line %d of a message that is taller than the pane it is in. ", i)
	}
	putMsg(c.msgs, "00000000000000000000", (v2.MessageRow_builder{
		Id: "tall", TextBody: &v2.Text{}, Text: long,
		Sender: person("x", "someone"),
	}.Build()))
	ready(c.msgs, true)

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
	a := stubApp(80, 24, 4, 0)
	c := a.conversation
	reset(c.msgs)
	putMsg(c.msgs, "00000000000000000001", (v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: "one line",
		Sender: person("x", "someone"),
	}.Build()))
	ready(c.msgs, true)
	a.paint()
	first := c.runs[0].height

	putMsg(c.msgs, "00000000000000000001", (v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: "one line\ntwo lines\nthree lines\nfour lines",
		Sender: person("x", "someone"),
	}.Build()))
	a.paint()
	if got := c.runs[0].height; got <= first {
		t.Fatalf("height after the edit = %d, was %d", got, first)
	}
}

func TestResizingRelaysTheWholeTranscript(t *testing.T) {
	a := stubApp(120, 40, 4, 20)
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
	a := stubApp(100, 26, 4, 0)
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
	reset(c.msgs)
	body := ""
	for i := 0; i < 60; i++ {
		body += "नमस्ते सर, Khatabook के इंस्टेंट लोन के साथ अपने बिजनेस के सपनों को हकीकत बनाएँ। "
	}
	putMsg(c.msgs, "00000000000000000000", (v2.MessageRow_builder{
		Id: "tall", TextBody: &v2.Text{}, Text: body,
		Sender: person("x", "Khatabook"),
	}.Build()))
	ready(c.msgs, true)

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
	// Wide enough that the panel lands inside the transcript with message
	// text running out past both of its edges.
	a := stubApp(140, 26, 4, 0)
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
	reset(c.msgs)
	for i := 0; i < 30; i++ {
		putMsg(c.msgs, fmt.Sprintf("%020d", i), v2.MessageRow_builder{
			Id: fmt.Sprintf("m%d", i), TextBody: &v2.Text{}, Text: "नमस्ते सर, आपके बिजनेस के सपनों को हकीकत बनाएँ और आगे बढ़ें, " +
				"कृपया अपनी पूरी जानकारी एक बार ध्यान से देख लें, धन्यवाद।",
			Sender: person("x", "Khatabook"),
		}.Build())
	}
	ready(c.msgs, true)

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
	crossed := false
	for _, p := range a.placements {
		if p.col+p.span > rect.Col && p.col < rect.Col+rect.Width &&
			p.row >= rect.Row && p.row < rect.Row+rect.Height {
			t.Fatalf("a run at %d,%d (%d cells) shows through the palette at %+v",
				p.col, p.row, p.span, rect)
		}
		// A phrase that only had its tail covered keeps the rest: it is one
		// image over many cells, and the half nobody covered is still text
		// somebody is reading.
		if p.span > 0 && p.span < p.cells {
			crossed = true
		}
	}
	if !crossed {
		t.Error("no phrase was trimmed at the palette's edge, so the split was never exercised")
	}
}

// oneMessage is a transcript holding exactly one row, for the tests about how
// one message is drawn.
func oneMessage(t *testing.T, a *App, m *v2.MessageRow) entry {
	t.Helper()
	c := a.conversation
	reset(c.msgs)
	m.SetSender(person("x", "someone"))
	putMsg(c.msgs, "00000000000000000001", m)
	ready(c.msgs, true)
	a.paint()
	return c.cache[m.GetId()]
}

// cellSaying is the style of the first cell on a row holding a grapheme.
func (a *App) cellSaying(row int, grapheme string) (vaxis.Style, bool) {
	cols, _ := a.vx.Window().Size()
	for col := 0; col < cols; col++ {
		if c := a.vx.Cell(col, row); c.Grapheme == grapheme {
			return c.Style, true
		}
	}
	return vaxis.Style{}, false
}

// A message nobody can read any more is not something anybody said, and it has
// to read that way rather than sitting there in full strength text.
func TestADeletedMessageIsDrawnQuietly(t *testing.T) {
	a := stubApp(100, 26, 4, 0)
	e := oneMessage(t, a, v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Revoked: true, Edited: true, Starred: true,
	}.Build())

	if !e.block.muted {
		t.Error("a deleted message is drawn like any other")
	}
	// And what was done to it before it went is no longer news.
	if e.starred || e.edited {
		t.Error("a deleted message still carries its flags")
	}

	style, ok := a.cellSaying(a.messages[0].at.Row, "T")
	if !ok {
		t.Fatal("the deleted placeholder is not on the frame")
	}
	if style.Foreground != a.theme.TextMuted {
		t.Errorf("the placeholder is %v, want the muted ink %v", style.Foreground, a.theme.TextMuted)
	}
	if style.Attribute&vaxis.AttrItalic == 0 {
		t.Error("the placeholder is not italic, so it reads as something somebody wrote")
	}
}

// A flag says something about a message without being part of it. It must cost
// the message nothing: not a column of its width, not a row of its height, and
// not a cell of the rail, which is exactly as wide as a time and its ticks.
func TestFlagsCostAMessageNothing(t *testing.T) {
	a := stubApp(100, 26, 4, 0)
	plain := oneMessage(t, a, v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: "short",
	}.Build())
	flagged := oneMessage(t, a, v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: "short", Edited: true, Starred: true,
	}.Build())

	if !flagged.starred || !flagged.edited {
		t.Fatal("the flags did not survive the layout")
	}
	if flagged.block.width != plain.block.width || flagged.block.rows() != plain.block.rows() {
		t.Errorf("a flagged message is %dx%d and the same message plain is %dx%d",
			flagged.block.width, flagged.block.rows(), plain.block.width, plain.block.rows())
	}
	if flagged.block.stamp != plain.block.stamp {
		t.Errorf("the rail says %q when flagged and %q when not", flagged.block.stamp, plain.block.stamp)
	}
	if got := a.width(flagged.block.stamp); got > runGutterIn {
		t.Errorf("the rail is %d cells wide, want no more than %d", got, runGutterIn)
	}
}

// The star stands in the one column between the time and the rule, which every
// message has and none of them uses, and it runs the height of the message it
// marks.
func TestTheStarMarksTheColumnBesideTheRule(t *testing.T) {
	a := stubApp(100, 26, 4, 0)
	room := a.runRoom(a.layout().Transcript.Width)
	e := oneMessage(t, a, v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: strings.Repeat("a", room*2), Starred: true,
	}.Build())
	if got := e.block.rows(); got < 2 {
		t.Fatalf("this message is %d rows, want one worth a ribbon", got)
	}

	// Incoming, so the rule is on the left and the flag column is the cell
	// before it.
	at := a.messages[0].at
	col := at.Col + runLead + runGutterIn
	head := a.vx.Cell(col, at.Row)
	if head.Grapheme != "▏" {
		t.Fatalf("the column beside the rule says %q on the first row, want the mark", head.Grapheme)
	}
	if head.Style.Foreground != a.theme.Warning {
		t.Errorf("the mark is %v, want the amber %v", head.Style.Foreground, a.theme.Warning)
	}
	// The ribbon under it, at the tier that has no pixels to draw one with.
	if tail := a.vx.Cell(col, at.Row+1).Grapheme; tail != "▏" {
		t.Errorf("the row under the star says %q, want the ribbon", tail)
	}
	// And the words are where they would be without it.
	plain := oneMessage(t, a, v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: strings.Repeat("a", room*2),
	}.Build())
	if plain.block.width != e.block.width {
		t.Error("the star took a column from the words")
	}
}

// An edit is the claim that the words are not the words that were said at that
// time, so the mark goes on the time. Every terminal can underline.
func TestAnEditMarksTheTime(t *testing.T) {
	a := stubApp(100, 26, 4, 0)
	e := oneMessage(t, a, v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: "short", Edited: true,
	}.Build())

	digit := string([]rune(e.block.stamp)[0])
	style, ok := a.cellSaying(a.messages[0].at.Row, digit)
	if !ok {
		t.Fatalf("no time on the frame to mark, wanted %q", e.block.stamp)
	}
	if style.UnderlineStyle != vaxis.UnderlineDotted {
		t.Errorf("the time is underlined %v, want the dotted mark", style.UnderlineStyle)
	}
	if style.UnderlineColor != a.theme.Warning {
		t.Errorf("the mark is %v, want the amber %v", style.UnderlineColor, a.theme.Warning)
	}

	// And an unedited message's time carries no mark at all.
	oneMessage(t, a, v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: "short",
	}.Build())
	if style, ok := a.cellSaying(a.messages[0].at.Row, digit); ok && style.UnderlineStyle != vaxis.UnderlineOff {
		t.Errorf("an unedited time is underlined %v", style.UnderlineStyle)
	}
}

// A run is one shape. Pointing at it has to say which message in it you are
// pointing at, or a run of five looks like one thing you cannot act on.
func TestThePointerLightsOneMessageOfARun(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	if len(a.messages) < 2 {
		t.Fatalf("frame has %d messages, want a run to point into", len(a.messages))
	}

	at := a.messages[0]
	if !a.hoverMessage(at.id) {
		t.Fatal("the pointer landing on a message changed nothing")
	}
	a.paint()

	lit := a.vx.Cell(at.at.Col+1, at.at.Row).Background
	if lit != a.theme.BackgroundHover {
		t.Fatalf("the message under the pointer has ground %v, want the hover %v", lit, a.theme.BackgroundHover)
	}
	// And only that one: the message above it keeps the pane's own ground.
	other := a.messages[1]
	if other.id == at.id {
		return
	}
	if bg := a.vx.Cell(other.at.Col+1, other.at.Row).Background; bg == a.theme.BackgroundHover {
		t.Fatal("the whole run lit up, not the message under the pointer")
	}
}

// The pointer finds a message by where the last frame put it, which is the
// only thing that knows.
func TestThePointerFindsTheMessageItIsOver(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	if len(a.messages) == 0 {
		t.Fatal("frame has no messages")
	}
	at := a.messages[0]
	got := a.messageUnder(vaxis.Mouse{Col: at.at.Col + 1, Row: at.at.Row})
	if got != at.id {
		t.Fatalf("pointer at %d,%d found %q, want %q", at.at.Col+1, at.at.Row, got, at.id)
	}
	if off := a.messageUnder(vaxis.Mouse{Col: 0, Row: 0}); off != "" {
		t.Fatalf("pointer on the chat list found message %q", off)
	}
}

// A message you sent hangs off the rule on the right, and everything in it
// lines up against that edge. A short answer under a long quote, left where
// the quote starts, reads as adrift in the middle of the column rather than as
// the end of the conversation.
func TestAnOutgoingMessageHangsOffTheRuleItStandsOn(t *testing.T) {
	a := stubApp(90, 26, 4, 0)
	c := a.conversation
	reset(c.msgs)
	putMsg(c.msgs, "00000000000000000001", (v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, FromMe: true, Status: v2.MessageStatus_MESSAGE_STATUS_READ, Text: "Haath mai",
		Sender: person("me", "me"),
		ReplyTo: v2.Quote_builder{
			MessageId: "q", Sender: person("x", "someone"),
			Text: "a quoted line long enough to set the width of the whole message",
		}.Build(),
	}.Build()))
	ready(c.msgs, true)
	a.paint()

	words := ""
	for _, line := range strings.Split(a.transcriptRowsText(), "\n") {
		if strings.Contains(line, "Haath mai") {
			words = line
		}
	}
	if words == "" {
		t.Fatalf("the message did not draw:\n%s", a.transcriptRowsText())
	}
	// The rule is the edge the message hangs off, so the last of the words is
	// the cell before it.
	rule := strings.Index(words, "\u258e")
	end := strings.Index(words, "Haath mai") + len("Haath mai")
	if rule < 0 {
		t.Fatalf("the run drew no rule: %q", words)
	}
	if end != rule {
		t.Errorf("the words end at column %d and the rule stands at %d: %q", end, rule, words)
	}
}
