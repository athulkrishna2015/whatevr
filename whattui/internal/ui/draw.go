package ui

import (
	"fmt"
	"strings"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/proto"
	"whattui/internal/view"
)

func (a *App) draw() {
	win := a.vx.Window()
	win.Clear()
	l := a.layout()

	a.drawChatList(win, l)
	if !l.Header.Empty() {
		a.drawHeader(win, l.Header)
	}
	if !l.Transcript.Empty() {
		a.drawTranscript(win, l.Transcript)
	}
	if !l.Composer.Empty() {
		a.drawComposer(win, l.Composer)
	}
	if !l.HintBar.Empty() {
		a.drawHintBar(win, l.HintBar)
	}
	a.vx.Render()
}

// sub is a vaxis window for a layout rect.
func sub(win vaxis.Window, r layout.Rect) vaxis.Window {
	return win.New(r.Col, r.Row, r.Width, r.Height)
}

// print writes a string clipped to the window width, from one column, and
// returns the column after it. Every pane goes through this, which is how a
// column of glyphs keeps one left edge.
//
// The advance is measured with the terminal's own width rules rather than by
// counting runes: a chat name full of emoji is not as many cells wide as it is
// long, and guessing is how a column loses its edge.
func (a *App) print(win vaxis.Window, col, row int, style vaxis.Style, s string) int {
	w, _ := win.Size()
	if row < 0 || col >= w || s == "" {
		return col
	}
	win.New(col, row, w-col, 1).PrintTruncate(0, vaxis.Segment{Text: s, Style: style})
	advance := a.vx.RenderedWidth(s)
	if col+advance > w {
		return w
	}
	return col + advance
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
	selected, top, focus, active := a.selected, a.listTop, a.focus, a.activeChat
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

	a.chats.Read(func(items []view.Item[proto.ChatRow]) {
		if len(items) == 0 {
			a.drawEmptyList(pane)
			return
		}
		perRow := 1
		if l.Shape == layout.ShapeWide {
			// Wide has the room for what a chat row is actually for: who it
			// is, and what they last said.
			perRow = 2
		}
		for i := 0; ; i++ {
			row := i * perRow
			if row+perRow > h || top+i >= len(items) {
				break
			}
			it := items[top+i]
			isSel := top+i == selected && focus == FocusList
			isActive := it.ID == active
			a.drawChatRow(pane, row, w, perRow, it.Value, isSel, isActive, rail)
		}
	})
}

func (a *App) drawEmptyList(pane vaxis.Window) {
	style := vaxis.Style{Foreground: a.theme.TextMuted, Background: a.theme.BackgroundPanel}
	if !a.chats.IsReady() {
		a.print(pane, 1, 1, style, "loading chats")
		return
	}
	a.print(pane, 1, 1, style, "no chats yet")
}

func (a *App) drawChatRow(pane vaxis.Window, row, w, height int, c proto.ChatRow, selected, active, rail bool) {
	bg := a.theme.BackgroundPanel
	switch {
	case selected:
		bg = a.theme.BackgroundActive
	case active:
		bg = a.theme.BackgroundActive
	}
	block := pane.New(0, row, w, height)
	fill(block, bg)
	line := pane.New(0, row, w, 1)

	unread := c.Unread > 0
	nameStyle := vaxis.Style{Foreground: a.theme.Text, Background: bg}
	if unread {
		// Weight as well as colour: at the plain tier and for anyone who
		// cannot tell the accent from the text, bold is what carries it.
		nameStyle.Attribute |= vaxis.AttrBold
	}

	if rail {
		a.drawRailRow(line, c, bg, unread)
		return
	}

	col := 1
	col = a.print(line, col, 0, vaxis.Style{Foreground: a.theme.IdentityFor(c.ID), Background: bg}, avatarGlyph(c))
	col++

	badge := ""
	if unread {
		badge = fmt.Sprintf(" %d", c.Unread)
	}
	nameRoom := w - col - a.vx.RenderedWidth(badge) - 1
	if nameRoom < 1 {
		nameRoom = 1
	}
	a.print(line.New(col, 0, nameRoom, 1), 0, 0, nameStyle, c.Name)

	if badge != "" {
		a.print(line, w-a.vx.RenderedWidth(badge)-1, 0, vaxis.Style{
			Foreground: a.theme.Accent, Background: bg, Attribute: vaxis.AttrBold,
		}, badge)
	}

	if height < 2 {
		return
	}
	// The preview shares the name's left edge rather than the avatar's, so the
	// two lines of a row line up as one block.
	preview := pane.New(0, row+1, w, 1)
	style := vaxis.Style{Foreground: a.theme.TextMuted, Background: bg}
	if unread {
		style.Foreground = a.theme.Text
	}
	a.print(preview, 3, 0, style, a.clip(c.Preview, maxInt(w-4, 1)))
}

func (a *App) drawRailRow(line vaxis.Window, c proto.ChatRow, bg vaxis.Color, unread bool) {
	a.print(line, 1, 0, vaxis.Style{Foreground: a.theme.IdentityFor(c.ID), Background: bg}, avatarGlyph(c))
	if unread {
		a.print(line, 3, 0, vaxis.Style{Foreground: a.theme.Accent, Background: bg}, "•")
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
	return a.layoutBubble(header, quote, m.Body(), a.messageFooter(m), max, headerStyle, m.Outgoing())
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

	a.mu.Lock()
	active, focus := a.activeChat, a.focus
	a.mu.Unlock()

	if active == "" {
		return
	}
	style := vaxis.Style{Foreground: a.theme.TextMuted, Background: a.theme.BackgroundPanel}
	marker := vaxis.Style{Foreground: a.theme.Border, Background: a.theme.BackgroundPanel}
	if focus == FocusComposer {
		marker = vaxis.Style{Foreground: a.theme.Accent, Background: a.theme.BackgroundPanel}
	}
	a.print(pane, 1, 0, marker, "› ")
	a.print(pane, 3, 0, style, "type a message")
}

func (a *App) drawHintBar(win vaxis.Window, r layout.Rect) {
	pane := sub(win, r)
	fill(pane, a.theme.Background)

	a.mu.Lock()
	focus := a.focus
	a.mu.Unlock()

	var hints []string
	switch focus {
	case FocusList:
		hints = []string{"⏎ open", "↑↓ move", "^p palette", "? help", "^c quit"}
	case FocusTranscript:
		hints = []string{"↑↓ select", "esc back", "^p palette", "? help"}
	default:
		hints = []string{"⏎ send", "esc chats", "^p palette", "? help"}
	}

	col := 1
	for _, h := range hints {
		col = a.print(pane, col, 0, vaxis.Style{Foreground: a.theme.TextMuted}, h)
		col += 2
	}
}

// wrap breaks text to a cell width, on spaces where it can and mid-word where
// it must, so a long url never runs off the pane. Widths are the terminal's,
// not rune counts.
func (a *App) wrap(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		cur := ""
		for _, word := range strings.Fields(para) {
			switch {
			case cur == "":
				cur = word
			case a.width(cur)+1+a.width(word) <= width:
				cur += " " + word
			default:
				out = append(out, cur)
				cur = word
			}
			for a.width(cur) > width {
				head := a.clip(cur, width)
				if head == "" {
					break
				}
				out = append(out, head)
				cur = cur[len(head):]
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}
