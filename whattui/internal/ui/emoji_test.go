package ui

import (
	"testing"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

func TestEmojiOnlyCount(t *testing.T) {
	tests := map[string]int{
		"😂":     1,
		"😂😂":    2,
		"😂 😂 😂": 3,
		"🎉🎉🎉🎉":  4,
		"":      0,
		"   ":   0,
		"hello": 0,
		"😂 lol": 0,
		"lol 😂": 0,
		"👍🏽":    1, // a skin tone modifier is part of one gesture
		"👨‍👩‍👧": 1, // a zero width joiner sequence is one gesture
		"❤️":    1, // a variation selector is not a second emoji
	}
	for in, want := range tests {
		if got := emojiOnlyCount(in); got != want {
			t.Errorf("emojiOnlyCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestBigEmojiScale(t *testing.T) {
	for count, want := range map[int]int{0: 1, 1: 3, 3: 3, 4: 2, 6: 2, 7: 1, 20: 1} {
		if got := bigEmojiScale(count); got != want {
			t.Errorf("bigEmojiScale(%d) = %d, want %d", count, got, want)
		}
	}
}

// The size a lone emoji is drawn at is the terminal's arithmetic, not ours: it
// claims a block of cells, and a terminal that drops one or puts it somewhere
// else leaves the model wrong about a region of the screen with nothing but a
// repaint to say so. It is asked for rather than assumed until that has stopped
// happening on real terminals.
func TestNothingIsDrawnLargeUnlessItWasAskedFor(t *testing.T) {
	a := stubApp(90, 26, 4, 0)
	a.caps.TextScale = true
	row := v2.MessageRow_builder{Id: "m", TextBody: &v2.Text{}, Text: "\U0001F389"}.Build()

	if got := a.layoutMessage(row, 90).scale; got != 1 {
		t.Errorf("a lone emoji drew at scale %d with nothing asking for it", got)
	}
	t.Setenv("WHATTUI_BIG_EMOJI", "1")
	if got := a.layoutMessage(row, 90).scale; got <= 1 {
		t.Errorf("asked for, a lone emoji still drew at scale %d", got)
	}
}
