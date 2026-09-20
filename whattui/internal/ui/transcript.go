package ui

import (
	"bytes"
	"encoding/json"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/paint"
	"whattui/internal/proto"
	"whattui/internal/theme"
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
	block   block
}

// run is everything one person said without being interrupted, and it is the
// unit the transcript draws: a rule down its side in that person's colour,
// their name once at the top, and one line per message with the time at the
// end of it.
//
// Drawing a shape per message instead turns a screenful of short messages into
// a screenful of little boxes. The run is the shape; a message inside it is
// found by pointing at it, which is what the hover is for.
type run struct {
	// ids run top to bottom, which is the reverse of the window, because the
	// window is newest first and the newest message is at the bottom.
	ids      []string
	outgoing bool
	centred  bool
	// sender is who spoke, for the colour and for the disc beside their name.
	sender string
	// divider is a day rather than a person: the line that says the messages
	// under it happened on another day. It has a name and no messages.
	divider bool
	name    string
	colour  vaxis.Color
	width   int
	height  int
}

// speaker identifies a run. A message nobody wrote breaks the run rather than
// joining it, which is why the id is in here: two texts either side of a
// system line are not one run.
func speaker(m proto.MessageRow, id string) string {
	if m.Centred() {
		return id
	}
	return m.Direction + "\x00" + m.Sender.ID
}

// refreshTranscript brings the layout cache up to date with the window.
//
// Keyed on the raw row rather than on the id alone: the daemon upserts a whole
// item for a status tick or an edit, and a version bump that threw the window
// away would re-wrap four hundred messages because one of them was read.
func (a *App) refreshTranscript(c *conversation, items []view.Item[proto.MessageRow], state view.State, w, rows int) {
	// The version arrives with the window rather than being asked for. A
	// second read lock on the collection we are already inside is a deadlock
	// the moment a writer is waiting, and a writer is the daemon delivering a
	// message, which is a thing that happens while you read.
	ver := state.Version
	// Only a group needs to say who is talking. In a chat with one other
	// person, both names are on the screen already and every one of them
	// costs a row.
	group := false
	if it, ok := a.chats.Get(c.chatID); ok {
		group = it.Value.IsGroup
	}
	if c.cache != nil && c.cacheWidth == w && c.cacheRows == rows &&
		c.cacheVer == ver && c.cacheGroup == group && c.cacheBoxed == a.boxed {
		return
	}
	if c.cache == nil || c.cacheWidth != w || c.cacheRows != rows {
		c.cache = make(map[string]entry, len(items))
	}
	c.cacheWidth, c.cacheRows, c.cacheVer = w, rows, ver
	c.cacheGroup, c.cacheBoxed = group, a.boxed

	for _, it := range items {
		if e, ok := c.cache[it.ID]; !ok || !bytes.Equal(e.raw, it.Raw) {
			c.cache[it.ID] = a.layoutEntry(it, w)
		}
	}

	c.runs = a.runsOf(c, items, w, group)
	total := 0
	for _, r := range c.runs {
		total += r.height + 1
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

// runsOf gathers the window into runs, newest first, the way the transcript
// draws them.
func (a *App) runsOf(c *conversation, items []view.Item[proto.MessageRow], w int, group bool) []run {
	runs := make([]run, 0, len(items))
	for i := 0; i < len(items); {
		key := speaker(items[i].Value, items[i].ID)
		last := i
		for last+1 < len(items) && speaker(items[last+1].Value, items[last+1].ID) == key {
			last++
		}

		top := items[last].Value
		r := run{outgoing: top.Outgoing(), centred: top.Centred(), colour: a.theme.Accent}
		for j := last; j >= i; j-- {
			r.ids = append(r.ids, items[j].ID)
		}
		if !r.outgoing {
			r.sender = top.Sender.ID
			r.colour = a.theme.IdentityFor(top.Sender.ID)
		}
		// The person who spoke is named at the top of what they said, once,
		// and only where there is more than one of them.
		if group && !r.outgoing && !r.centred && top.Sender.Name != "" {
			r.name = top.Sender.Name
		}

		for _, id := range r.ids {
			r.width = maxInt(r.width, c.cache[id].block.width)
		}
		r.width = minInt(maxInt(r.width, a.width(r.name)), a.runRoom(w))
		if r.name != "" {
			r.height++
		}
		for _, id := range r.ids {
			if e := c.cache[id]; e.centred {
				r.height += len(e.lines)
			} else {
				r.height += e.block.rows()
			}
		}
		runs = append(runs, r)
		// A run drawn above this one on the screen is one further along the
		// window, so the day it belongs to is decided here and the line that
		// says so is appended after it.
		if last+1 < len(items) && !sameDay(top.Timestamp, items[last+1].Value.Timestamp) {
			runs = append(runs, run{divider: true, height: 1, name: dayLabel(top.Timestamp)})
		}
		i = last + 1
	}
	return runs
}

func sameDay(a, b int64) bool {
	at, bt := time.Unix(a, 0), time.Unix(b, 0)
	ay, am, ad := at.Date()
	by, bm, bd := bt.Date()
	return ay == by && am == bm && ad == bd
}

// dayLabel names a day the way a person would say it.
func dayLabel(stamp int64) string {
	t := time.Unix(stamp, 0)
	now := time.Now()
	switch {
	case sameDay(stamp, now.Unix()):
		return "Today"
	case sameDay(stamp, now.AddDate(0, 0, -1).Unix()):
		return "Yesterday"
	case now.Sub(t) < 6*24*time.Hour:
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("Monday 2 January")
	default:
		return t.Format("2 January 2006")
	}
}

// runRoom is the widest the words may be. Capped at a measure rather than at
// the pane, because a line of ninety characters is a line nobody can find
// their way back to the start of.
func (a *App) runRoom(pane int) int {
	// The gutter side is paid for in full and the other side keeps a margin,
	// so the widest message still stops short of the far edge instead of
	// running into it.
	room := pane - (runLead + runGutter + a.chromeCols()) - runLead - 2
	return maxInt(minInt(room, runMeasure), 8)
}

// chromeCols is what the decoration takes either side of the words: one column
// for the rule, or a border and a padding column for a box.
func (a *App) chromeCols() int {
	if a.boxed {
		return 2 * (1 + bubblePadX)
	}
	return 1
}

const (
	// runLead is the margin between the pane edge and the gutter.
	runLead = 1
	// The rails the times stand in, beside the rule and outside the words. A
	// message you sent carries its ticks and a message you received does not,
	// so the two rails are not the same width: making them both as wide as
	// the wider one is three empty columns down the left of every screen.
	runGutter   = 8
	runGutterIn = 6
	// runMeasure is the longest line of prose worth setting. Newspapers
	// settled this argument a century ago.
	runMeasure = 72
	// bubblePadX is the space between a box and its words, per side.
	bubblePadX = 1
	// avatarCols is the disc beside a name: three cells around one letter, so
	// the letter lands on the middle of the circle.
	avatarCols = 3
)

func (a *App) layoutEntry(it view.Item[proto.MessageRow], w int) entry {
	m := it.Value
	if m.Centred() {
		return entry{raw: it.Raw, centred: true, lines: a.wrap(m.Body(), w-4)}
	}
	return entry{raw: it.Raw, block: a.layoutMessage(m, w)}
}

// drawRun paints one run with its top at row, which may be off either end of
// the pane: a run taller than the screen has to be readable a row at a time,
// so the parts that do not fit are clipped rather than skipped.
func (a *App) drawRun(pane vaxis.Window, c *conversation, r run, row, w int) {
	if r.divider {
		a.drawDay(pane, r.name, row, w)
		return
	}
	if r.centred {
		style := vaxis.Style{
			Foreground: a.theme.TextMuted, Background: a.theme.Background,
			Attribute: vaxis.AttrItalic,
		}
		for _, id := range r.ids {
			for _, line := range c.cache[id].lines {
				a.print(pane, maxInt((w-a.width(line))/2, 0), row, style, line)
				row++
			}
		}
		return
	}

	// The rule stands at one column for the whole run, whatever the messages
	// in it are, so a run has one straight edge. Each message then hangs off
	// that edge: incoming to the right of it, sent to the left, which is what
	// puts a short message next to the rule instead of adrift in the column.
	rule := runLead + runGutterIn + 1
	if r.outgoing {
		rule = w - runLead - runGutter - 2
	}

	if r.name != "" {
		// The disc goes in the gutter, which on this row holds no time. It is
		// the one place a face fits without taking a column from the words.
		a.avatar(pane, rule-avatarCols-1, row, avatarCols, 1, r.sender, r.name, a.theme.Background)
		a.print(pane, a.wordsAt(r.outgoing, rule, r.width), row, vaxis.Style{
			Foreground: r.colour, Background: a.theme.Background, Attribute: vaxis.AttrBold,
		}, a.clip(r.name, r.width))
		row++
	}

	top := row
	for _, id := range r.ids {
		e := c.cache[id]
		width := minInt(e.block.width, r.width)
		words := a.wordsAt(r.outgoing, rule, width)
		height := e.block.rows()
		ground := a.hoverGround(id)
		// The pointer lights the whole row, not the words: a message is a
		// line of the conversation, and the line is what you point at.
		a.noteMessage(pane, id, 0, row, w, height)
		a.paintBand(pane, 0, row, w, height, ground)
		if a.boxed {
			a.drawBox(pane, r, words-1-bubblePadX, row, width+2*(1+bubblePadX), height)
		}
		a.drawStamp(pane, r, e.block.stamp, rule, row, ground)
		a.drawBlock(pane, e.block, words, row, width, ground)
		row += height
	}
	if !a.boxed {
		a.drawRule(pane, rule, top, row-top, r.colour)
	}
}

// wordsAt is the column a message's words start in: just inside the rule, on
// the side the message came from.
func (a *App) wordsAt(outgoing bool, rule, width int) int {
	pad := 1
	if a.boxed {
		pad = 1 + bubblePadX
	}
	if outgoing {
		return rule - pad + 1 - width
	}
	return rule + pad
}

// drawScrollbar is how far down the conversation you are, in the last column
// of the pane. Nothing when everything fits: a bar that is always full length
// is a bar that says nothing.
func (a *App) drawScrollbar(pane vaxis.Window, w, h, content, scroll int) {
	if content <= h || h < 3 {
		return
	}
	span := maxInt(h*h/content, 1)
	// scroll counts up from the live edge, so the thumb runs the other way.
	top := (h - span) * (content - h - scroll) / (content - h)
	col := w - 1
	// Quieter than a speaker's rule on purpose: where you are in a
	// conversation matters less than who is talking.
	if ink, ok := theme.Paint(a.theme.Background, a.theme.Border, 0.7); ok && a.painted() {
		a.paintRect(pane, col, top, 1, span, func(pw, ph int) paint.Spec {
			return paint.Rule{W: pw, H: ph, Fill: ink, Thick: maxInt(pw/8, 1)}
		})
		return
	}
	style := vaxis.Style{Foreground: a.theme.Border, Background: a.theme.Background}
	for i := 0; i < span; i++ {
		a.print(pane, col, top+i, style, "▕")
	}
}

// drawDay rules a line across the transcript with a day written into it. The
// line is drawn where the terminal can draw: a row of box glyphs is a fence,
// and one pixel across the pane is a rule under a date.
func (a *App) drawDay(pane vaxis.Window, label string, row, w int) {
	style := vaxis.Style{Foreground: a.theme.TextFaint, Background: a.theme.Background}
	text := " " + label + " "
	at := maxInt((w-a.width(text))/2, 0)
	a.print(pane, at, row, style, text)

	left, right := runLead, w-runLead
	if ink, ok := theme.Paint(a.theme.Background, a.theme.Border, 1); ok && a.painted() {
		hair := func(col, cells int) {
			a.paintRect(pane, col, row, cells, 1, func(pw, ph int) paint.Spec {
				return paint.Hairline{W: pw, H: ph, Fill: ink, Thick: maxInt(ph/12, 1)}
			})
		}
		hair(left, at-left)
		hair(at+a.width(text), right-at-a.width(text))
		return
	}
	bx := boxFor(a.caps)
	a.rule(pane, left, row, at-left, bx.horizontal, style)
	a.rule(pane, at+a.width(text), row, right-at-a.width(text), bx.horizontal, style)
}

// drawStamp puts the time in the gutter, on the first row of the message and
// hard against the rule: one rail of times down the outside of the transcript,
// where it can be read as a column instead of hunted for at the end of every
// sentence.
func (a *App) drawStamp(pane vaxis.Window, r run, stamp string, rule, row int, ground vaxis.Color) {
	if stamp == "" {
		return
	}
	style := vaxis.Style{Foreground: a.theme.TextFaint, Background: ground}
	if r.outgoing {
		a.print(pane, rule+2, row, style, a.clip(stamp, runGutter))
		return
	}
	stamp = a.clip(stamp, runGutterIn)
	a.print(pane, rule-1-a.width(stamp), row, style, stamp)
}

// drawRule marks who is talking down the side of what they said. A hairline
// where the terminal can draw one: a quarter block glyph is a quarter of a
// cell of solid colour, and two pixels with round ends is a rule.
func (a *App) drawRule(pane vaxis.Window, col, row, height int, colour vaxis.Color) {
	if height < 1 {
		return
	}
	if ink, ok := theme.Paint(a.theme.Background, colour, 1); ok && a.painted() {
		a.paintRect(pane, col, row, 1, height, func(pw, ph int) paint.Spec {
			return paint.Rule{W: pw, H: ph, Fill: ink, Thick: maxInt(pw/5, 2)}
		})
		return
	}
	glyph := "▎"
	if a.caps.Tier <= 0 {
		glyph = "|"
	}
	style := vaxis.Style{Foreground: colour, Background: a.theme.Background}
	for i := 0; i < height; i++ {
		a.print(pane, col, row+i, style, glyph)
	}
}

// drawBox is the opt-in shape: one panel per message, the way a chat
// application with a window to itself draws them.
func (a *App) drawBox(pane vaxis.Window, r run, col, row, width, height int) {
	fill, edge := a.theme.PaintIn, a.theme.PaintInEdge
	anchor := paint.Left
	if r.outgoing {
		fill, edge = a.theme.PaintOut, a.theme.PaintOutEdge
		anchor = paint.Right
	}
	if a.painted() {
		a.paintRect(pane, col, row, width, height, func(pw, ph int) paint.Spec {
			return paint.Bubble{W: pw, H: ph, Fill: fill, Edge: edge, Radius: a.bubbleRadius(), Anchor: anchor}
		})
		return
	}
	// Typed, the box is a column of glyphs either side. No top or bottom
	// rule: a row of them per message is what "too spaced out" was.
	bx := boxFor(a.caps)
	style := vaxis.Style{Foreground: a.theme.Border, Background: a.bubbleGround(r)}
	for i := 0; i < height; i++ {
		a.print(pane, col, row+i, style, bx.vertical)
		a.print(pane, col+width-1, row+i, style, bx.vertical)
	}
}

// bubbleGround is the cell colour a box has below the graphics tiers, where
// the ground has to come from the cell because there is nothing to draw with.
func (a *App) bubbleGround(r run) vaxis.Color {
	if !a.boxed || a.painted() {
		return a.theme.Background
	}
	if r.outgoing {
		return a.theme.BubbleOut
	}
	return a.theme.BubbleIn
}

// bubbleRadius is how round a corner is, in pixels. Two fifths of a cell:
// enough to read as a curve at twenty pixels a row, not so much that a one
// line box turns into a pill.
func (a *App) bubbleRadius() int {
	_, ch := a.cellPix()
	return maxInt(ch*2/5, 3)
}

// noteMessage remembers where a message landed, so the pointer can find it.
func (a *App) noteMessage(pane vaxis.Window, id string, col, row, w, h int) {
	oc, or := pane.Origin()
	a.messages = append(a.messages, messageAt{
		id: id,
		at: layout.Rect{Col: oc + col, Row: or + row, Width: w, Height: h},
	})
}

// messageAt is where one message landed on the last frame.
type messageAt struct {
	id string
	at layout.Rect
}

// hoverGround is the colour under one message: the pane's own, unless the
// pointer is on it. A run is one shape, so this is what says where one message
// in it ends and the next begins.
func (a *App) hoverGround(id string) vaxis.Color {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hoveredMsg != "" && a.hoveredMsg == id {
		return a.theme.BackgroundHover
	}
	return a.theme.Background
}

// paintBand lays the ground for one message, gutter included, so the pointer
// lights up the whole row it is on rather than the words alone.
func (a *App) paintBand(pane vaxis.Window, col, row, w, h int, ground vaxis.Color) {
	if ground == a.theme.Background {
		return
	}
	for i := 0; i < h; i++ {
		a.blank(pane, col, row+i, w, vaxis.Style{Background: ground})
	}
}

// toggleBoxes switches between a rule per run and a panel per message. The
// rule is the default because it is the one that scales: a screenful of short
// messages is a screenful of boxes the other way.
func (a *App) toggleBoxes() {
	a.mu.Lock()
	a.boxed = !a.boxed
	on := a.boxed
	a.mu.Unlock()
	if on {
		a.toast("message boxes on")
	} else {
		a.toast("message boxes off")
	}
}

// maxScroll is how far back the window can go before it runs out of itself.
func (c *conversation) maxScroll(viewport int) int {
	if n := c.contentRows - viewport; n > 0 {
		return n
	}
	return 0
}
