package ui

import (
	"strings"

	"go.rockorager.dev/vaxis"
)

// span is a stretch of a line that shares one decoration. Today that is only
// whether it is part of a link, which is the one thing inside a message body
// that is not plain text.
type span struct {
	text string
	// link is the whole url, even when this span holds three characters of
	// the middle of it. A wrapped or truncated link is still the link it was.
	link string
}

// line is one row of wrapped text.
type line []span

func lineText(l line) string {
	if len(l) == 1 {
		return l[0].text
	}
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.text)
	}
	return b.String()
}

func (a *App) lineWidth(l line) int {
	n := 0
	for _, s := range l {
		n += a.width(s.text)
	}
	return n
}

// appendSpan adds text to a line, merging into the last span when the
// decoration is the same, so a line is as few spans as it can be.
func appendSpan(l line, s span) line {
	if s.text == "" {
		return l
	}
	if n := len(l); n > 0 && l[n-1].link == s.link {
		l[n-1].text += s.text
		return l
	}
	return append(l, s)
}

// splitLine cuts a line at a cell width, never mid-grapheme, and returns what
// fit and what did not.
func (a *App) splitLine(l line, width int) (head, tail line) {
	used := 0
	for i, s := range l {
		w := a.width(s.text)
		if used+w <= width {
			head = appendSpan(head, s)
			used += w
			continue
		}
		cut := a.clip(s.text, width-used)
		head = appendSpan(head, span{text: cut, link: s.link})
		tail = appendSpan(tail, span{text: s.text[len(cut):], link: s.link})
		tail = append(tail, l[i+1:]...)
		return head, tail
	}
	return head, nil
}

// wrapSpans breaks text to a cell width, on spaces where it can and mid-word
// where it must, keeping every span's link with it. Widths are the terminal's,
// not rune counts.
func (a *App) wrapSpans(s string, width int, links bool) []line {
	if width < 1 {
		width = 1
	}
	var out []line
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, nil)
			continue
		}
		var cur line
		for _, w := range a.words(para, links) {
			switch {
			case len(cur) == 0:
				cur = w
			case a.lineWidth(cur)+1+a.lineWidth(w) <= width:
				cur = appendSpan(cur, span{text: " "})
				for _, sp := range w {
					cur = appendSpan(cur, sp)
				}
			default:
				out = append(out, cur)
				cur = w
			}
			// A single word wider than the line, which is usually a url, is
			// cut rather than allowed to run off the pane.
			for a.lineWidth(cur) > width {
				head, tail := a.splitLine(cur, width)
				if len(head) == 0 {
					break
				}
				out = append(out, head)
				cur = tail
			}
		}
		if len(cur) > 0 {
			out = append(out, cur)
		}
	}
	return out
}

// linkLine turns one row's worth of text into spans, marking any url in it.
// Unlike wrapSpans it never breaks: the caller has exactly one row and cuts it
// with clipLine.
func (a *App) linkLine(s string) line {
	var out line
	for i, w := range strings.Fields(s) {
		if i > 0 {
			out = appendSpan(out, span{text: " "})
		}
		for _, sp := range linkSpans(w) {
			out = appendSpan(out, sp)
		}
	}
	return out
}

// clipLine cuts a line to a width, keeping every surviving fragment's link.
// This is what makes a chat list preview worth clicking: the row shows
// "https://music.youtube.com/wat" because that is all the column holds, and
// the escape under it still carries the whole url.
func (a *App) clipLine(l line, width int) line {
	if a.lineWidth(l) <= width {
		return l
	}
	head, _ := a.splitLine(l, width)
	return head
}

// wrap is wrapSpans for the panes with nothing to decorate.
func (a *App) wrap(s string, width int) []string {
	lines := a.wrapSpans(s, width, false)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lineText(l)
	}
	return out
}

// words splits a paragraph into wrappable units, each already cut into spans
// around whatever link it holds.
func (a *App) words(para string, links bool) []line {
	var out []line
	for _, w := range strings.Fields(para) {
		if !links {
			out = append(out, line{{text: w}})
			continue
		}
		out = append(out, linkSpans(w))
	}
	return out
}

// printLine writes one wrapped line, giving every link span the OSC 8 that
// makes the whole url reachable from a fragment of it.
//
// The point of the hyperlink is precisely the case where the url does not fit:
// the reader sees "https://music.youtube.com/wat" and the terminal still opens
// the right video, because the escape carries the url and the cells only carry
// what there was room for.
func (a *App) printLine(win vaxis.Window, col, row int, style vaxis.Style, l line) int {
	for _, s := range l {
		st := style
		if s.link != "" {
			if a.caps.Hyperlinks {
				st.Hyperlink = s.link
			}
			// Underlined either way. A terminal without OSC 8 still has to
			// show that this is a url, and a colour would spend the budget
			// the palette keeps for state.
			st.UnderlineStyle = vaxis.UnderlineSingle
		}
		col = a.print(win, col, row, st, s.text)
	}
	return col
}
