package ui

import (
	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/view"
)

// The selection cursor is the one message the transcript is pointing at, and
// the target of every message action. One concept, not a mode: it appears when
// the arrows are pressed with nothing typed, it follows the arrows from there,
// and it lets go the moment somebody starts typing or presses escape.
//
// Which is why the gesture is the one Slack and Discord already taught: up on
// an empty composer is your last message, and everything you can do to it is on
// the hint line once it is.

// messageIDs is every message in the window the cursor can land on, newest
// first. A day divider and a system line are not things anybody wrote, so
// there is nothing to reply to, edit or star about them.
func (c *conversation) messageIDs() []string {
	ids := make([]string, 0, len(c.runs))
	for _, r := range c.runs {
		if r.divider || r.centred {
			continue
		}
		// A run's own ids read top to bottom, which is oldest first. The
		// cursor counts from the newest, because that is the end it starts at.
		for i := len(r.ids) - 1; i >= 0; i-- {
			ids = append(ids, r.ids[i])
		}
	}
	return ids
}

// messageAbove is how far one message's top sits above the live edge, and how
// tall it is. Rows, counted exactly the way the transcript lays them out: every
// run's height plus the blank row after it, less the offset of the message
// inside its own run.
func (c *conversation) messageAbove(id string) (above, height int, ok bool) {
	rows := 0
	for i, r := range c.runs {
		rows += r.height
		if r.divider || r.centred {
			continue
		}
		off := 0
		if r.name != "" {
			off++
		}
		for _, mid := range r.ids {
			h := c.cache[mid].block.rows()
			if mid == id {
				return rows + i - off, h, true
			}
			off += h
		}
	}
	return 0, 0, false
}

func (a *App) conv() *conversation {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conversation
}

// cursor is the message the actions act on, or empty for none.
func (a *App) cursor() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conversation == nil {
		return ""
	}
	return a.conversation.selected
}

// setCursor points at a message and keeps a copy of it.
//
// The copy is the point. Whether an action applies is asked while App.mu is
// held, and asking the collection under App.mu is the other half of the
// deadlock the transcript already owns: a frame holds the window open and takes
// App.mu inside it. So the row is fetched here, outside both.
func (a *App) setCursor(id string) {
	c := a.conv()
	if c == nil {
		return
	}
	it, ok := c.msgs.Get(id)
	a.mu.Lock()
	if a.conversation == c {
		c.selected = id
		if ok {
			c.selectedRow = it.Value
		}
	}
	a.mu.Unlock()
}

// messageRow is one row of the open transcript by id, for the callers that
// have a message in their hand rather than the cursor. Asked of the collection
// outside App.mu, like everything else.
func (a *App) messageRow(id string) (proto.MessageRow, bool) {
	c := a.conv()
	if c == nil || id == "" {
		return proto.MessageRow{}, false
	}
	it, ok := c.msgs.Get(id)
	return it.Value, ok
}

// clearCursor stops pointing at anything and reports whether it was.
func (a *App) clearCursor() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conversation == nil || a.conversation.selected == "" {
		return false
	}
	a.conversation.selected = ""
	a.conversation.selectedRow = proto.MessageRow{}
	return true
}

// syncCursor brings the cursor's copy of its message up to date, and drops the
// cursor when the message it points at has left the window. The window is the
// daemon's, and a row outside it is not ours to go on pointing at.
//
// Called from inside the window it was handed, which is the only place that
// can answer both questions without asking the collection a second time.
func (a *App) syncCursor(c *conversation, items []view.Item[proto.MessageRow]) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c.selected == "" {
		return
	}
	for _, it := range items {
		if it.ID == c.selected {
			c.selectedRow = it.Value
			return
		}
	}
	c.selected = ""
	c.selectedRow = proto.MessageRow{}
}

// selectedMessage is the row the cursor is on, as of the last time anything
// changed about it.
func (a *App) selectedMessage() (proto.MessageRow, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selectedMessageLocked()
}

func (a *App) selectedMessageLocked() (proto.MessageRow, bool) {
	if a.conversation == nil || a.conversation.selected == "" {
		return proto.MessageRow{}, false
	}
	return a.conversation.selectedRow, true
}

// moveCursor walks the cursor through the window. Positive is back in time,
// the direction the reader thinks in and the direction the up arrow points.
//
// The first press picks the newest message whichever way it came from: that is
// the one on screen, and reaching for it is what the gesture is for. Walking
// off the newest end lets go entirely and hands the keyboard back to the
// composer, because past your last message is where you were about to type.
func (a *App) moveCursor(by int) {
	c := a.conv()
	if c == nil {
		return
	}
	ids := c.messageIDs()
	if len(ids) == 0 {
		return
	}

	at, current := -1, a.cursor()
	for i, id := range ids {
		if id == current {
			at = i
			break
		}
	}
	next := 0
	if at >= 0 {
		next = at + by
	}
	if next < 0 {
		a.clearCursor()
		a.setFocus(FocusComposer)
		return
	}
	if next >= len(ids) {
		next = len(ids) - 1
	}
	a.setCursor(ids[next])
	a.setFocus(FocusTranscript)
	a.revealCursor()
}

// revealCursor scrolls until the message the cursor is on is whole on the
// screen. The window follows the cursor rather than the cursor being penned
// into the rows that happen to be visible.
func (a *App) revealCursor() {
	c := a.conv()
	if c == nil {
		return
	}
	above, height, ok := c.messageAbove(a.cursor())
	if !ok {
		return
	}
	viewport := a.transcriptPage()
	top := viewport + c.scroll - above
	switch {
	case top < 0:
		c.scroll -= top
	case top+height > viewport:
		// A message taller than the pane cannot be shown whole. Its top is the
		// half worth showing: that is where the quote, the name and the first
		// words are.
		c.scroll -= minInt(top+height-viewport, top)
	}
	if c.scroll < 0 {
		c.scroll = 0
	}
	if max := c.maxScroll(viewport); c.scroll > max {
		c.scroll = max
	}
	// Same reach as scrolling by hand: the cursor walking back through the
	// window is what asks for more of it.
	if c.scroll+viewport > c.contentRows-viewport {
		a.loadOlder()
	}
}

// messageGround is the colour under one message, and whether the cursor is on
// it. Three states, one ramp, in the order a chat row uses them: where the
// keyboard is beats where the pointer is.
func (a *App) messageGround(id string) (vaxis.Color, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conversation != nil && a.conversation.selected == id {
		return a.theme.BackgroundActive, true
	}
	if a.hoveredMsg != "" && a.hoveredMsg == id {
		return a.theme.BackgroundHover, false
	}
	return a.theme.Background, false
}

// drawCursorMark stands in the margin beside the message the cursor is on, for
// the whole height of it. The ground says so in colour; this says so in a
// glyph, which is what carries it at the plain tier and under NO_COLOR.
func (a *App) drawCursorMark(pane vaxis.Window, row, height int) {
	glyph := "▎"
	if a.caps.Tier <= term.TierPlain {
		glyph = "|"
	}
	style := vaxis.Style{Foreground: a.theme.Accent, Background: a.theme.BackgroundActive}
	for i := 0; i < height; i++ {
		a.print(pane, 0, row+i, style, glyph)
	}
}
