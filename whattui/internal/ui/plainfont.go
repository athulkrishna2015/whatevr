package ui

import (
	"strings"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"
)

// A terminal whose font has no pictures in it draws an emoji as a hole. The
// kernel's own console is that terminal, and it is not a corner case: it is
// where whattui is the only chat client there is.
//
// The daemon opens its one-line renderings with the picture for the kind and
// then says the same thing in words, so on that terminal the picture is the
// half worth losing: "📷 Photo" reads as "Photo" rather than as a gap and a
// word. Only what leads a line, and only where words follow it, so a message
// that is nothing but a picture keeps it and what somebody wrote in the middle
// of a sentence is left alone. A preview is the one place the two cannot be
// told apart, and a chat list that opens every media row with a hole is the
// worse of the two mistakes.

// spelled is one line with its leading picture taken off, on a terminal that
// cannot draw one, and the line itself everywhere else.
func (a *App) spelled(s string) string {
	if !a.caps.PlainFont {
		return s
	}
	rest := strings.TrimLeft(s[leadingPicture(s):], " ")
	if rest == "" {
		return s
	}
	return rest
}

// body is what to draw as a message's words: what somebody wrote, or the
// daemon's line about a kind whattui does not draw itself. Only the second is
// spelled out, because only the second is the daemon's own wording.
func (a *App) body(m *v2.MessageRow) string {
	text := messageBody(m, a.textOf(m))
	if m.GetTextTruncated() && !m.GetRevoked() {
		// cut short and not expanded: say there is more
		if _, ok := a.expandedOf(m); !ok {
			text += "…"
		}
	}
	if m.GetRevoked() || centred(m) || plainText(m) {
		return text
	}
	return a.spelled(text)
}

// leadingPicture is how many bytes of pictures a line opens with.
func leadingPicture(s string) int {
	cut := 0
	it := vaxis.NewCharacterIterator(s)
	for {
		cluster, ok := it.Next()
		if !ok {
			return cut
		}
		for _, r := range cluster.Grapheme {
			if !isEmojiRune(r) {
				return cut
			}
		}
		cut += len(cluster.Grapheme)
	}
}
