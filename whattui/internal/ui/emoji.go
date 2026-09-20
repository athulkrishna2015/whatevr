package ui

import (
	"strings"

	"go.rockorager.dev/vaxis"
)

// emojiOnlyCount reports how many emoji a message is, when it is nothing but
// emoji, and zero otherwise.
//
// WhatsApp draws those big, and it is the one place in a chat where larger
// text is the meaning rather than decoration: a lone 🎉 is not a sentence, it
// is a gesture.
func emojiOnlyCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Counted in grapheme clusters, not runes: a family joined by zero width
	// joiners, a skin tone on a thumb and a flag built from regional
	// indicators are each one gesture made of several codepoints, and
	// counting codepoints would call the family three emoji.
	count := 0
	it := vaxis.NewCharacterIterator(s)
	for {
		cluster, ok := it.Next()
		if !ok {
			break
		}
		if strings.TrimSpace(cluster.Grapheme) == "" {
			continue
		}
		for _, r := range cluster.Grapheme {
			if !isEmojiRune(r) {
				return 0
			}
		}
		count++
	}
	return count
}

func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF: // symbols, pictographs, supplements
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols and dingbats
		return true
	case r >= 0x1F000 && r <= 0x1F2FF: // mahjong through enclosed supplement
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators, for flags
		return true
	case r >= 0x2190 && r <= 0x21FF: // arrows
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // misc symbols and arrows
		return true
	case r == 0x200D, r == 0xFE0E, r == 0xFE0F:
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF:
		return true
	case r >= 0xE0020 && r <= 0xE007F:
		return true
	}
	return false
}

// bigEmojiScale is how large an emoji-only message draws. Three emoji get the
// full gesture; more than six is a sentence made of emoji and reads better at
// its natural size.
func bigEmojiScale(count int) int {
	switch {
	case count == 0 || count > 6:
		return 1
	case count <= 3:
		return 3
	default:
		return 2
	}
}
