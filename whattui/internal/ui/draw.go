package ui

import (
	"strconv"
	"strings"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/proto"
	"whattui/internal/view"
)

func (a *App) draw() {
	// Asked once, before anything is measured: a frame that measures in cells
	// and draws in pixels is a frame with a hole in it.
	a.shaping = a.shaper.Begin()

	win := a.vx.Window()
	win.Clear()
	l := a.layout()

	a.drawChatList(win, l)
	if !l.Header.Empty() {
		a.drawHeader(win, l.Header)
	}
	if !l.Transcript.Empty() {
		if title, colour, body, ok := a.notice(); ok {
			a.drawNotice(sub(win, l.Transcript), title, colour, body)
		} else {
			a.drawTranscript(win, l.Transcript)
		}
	}
	if !l.Composer.Empty() {
		a.drawComposer(win, l.Composer)
	}
	if !l.HintBar.Empty() {
		a.drawHintBar(win, l.HintBar)
	}
	a.flushRuns()
	a.vx.Render()
}

// sub is a vaxis window for a layout rect.
func sub(win vaxis.Window, r layout.Rect) vaxis.Window {
	return win.New(r.Col, r.Row, r.Width, r.Height)
}

// fill paints a rectangle with a background.
func fill(win vaxis.Window, bg vaxis.Color) {
	win.Fill(vaxis.Cell{
		Character: vaxis.Character{Grapheme: " ", Width: 1},
		Style:     vaxis.Style{Background: bg},
	})
}

func (a *App) drawChatList(win vaxis.Window, l layout.Layout) {
	if l.ChatList.Empty() {
		return
	}
	pane := sub(win, l.ChatList)
	fill(pane, a.theme.BackgroundPanel)
	w, h := pane.Size()

	a.mu.Lock()
	selected, top, focus, active, hovered := a.selected, a.listTop, a.focus, a.activeChat, a.hovered
	a.mu.Unlock()

	rail := l.Shape == layout.ShapeRail && !l.ListFocused

	// The rule is the list's own last column, so the two panes read as two
	// panes without the layout having to reserve a third.
	if l.Shape == layout.ShapeWide || l.Shape == layout.ShapeCompact {
		bx := boxFor(a.caps)
		for row := 0; row < h; row++ {
			a.print(pane, w-1, row, vaxis.Style{
				Foreground: a.theme.Border, Background: a.theme.BackgroundPanel,
			}, bx.vertical)
		}
		w--
	}

	perRow := l.ChatRowHeight()
	a.chats.Read(func(items []view.Item[proto.ChatRow]) {
		if len(items) == 0 {
			a.drawEmptyList(pane)
			return
		}
		for i := 0; ; i++ {
			row := i * perRow
			if row+perRow > h || top+i >= len(items) {
				break
			}
			it := items[top+i]
			a.drawChatRow(pane, row, w, perRow, it.Value, rowState{
				selected: top+i == selected && focus == FocusList,
				active:   it.ID == active,
				hovered:  top+i == hovered,
				rail:     rail,
			})
		}
	})
}

func (a *App) drawEmptyList(pane vaxis.Window) {
	style := vaxis.Style{Foreground: a.theme.TextMuted, Background: a.theme.BackgroundPanel}
	a.mu.Lock()
	transport := a.transport
	a.mu.Unlock()
	if transport != proto.Ready {
		a.print(pane, 1, 1, style, "waiting for whatevrd")
		return
	}
	if !a.chats.IsReady() {
		a.print(pane, 1, 1, style, "loading chats")
		return
	}
	a.print(pane, 1, 1, style, "no chats yet")
}

// rowState is everything about a chat row that is not the chat.
type rowState struct {
	selected, active, hovered, rail bool
}

// ground is the row's background. Three states, three steps up the same ramp,
// in the order of how much they mean: where the keyboard is beats where the
// pointer is beats which chat is open.
func (a *App) ground(st rowState) vaxis.Color {
	switch {
	case st.selected:
		return a.theme.BackgroundActive
	case st.hovered:
		return a.theme.BackgroundHover
	case st.active:
		return a.theme.BackgroundHover
	default:
		return a.theme.BackgroundPanel
	}
}

func (a *App) drawChatRow(pane vaxis.Window, row, w, height int, c proto.ChatRow, st rowState) {
	bg := a.ground(st)
	fill(pane.New(0, row, w, height), bg)
	line := pane.New(0, row, w, 1)

	unread := c.Unread > 0
	nameStyle := vaxis.Style{Foreground: a.theme.Text, Background: bg}
	if unread {
		// Weight as well as colour: at the plain tier and for anyone who
		// cannot tell the accent from the text, bold is what carries it.
		nameStyle.Attribute |= vaxis.AttrBold
	}

	if st.rail {
		a.drawRailRow(line, c, bg, unread)
		return
	}

	// A bar in the first column, in the chat's own colour, for the chat the
	// transcript is showing. Position rather than hue, so it survives a
	// terminal that cannot show the hue.
	if st.active || st.selected {
		a.print(line, 0, 0, vaxis.Style{Foreground: a.theme.Accent, Background: bg}, "▎")
	}

	col := 1
	col = a.print(line, col, 0, vaxis.Style{Foreground: a.theme.IdentityFor(c.ID), Background: bg}, avatarGlyph(c))
	col++

	badge := ""
	if unread {
		badge = strconv.Itoa(int(c.Unread))
	}

	// The time goes on the name's line and the badge on the preview's, which
	// is where every chat application in the world puts them. With no preview
	// line the badge is worth more than the time, so it takes the slot.
	right, rightStyle := "", vaxis.Style{Foreground: a.theme.TextFaint, Background: bg}
	switch {
	case height < 2 && badge != "":
		right = badge
		rightStyle = vaxis.Style{Foreground: a.theme.Accent, Background: bg, Attribute: vaxis.AttrBold}
	case c.LastMessageTime > 0:
		right = relTime(time.Unix(c.LastMessageTime, 0))
	}
	rightW := a.width(right)
	nameRoom := w - col - rightW - 2
	if nameRoom < 1 {
		nameRoom = 1
	}
	a.print(line.New(col, 0, nameRoom, 1), 0, 0, nameStyle, c.Name)
	if right != "" && w-rightW-1 > col {
		a.print(line, w-rightW-1, 0, rightStyle, right)
	}

	if height < 2 {
		return
	}
	// The preview is always the muted step of the ramp, unread or not.
	// Brightening it for unread looked like two kinds of row rather than one
	// kind in two states, and a preview that opens with an emoji does not
	// dim anyway, so the contrast it was supposed to carry was never there.
	// Unread is the bold name and the badge, both of which are reliable.
	preview := pane.New(0, row+1, w, 1)
	badgeW := a.width(badge)
	room := w - 3 - badgeW - 1
	if room < 1 {
		room = 1
	}
	// The preview shares the name's left edge rather than the avatar's, so the
	// two lines of a row line up as one block.
	a.printLine(preview, 3, 0, vaxis.Style{Foreground: a.theme.TextMuted, Background: bg},
		a.clipLine(a.linkLine(c.Preview), room))
	if badge != "" {
		a.print(preview, w-badgeW-1, 0, vaxis.Style{
			Foreground: a.theme.Accent, Background: bg, Attribute: vaxis.AttrBold,
		}, badge)
	}
}

// relTime is the short form a chat list uses: a time today, a weekday this
// week, a date before that.
func relTime(t time.Time) string {
	now := time.Now()
	switch d := now.Sub(t); {
	case d < 0 || d < 12*time.Hour && t.Day() == now.Day():
		return t.Format("15:04")
	case d < 6*24*time.Hour:
		return t.Format("Mon")
	default:
		return t.Format("02/01")
	}
}

func (a *App) drawRailRow(line vaxis.Window, c proto.ChatRow, bg vaxis.Color, unread bool) {
	a.print(line, 1, 0, vaxis.Style{Foreground: a.theme.IdentityFor(c.ID), Background: bg}, avatarGlyph(c))
	if unread {
		a.print(line, 3, 0, vaxis.Style{Foreground: a.theme.Accent, Background: bg}, "\u2022")
	}
}

// avatarGlyph is the initial to stand in for a picture until the picture is
// drawn. Groups and people read differently at a glance.
func avatarGlyph(c proto.ChatRow) string {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return "?"
	}
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func (a *App) drawHeader(win vaxis.Window, r layout.Rect) {
	pane := sub(win, r)
	fill(pane, a.theme.BackgroundPanel)

	a.mu.Lock()
	active := a.activeChat
	a.mu.Unlock()

	style := vaxis.Style{
		Foreground: a.theme.Text, Background: a.theme.BackgroundPanel,
		Attribute: vaxis.AttrBold,
	}
	title := "whattui"
	if active != "" {
		if it, ok := a.chats.Get(active); ok {
			title = it.Value.Name
		}
	}
	a.print(pane, 1, 0, style, title)

	if msg, colour, show := a.status(); show {
		w, _ := pane.Size()
		if len(msg) < w-2 {
			a.print(pane, w-len(msg)-1, 0, vaxis.Style{
				Foreground: colour, Background: a.theme.BackgroundPanel,
			}, msg)
		}
	}
}

// drawNotice is the centred panel a reader gets instead of an empty pane: what
// is wrong, and the line that fixes it. Left-aligned as a block and centred as
// a block, so the command in it is still a command somebody can read.
func (a *App) drawNotice(pane vaxis.Window, title string, colour vaxis.Color, body []string) {
	w, h := pane.Size()
	block := make([]string, 0, len(body)+2)
	block = append(block, title, "")
	block = append(block, body...)

	widest := 0
	for _, line := range block {
		widest = maxInt(widest, a.width(line))
	}
	left := maxInt((w-widest)/2, 1)
	top := maxInt((h-len(block))/2, 0)

	for i, line := range block {
		if top+i >= h {
			break
		}
		style := vaxis.Style{Foreground: a.theme.TextMuted}
		if i == 0 {
			style = vaxis.Style{Foreground: colour, Attribute: vaxis.AttrBold}
		} else if strings.HasPrefix(line, "  ") {
			// A command is the one thing on the panel worth the reader's
			// full attention, so it gets the full-strength ink.
			style = vaxis.Style{Foreground: a.theme.Text}
		}
		a.print(pane, left, top+i, style, a.clip(line, w-left))
	}
}

func (a *App) drawTranscript(win vaxis.Window, r layout.Rect) {
	pane := sub(win, r)
	w, h := pane.Size()

	a.mu.Lock()
	c := a.conversation
	a.mu.Unlock()

	if c == nil {
		a.print(pane, 2, h/2, vaxis.Style{Foreground: a.theme.TextMuted},
			"pick a chat on the left, or press ctrl+p")
		return
	}

	c.msgs.Read(func(items []view.Item[proto.MessageRow]) {
		if len(items) == 0 {
			msg := "loading messages"
			if c.msgs.IsReady() {
				msg = "no messages yet, say something"
			}
			a.print(pane, 2, h/2, vaxis.Style{Foreground: a.theme.TextMuted}, msg)
			return
		}

		// Laid out from the bottom up, because the live edge is where the
		// reader already is and a message arriving must not shift what they
		// are reading.
		bottom := h
		for i := c.scroll; i < len(items) && bottom > 0; i++ {
			m := items[i].Value

			if m.Centred() {
				lines := a.wrap(m.Body(), w-4)
				bottom -= len(lines)
				style := vaxis.Style{Foreground: a.theme.TextMuted, Attribute: vaxis.AttrItalic}
				for j, line := range lines {
					indent := (w - a.vx.RenderedWidth(line)) / 2
					if indent < 0 {
						indent = 0
					}
					a.print(pane, indent, bottom+j, style, line)
				}
				bottom--
				continue
			}

			b := a.layoutMessage(m, w)
			bottom -= a.bubbleHeight(b)
			col := 2
			if m.Outgoing() {
				col = w - b.Width() - 2
				if col < 2 {
					col = 2
				}
			}
			a.drawBubble(pane, b, col, bottom)
			bottom--
		}
	})
}

// layoutMessage turns one row into a bubble. Every kind ends up here, and a
// kind whattui does not draw itself renders the daemon's fallback, so the
// transcript is never blank because of a message nobody taught it.
func (a *App) layoutMessage(m proto.MessageRow, paneWidth int) bubble {
	header := ""
	headerStyle := vaxis.Style{}
	// Who is talking is the first thing you need in a group, and it is the
	// main place colour earns its keep.
	if !m.Outgoing() && m.Sender.Name != "" {
		header = m.Sender.Name
		headerStyle = vaxis.Style{
			Foreground: a.theme.IdentityFor(m.Sender.ID),
			Attribute:  vaxis.AttrBold,
		}
	}

	quote := ""
	if m.ReplyTo != nil {
		quote = m.ReplyTo.Text
		if quote == "" {
			quote = m.ReplyTo.Fallback
		}
	}

	max := paneWidth * 3 / 4
	if max < 24 {
		max = paneWidth - 4
	}
	b := a.layoutBubble(header, quote, m.Body(), a.messageFooter(m), max, headerStyle, m.Outgoing())

	// A message that is nothing but emoji draws big, the way it does in every
	// other chat client, because the size is what the message means.
	if n := emojiOnlyCount(m.Text); n > 0 && !m.Revoked && len(b.body) == 1 {
		// Clamped to the room the bubble can grow into, not the room it
		// currently occupies: the bubble is sized by its content, and at this
		// point the content is about to get three times bigger.
		//
		// Clamping at all is not politeness. A terminal discards a multicell
		// character that does not fit, so an unclamped scale is not a big
		// emoji, it is a missing one.
		room := max - 2*bubblePadX - 2
		glyphs := lineText(b.body[0])
		b.scale = layout.Clamp(bigEmojiScale(n), a.width(glyphs), room, a.transcriptPage())
		b.inner = minInt(maxInt(b.inner, a.width(glyphs)*b.scale), room)
		if !a.footerFitsInline(b) {
			b.inner = minInt(maxInt(b.inner, a.width(b.footer)), room)
		}
	}
	return b
}

// messageFooter is the time and the delivery state.
//
// Every state is its own glyph, not just its own colour: one tick sent, two
// delivered, two filled read. Colour reinforces it rather than carrying it, so
// the state survives NO_COLOR, a colour-blind reader, and the plain tier.
func (a *App) messageFooter(m proto.MessageRow) string {
	stamp := time.Unix(m.Timestamp, 0).Format("15:04")
	if m.Edited {
		stamp += " edited"
	}
	if !m.Outgoing() {
		return stamp
	}
	return stamp + " " + statusGlyph(m.Status)
}

func statusGlyph(status string) string {
	switch status {
	case "read":
		return "✔✔"
	case "delivered":
		return "✓✓"
	case "sent":
		return "✓"
	case "failed":
		return "!"
	default:
		return "·"
	}
}

func (a *App) drawComposer(win vaxis.Window, r layout.Rect) {
	pane := sub(win, r)
	fill(pane, a.theme.BackgroundPanel)
	w, h := pane.Size()

	a.mu.Lock()
	active, focus := a.activeChat, a.focus
	text, cursor, sendErr := a.composer.String(), a.composer.cursor, a.composer.sendErr
	a.mu.Unlock()

	if active == "" {
		return
	}

	marker := vaxis.Style{Foreground: a.theme.Border, Background: a.theme.BackgroundPanel}
	if focus == FocusComposer {
		marker = vaxis.Style{Foreground: a.theme.Accent, Background: a.theme.BackgroundPanel}
	}
	a.print(pane, 1, 0, marker, "\u203a")

	if sendErr != "" {
		a.print(pane, 3, 0, vaxis.Style{
			Foreground: a.theme.Error, Background: a.theme.BackgroundPanel,
		}, a.clip("not sent: "+sendErr, w-4))
		return
	}

	if text == "" {
		a.print(pane, 3, 0, vaxis.Style{
			Foreground: a.theme.TextFaint, Background: a.theme.BackgroundPanel,
		}, "type a message")
		if focus == FocusComposer {
			win.ShowCursor(r.Col+3, r.Row, vaxis.CursorBeam)
		}
		return
	}

	style := vaxis.Style{Foreground: a.theme.Text, Background: a.theme.BackgroundPanel}
	lines := a.wrap(text, w-composerGutter)
	// The tail is what is being written, so that is the end that stays on
	// screen when the draft outgrows the rows it has.
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	for i, line := range lines {
		a.print(pane, 3, i, style, line)
	}

	if focus == FocusComposer {
		col, row := a.cursorCell(text, cursor, w-composerGutter, len(lines), h)
		win.ShowCursor(r.Col+3+col, r.Row+row, vaxis.CursorBeam)
	}
}

// cursorCell is where the caret sits once the draft has been wrapped: the
// same wrap the drawing used, walked until it has passed as many runes as the
// caret is after.
func (a *App) cursorCell(text string, at, width, shown, height int) (int, int) {
	if width < 1 {
		return 0, 0
	}
	head := string([]rune(text)[:at])
	lines := a.wrap(head, width)
	if len(lines) == 0 {
		return 0, 0
	}
	// A caret just past a space that wrapping ate belongs at the end of the
	// text before it, which is exactly where the wrapped head puts it.
	row := len(lines) - 1
	col := a.width(lines[row])
	if strings.HasSuffix(head, " ") && col < width {
		col++
	}
	if row >= shown {
		row = shown - 1
	}
	if off := shown - height; off > 0 {
		row -= off
	}
	if row < 0 {
		row = 0
	}
	return minInt(col, width-1), row
}

func (a *App) drawHintBar(win vaxis.Window, r layout.Rect) {
	pane := sub(win, r)
	fill(pane, a.theme.Background)

	a.mu.Lock()
	focus := a.focus
	typed := !a.composer.empty()
	a.mu.Unlock()

	newline := "s-\u23ce"
	if !a.caps.KittyKeyboard {
		// Without the kitty keyboard protocol the terminal cannot tell
		// shift+enter from enter, so the hint has to name the one that works.
		newline = "^j"
	}

	var hints []string
	switch focus {
	case FocusList:
		hints = []string{"\u23ce open", "\u2191\u2193 move", "tab chat", "^q quit"}
	case FocusTranscript:
		hints = []string{"\u2191\u2193 scroll", "esc composer", "tab chats"}
	default:
		hints = []string{"\u23ce send", newline + " newline", "esc chats", "tab list"}
		if !typed {
			hints = []string{"\u23ce send", "\u2191 scroll back", "esc chats", "tab list"}
		}
	}

	w, _ := pane.Size()
	col := 1
	for _, h := range hints {
		if col+a.width(h) >= w {
			break
		}
		col = a.print(pane, col, 0, vaxis.Style{Foreground: a.theme.TextFaint}, h)
		col += 2
	}
}
