package ui

import (
	"strings"
	"unicode/utf8"
)

// Everything on this screen was typed by somebody else. A chat name, a push
// name, a message: all of it arrives from a phone whatevr has never met, and a
// terminal acts on what it is handed. An escape byte in a contact's name is a
// contact who can colour your screen, move your cursor, set your title or write
// your clipboard, and a bidi override is a name that reverses how the rest of
// the line reads. Neither is exotic; both are what the character was designed
// to do.
//
// So nothing untrusted becomes a cell without coming through here. It is the
// same argument as the left edge: a column keeps its edge because every pane
// prints through one function, and a terminal keeps its integrity for the same
// reason.

// safe drops what a terminal must never be handed. It is written to cost
// nothing in the ordinary case, which is every string on almost every frame:
// one scan, and the original string back if there was nothing to remove.
func safe(s string) string {
	if !needsSanitising(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if hostile(r) {
			continue
		}
		if r == utf8.RuneError {
			// Invalid bytes reach us as RuneError one byte at a time, which is
			// a row of replacement characters where a name was. One is enough
			// to say the same thing.
			if strings.HasSuffix(b.String(), string(utf8.RuneError)) {
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// needsSanitising is the fast path: almost nothing needs work, and finding that
// out must not allocate.
func needsSanitising(s string) bool {
	for i := 0; i < len(s); i++ {
		// Every rune worth removing either is an ascii control or starts with a
		// byte no plain latin text carries, so a byte scan settles it without
		// decoding.
		if c := s[i]; c < 0x20 || c == 0x7f || c >= 0x80 {
			for _, r := range s[i:] {
				if hostile(r) {
					return true
				}
			}
			return false
		}
	}
	return false
}

// hostile is the whole list, and it is deliberately short.
//
// Control characters go because a terminal executes them. The bidi overrides,
// embeddings and isolates go because they reorder a line that is already drawn
// left to right, which is a name that can pretend to be another name. What
// stays is everything that carries meaning: the zero width joiner an emoji
// family is made of, the zero width non-joiner Persian and Devanagari need, the
// marks, the variation selectors. Tab and newline are gone too, because every
// caller here draws one line and a tab in the middle of a chat name is a column
// that jumps.
func hostile(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f:
		return true
	case r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x061c:
		return true
	case r == 0xfeff:
		return true
	}
	return false
}
