package ui

import "testing"

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
