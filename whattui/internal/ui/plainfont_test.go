package ui

import (
	"strings"
	"testing"

	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/theme"
)

// onTheConsole is whattui where it ran on a virtual console: sixteen colours,
// no graphics, and a font of a couple of hundred glyphs with no pictures in it.
func onTheConsole(a *App) *App {
	a = atTier(a, term.TierPlain)
	a.caps.PlainFont = true
	a.theme = theme.Default()
	return a
}

// A tick is a picture, and a font without pictures draws every one of them as
// the same blank. Two blanks beside two blanks say nothing at all, which is
// what "delivered" and "read" looked like on a console, so the mark is written
// in the alphabet that terminal actually has.
func TestDeliveryStatesAreToldApartWhereTheFontHasNoTicks(t *testing.T) {
	a := onTheConsole(stubApp(90, 26, 4, 0))

	seen := map[string]string{}
	for _, status := range []string{"sent", "delivered", "read", "failed", "pending"} {
		glyph := a.statusGlyph(status)
		if glyph == "" {
			t.Fatalf("%s draws nothing", status)
		}
		for _, r := range glyph {
			if r > 0x7f {
				t.Errorf("%s draws %q, which is not in the console font", status, glyph)
			}
		}
		if other, clash := seen[glyph]; clash {
			t.Errorf("%s and %s both draw %q, so nothing tells them apart", status, other, glyph)
		}
		seen[glyph] = status
	}

	// And the fact is still in the glyph rather than only in the colour, on
	// every terminal: two ticks and two heavy ticks are two different marks.
	rich := stubApp(90, 26, 4, 0)
	if rich.statusGlyph("delivered") == rich.statusGlyph("read") {
		t.Error("delivered and read draw the same glyph, so colour is carrying it alone")
	}
	if rich.statusInk("read") != rich.theme.Accent || rich.statusInk("failed") != rich.theme.Error {
		t.Error("read and failed are not in their own ink")
	}
}

// The mark stands beside the time in the gutter, in its own colour, and the
// time stays as faint as it is on a message nobody sent.
func TestTheDeliveryMarkIsDrawnApartFromTheTime(t *testing.T) {
	a := stubApp(100, 26, 4, 0)
	c := a.conversation
	c.msgs.Reset()
	c.msgs.Upsert("00000000000000000001", mustJSON(proto.MessageRow{
		ID: "m", Kind: "text", Direction: "outgoing", Status: "read",
		Text: "read by everybody", Timestamp: 1758000000,
	}))
	c.msgs.Ready(true, true)
	a.paint()

	mark, time := a.styleOf(t, "✔"), a.styleOf(t, ":")
	if mark.Foreground != a.theme.Accent {
		t.Errorf("the read mark is %v, want the accent", mark.Foreground)
	}
	if time.Foreground != a.theme.TextFaint {
		t.Errorf("the time beside it is %v, want the faint step", time.Foreground)
	}
}

// What the daemon composed opens with a picture and then says the same thing in
// words. Where the picture cannot be drawn it is the half that goes: a hole and
// a word is worse than the word.
func TestAKindLineIsSpelledOutWhereThePictureIsAHole(t *testing.T) {
	console := onTheConsole(stubApp(90, 26, 4, 0))
	rich := stubApp(90, 26, 4, 0)

	voice := proto.MessageRow{
		ID: "m", Kind: "voice", Direction: "incoming",
		Fallback: "🎤 Voice message (0:12)",
	}
	if got := console.body(voice); got != "Voice message (0:12)" {
		t.Errorf("on a console the line reads %q", got)
	}
	if got := rich.body(voice); got != voice.Fallback {
		t.Errorf("in a terminal with pictures the line reads %q", got)
	}

	// What somebody wrote is theirs, picture and all.
	wrote := proto.MessageRow{ID: "m", Kind: "text", Direction: "incoming", Text: "🔥 that was great"}
	if got := console.body(wrote); got != wrote.Text {
		t.Errorf("a message somebody wrote came out as %q", got)
	}
	// And a line that is nothing but a picture keeps it: an empty row says
	// less than a blank one.
	if got := console.spelled("👍"); got != "👍" {
		t.Errorf("a line of nothing but a picture came out as %q", got)
	}
}

// The same on the list, where a photo row opened with a hole where the picture
// was and the word after it did the work anyway.
func TestAPreviewLosesThePictureItCannotDraw(t *testing.T) {
	// Wide enough for the list to carry a preview line, which is where the
	// picture is.
	a := onTheConsole(stubApp(120, 30, 4, 0))
	a.chats.Reset()
	a.chats.Upsert("00000000000000000001", mustJSON(proto.ChatRow{
		ID: "910000000@s.whatsapp.net", Name: "Asha",
		Preview: "📷 Photo", LastMessageTime: 1758000000,
	}))
	a.chats.Ready(true, true)
	a.paint()

	frame := a.frameText()
	if !strings.Contains(frame, "Photo") {
		t.Fatalf("the list says nothing about a photo:\n%s", frame)
	}
	if strings.ContainsRune(frame, '📷') {
		t.Errorf("the console frame still carries a picture:\n%s", frame)
	}
}

// frameText is everything on the frame, row by row.
func (a *App) frameText() string {
	cols, rows := a.vx.Window().Size()
	var b strings.Builder
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			b.WriteString(a.vx.Cell(col, row).Grapheme)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// consoleHas is roughly what a virtual console can draw: its font is the one
// IBM settled in 1981 plus the letters of a couple of alphabets, so line
// drawing and the four arrows are in it, and blocks, dingbats and pictures are
// not.
func consoleHas(r rune) bool {
	switch {
	case r >= 0x2580 && r <= 0x259f: // block elements
		return false
	case r >= 0x2600 && r <= 0x27bf: // misc symbols and dingbats
		return false
	case r >= 0x1f000: // everything pictorial
		return false
	}
	return true
}
