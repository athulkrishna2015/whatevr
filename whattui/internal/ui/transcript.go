package ui

import (
	"bytes"
	"encoding/json"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// entry is one message, laid out. Laying one out means wrapping its text,
// which means measuring every grapheme in it, which for the scripts we shape
// ourselves means shaping them: far and away the most expensive thing a frame
// does, and the one thing about a message that does not change while the
// reader scrolls past it.
type entry struct {
	raw     json.RawMessage
	centred bool
	lines   []string
	bubble  bubble
	height  int
}

// refreshTranscript brings the layout cache up to date with the window.
//
// Keyed on the raw row rather than on the id alone: the daemon upserts a whole
// item for a status tick or an edit, and a version bump that threw the window
// away would re-wrap four hundred messages because one of them was read.
func (a *App) refreshTranscript(c *conversation, items []view.Item[proto.MessageRow], w, rows int) {
	ver := c.msgs.Version()
	if c.cache != nil && c.cacheWidth == w && c.cacheRows == rows && c.cacheVer == ver {
		return
	}
	if c.cache == nil || c.cacheWidth != w || c.cacheRows != rows {
		c.cache = make(map[string]entry, len(items))
	}
	c.cacheWidth, c.cacheRows, c.cacheVer = w, rows, ver

	total := 0
	for _, it := range items {
		e, ok := c.cache[it.ID]
		if !ok || !bytes.Equal(e.raw, it.Raw) {
			e = a.layoutEntry(it, w)
			c.cache[it.ID] = e
		}
		total += e.height + 1
	}
	c.contentRows = total

	// Anything the window no longer holds is gone for good: the daemon owns
	// the window, and a row outside it is not ours to remember.
	if len(c.cache) > len(items) {
		live := make(map[string]struct{}, len(items))
		for _, it := range items {
			live[it.ID] = struct{}{}
		}
		for id := range c.cache {
			if _, ok := live[id]; !ok {
				delete(c.cache, id)
			}
		}
	}
}

func (a *App) layoutEntry(it view.Item[proto.MessageRow], w int) entry {
	m := it.Value
	if m.Centred() {
		lines := a.wrap(m.Body(), w-4)
		return entry{raw: it.Raw, centred: true, lines: lines, height: len(lines)}
	}
	b := a.layoutMessage(m, w)
	return entry{raw: it.Raw, bubble: b, height: a.bubbleHeight(b)}
}

// drawEntry paints one message with its top at row, which may be off either
// end of the pane: a message taller than the screen has to be readable a row
// at a time, so the parts that do not fit are clipped rather than skipped.
func (a *App) drawEntry(pane vaxis.Window, e entry, row, w int) {
	if e.centred {
		style := vaxis.Style{
			Foreground: a.theme.TextMuted, Background: a.theme.Background,
			Attribute: vaxis.AttrItalic,
		}
		for j, line := range e.lines {
			indent := (w - a.width(line)) / 2
			if indent < 0 {
				indent = 0
			}
			a.print(pane, indent, row+j, style, line)
		}
		return
	}

	col := 2
	if e.bubble.outgoing {
		col = w - e.bubble.Width() - 2
		if col < 2 {
			col = 2
		}
	}
	a.drawBubble(pane, e.bubble, col, row)
}

// maxScroll is how far back the window can go before it runs out of itself.
func (c *conversation) maxScroll(viewport int) int {
	if n := c.contentRows - viewport; n > 0 {
		return n
	}
	return 0
}
