package ui

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
)

// under finds a line of the transcript holding want and answers the line after
// it, with its spacing squeezed out: a wide glyph leaves the cell beside it
// empty, and how many of those a row has is not what any of this is about.
func under(t *testing.T, a *App, want string) string {
	t.Helper()
	lines := strings.Split(a.transcriptRowsText(), "\n")
	for i, line := range lines {
		if strings.Contains(line, want) && i+1 < len(lines) {
			return strings.Join(strings.Fields(lines[i+1]), " ")
		}
	}
	t.Fatalf("no message saying %q on screen:\n%s", want, a.transcriptRowsText())
	return ""
}

// A reaction is not a message and not a flag: it is content that belongs to the
// message above it, so it goes on a row of its own under the words, and the
// same emoji from three people is one thing to read rather than three.
func TestReactionsDrawUnderTheMessageTheyAreAbout(t *testing.T) {
	a := mockApp(t, tortureScenario, "Reactions", 100, 30)
	a.paint()

	if got := under(t, a, "one reaction, from one person"); !strings.Contains(got, "👍") {
		t.Errorf("under a message with one reaction the row reads %q", got)
	}
	if got := under(t, a, "to read"); !strings.Contains(got, "😂 3") {
		t.Errorf("three of one emoji read as %q, want one pill counting them", got)
	}
	// What a deletion takes with it. WhatsApp drops the reactions on a message
	// somebody deleted for everybody, and a row of applause under a sentence
	// saying the message is gone is applause for nothing.
	if got := under(t, a, "This message was deleted"); strings.Contains(got, "😢") {
		t.Errorf("a deleted message kept its reactions: %q", got)
	}
	// And one taken back leaves nothing at all, rather than an empty strip.
	if got := under(t, a, "under it"); strings.Contains(got, "👎") {
		t.Errorf("a reaction that was taken back is still drawn: %q", got)
	}
}

// The one you put there is the one you can take back, so it has to be findable
// at a glance: the accent and the weight, which between them survive a terminal
// with sixteen colours and a reader who cannot tell two of them apart.
func TestYourOwnReactionIsMarkedApartFromEverybodyElses(t *testing.T) {
	a := stubApp(90, 26, 4, 0)
	c := a.conversation
	c.msgs.Reset()
	c.msgs.Upsert("00000000000000000001", mustJSON(proto.MessageRow{
		ID: "m", Kind: "text", Direction: "incoming", Text: "something worth agreeing with",
		Sender: proto.Sender{ID: "x", Name: "someone"},
		Reactions: []proto.Reaction{
			{Emoji: "👍", SenderID: "x", SenderName: "someone"},
			{Emoji: "🔥", FromMe: true},
		},
	}))
	c.msgs.Ready(true, true)
	a.paint()

	strip := under(t, a, "something worth agreeing with")
	if !strings.Contains(strip, "👍") || !strings.Contains(strip, "🔥") {
		t.Fatalf("the strip reads %q, want both reactions", strip)
	}

	mine, theirs := a.styleOf(t, "🔥"), a.styleOf(t, "👍")
	// The underline is the one that has to be there. An emoji is drawn by the
	// font in the font's own colours, so a foreground and a weight are marks on
	// a cell nobody can see.
	if mine.UnderlineStyle != vaxis.UnderlineSingle {
		t.Error("your own reaction is not underlined, so nothing visible marks it as yours")
	}
	if mine.Foreground != a.theme.Accent {
		t.Errorf("your own reaction is drawn in %v, want the accent", mine.Foreground)
	}
	if theirs.UnderlineStyle == mine.UnderlineStyle || theirs.Foreground == mine.Foreground {
		t.Error("somebody else's reaction is drawn the same way as your own")
	}
}

// styleOf is how the frame drew a grapheme, for the tests about which ink
// carries which fact.
func (a *App) styleOf(t *testing.T, grapheme string) vaxis.Style {
	t.Helper()
	cols, rows := a.vx.Window().Size()
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			if cell := a.vx.Cell(col, row); cell.Grapheme == grapheme {
				return cell.Style
			}
		}
	}
	t.Fatalf("%q is not on the frame", grapheme)
	return vaxis.Style{}
}

// A message is as wide as the widest thing in it, and a strip of reactions is
// one of the things in it. Laid out any other way the pills run out of the
// column the message occupies and over whatever is beside them.
func TestAMessageIsWideEnoughForItsReactions(t *testing.T) {
	a := stubApp(90, 26, 4, 0)
	row := proto.MessageRow{
		ID: "m", Kind: "text", Direction: "incoming", Text: "short",
		Sender: proto.Sender{ID: "x", Name: "someone"},
	}
	plain := a.layoutMessage(row, 60)

	row.Reactions = []proto.Reaction{
		{Emoji: "👍"}, {Emoji: "🎉"}, {Emoji: "😮"}, {Emoji: "❤️"},
	}
	reacted := a.layoutMessage(row, 60)

	if reacted.rows() != plain.rows()+1 {
		t.Errorf("a message with reactions is %d rows, want one more than %d", reacted.rows(), plain.rows())
	}
	if reacted.width <= plain.width {
		t.Errorf("width with reactions = %d, want wider than the word above them (%d)", reacted.width, plain.width)
	}
}

// A narrow column runs out of room before a popular message runs out of
// reactions. What is left over is counted rather than cut: half a pill is a lie
// about who reacted, and how many are missing is the part worth the columns.
func TestReactionsThatDoNotFitAreCountedRatherThanCut(t *testing.T) {
	a := stubApp(90, 26, 4, 0)
	groups := groupReactions([]proto.Reaction{
		{Emoji: "👍"}, {Emoji: "🎉"}, {Emoji: "😮"}, {Emoji: "❤️"}, {Emoji: "🔥"},
	})

	pills, width := a.pillsFor(groups, 12)
	if width > 12 {
		t.Fatalf("the strip came out %d wide in room for 12", width)
	}
	if len(pills) == 0 || len(pills) == len(groups) {
		t.Fatalf("pills = %#v, want some of them and a count", pills)
	}
	if last := pills[len(pills)-1].text; !strings.HasPrefix(last, "+") {
		t.Errorf("the strip ends with %q, want the number it left out", last)
	}
	// And with nothing like enough room, nothing at all: a lone "+5" under a
	// message says a number and not what it counts.
	if pills, _ := a.pillsFor(groups, 1); pills != nil {
		t.Errorf("a column one cell wide drew %#v", pills)
	}
}

// Everybody who reacted is worth knowing, and the strip has room for none of
// it. The picker is where that lives, so it leads with what is already there.
func TestTheReactionPickerLeadsWithWhatIsAlreadyOnTheMessage(t *testing.T) {
	a := stubApp(90, 26, 4, 0)
	choices := a.reactChoices(proto.MessageRow{
		Reactions: []proto.Reaction{
			{Emoji: "👍", SenderName: "Asha"},
			{Emoji: "👍", SenderName: "Ravi"},
			{Emoji: "🔥", FromMe: true},
		},
	}, "")

	if len(choices) < 3 {
		t.Fatalf("the picker offers %d rows", len(choices))
	}
	if !strings.Contains(choices[0].Detail, "Asha") || !strings.Contains(choices[0].Detail, "Ravi") {
		t.Errorf("the first row says %q, want who put it there", choices[0].Detail)
	}
	if !choices[1].Mine || !strings.Contains(choices[1].Detail, "takes it back") {
		t.Errorf("your own reaction reads %#v, want the row that takes it off", choices[1])
	}
	// And the palette follows, without repeating what is already on the message.
	for _, c := range choices[2:] {
		if c.Emoji == "👍" || c.Emoji == "🔥" {
			t.Errorf("the palette offers %q again, which is already on the message", c.Emoji)
		}
	}
}
