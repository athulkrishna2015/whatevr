package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/paint"
	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/theme"
	"whattui/internal/view"
)

func (a *App) draw() {
	a.paint()
	a.flushImages()
	a.vx.Render()
}

// paint lays the whole frame into the cell buffer and stops there. Split out
// from draw because it is the half worth measuring and the half a test can
// run: an offscreen window has cells but no terminal to render to.
func (a *App) paint() {
	a.vx.BeginGraphicsFrame()
	// Asked once, before anything is measured: a frame that measures in cells
	// and draws in pixels is a frame with a hole in it.
	a.shaping = a.shaper.Begin()
	// All of these are where the last frame put things, and this is a new one.
	a.blocks = a.blocks[:0]
	a.messages = a.messages[:0]
	a.placements = a.placements[:0]
	a.surfaces = a.surfaces[:0]

	// No clear: the panes tile the screen between them, so clearing first is a
	// write to every cell that every one of them is about to write again.
	win := a.vx.Window()
	l := a.layout()
	a.followChat(max(l.Transcript.Height, 1))

	if a.pairing() {
		a.drawPairing(win)
		a.drawModal(win)
		a.drawToast(win, layout.Layout{})
		return
	}

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
	a.paintSelection()
	a.drawModal(win)
	// Last, so it is over the panel as well. A toast is usually the answer to
	// something pressed on a panel, and an answer behind the question is no
	// answer.
	a.drawToast(win, l)
}

// drawToast is the line that says what just happened, in the top right corner,
// over whatever is there.
//
// A corner rather than a row of its own: it is gone in two seconds, and a strip
// that appears and disappears would move every message on the screen twice for
// every copied line. Over the top right because that is the one part of a chat
// window nothing is ever being read in, and because the eye that just pressed a
// key is at the bottom, where a toast would be in the way of the next thing
// typed.
func (a *App) drawToast(win vaxis.Window, l layout.Layout) {
	msg, refused, life := a.toastNow()
	if msg == "" {
		return
	}
	w, h := win.Size()
	// The words sit in the middle of the card with the same air either side of
	// them, and the air is a pill's own rule: a shape with fully round ends pads
	// its sides to its height rather than to a flat unit.
	room := w - 2*toastPad - 2
	if room < 8 {
		return
	}
	msg = a.clip(msg, room)
	width := a.width(msg) + 2*toastPad

	// The card is taller than the row its words are on, so it claims the row
	// above and the row below as well: half a row of air over the letters, half
	// under them, and the shadow into what is left. Without pixels there is no
	// card to give air to and the toast is the one row it writes in.
	top := 0
	if !l.Header.Empty() {
		// Under the header, which is already saying something about the chat.
		top = l.Header.Row + l.Header.Height
	}
	claimed, textRow := 1, top
	if a.painted() && top+toastRows <= h {
		claimed, textRow = toastRows, top+1
	}
	rect := layout.Rect{Col: w - width - 1, Row: top, Width: width, Height: claimed}
	if rect.Col < 0 || rect.Row+rect.Height > h {
		return
	}

	// It leaves the way a thing that was never part of the page leaves: by
	// going, rather than by being switched off. Everything it is made of mixes
	// toward the ground over the last moment of its life, cells and pixels
	// alike, which is the one piece of motion in the whole application and the
	// only place one belongs.
	fade := 100
	if life < toastFade {
		fade = int(life * 100 / toastFade)
	}
	if fade <= 0 {
		return
	}

	// The edge and the words carry which kind of answer this is. Colour is the
	// whole of it here and that is allowed: there is only ever one toast, it
	// says what happened in words, and the words are the other signal.
	edge := a.theme.Accent
	ink := a.theme.Text
	if refused {
		edge, ink = a.theme.Warning, a.theme.Warning
	}
	ink = a.mixInk(ink, a.theme.Background, fade)

	// A run and a bubble are both images over the cell background, so the toast
	// has to take the ones it covers with it, exactly as a panel does.
	a.occlude(rect)
	a.occludeSurfaces(rect)

	// The ground under the whole claim is the page's own, not the card's: what
	// falls outside a rounded corner has to be the page or the corner is a
	// square. The card's own colour is in the card.
	ground := a.theme.Background
	if claimed == 1 {
		ground = a.mixInk(a.theme.BackgroundPanel, a.theme.Background, fade)
	}
	fill(sub(win, rect), ground)
	a.drawCard(win, rect, edge, fade)
	a.print(sub(win, layout.Rect{Col: rect.Col, Row: textRow, Width: width, Height: 1}),
		toastPad, 0, vaxis.Style{Foreground: ink, Background: ground}, msg)
}

const (
	// toastPad is the air either side of a toast's words, in columns.
	toastPad = 2
	// toastRows is what a toast claims where there are pixels: the row its
	// words are on, a row of air above it, and the room its shadow falls into
	// below.
	toastRows = 3
)

// drawCard is the pill a toast floats on, where there are pixels to draw one
// with. Below that the cells it stands in are the whole of it, and the only
// thing lost is the shape.
func (a *App) drawCard(win vaxis.Window, r layout.Rect, edge vaxis.Color, fade int) {
	if !a.painted() || r.Height < toastRows {
		return
	}
	fill, ok := theme.Paint(a.theme.Background, a.theme.BackgroundPanel, 1)
	if !ok {
		return
	}
	rim, _ := theme.Paint(a.theme.Background, edge, 1)
	_, ch := a.cellPix()

	spec := func(pw, ph int) paint.Spec {
		card := paint.Card{
			W: pw, H: ph,
			// Half a row above the words and half a row below, which puts the
			// middle of the card on the middle of the row they are on.
			Top:  ch / 2,
			Body: 2 * ch,
			Fill: fill, Edge: rim,
			// The same curve every other corner in the application has, and
			// the same one the palette's own frame draws with a box glyph: a
			// corner taken off, not a semicircle. A card as round as it is
			// tall is a lozenge, and a lozenge belongs to a different
			// application than the one behind it.
			Radius: a.bubbleRadius(),
			Shadow: maxInt(ch/3, 3),
			Drop:   maxInt(ch/6, 2),
		}
		if fade < 100 {
			ground := color.NRGBA{a.theme.InkGround[0], a.theme.InkGround[1], a.theme.InkGround[2], 0xff}
			return paint.Fade{Spec: card, Toward: ground, Percent: fade}
		}
		return card
	}
	a.paintRect(sub(win, r), 0, 0, r.Width, r.Height, spec)
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
	w, h := pane.Size()

	a.mu.Lock()
	selected, top, focus, active, hovered := a.selected, a.listTop, a.focus, a.activeChat, a.hovered
	live := a.transport == proto.Ready && a.linkedLocked()
	a.mu.Unlock()

	rail := l.Shape == layout.ShapeRail && !l.ListFocused

	// The rule is the list's own last column, so the two panes read as two
	// panes without the layout having to reserve a third.
	if l.Shape == layout.ShapeWide || l.Shape == layout.ShapeCompact {
		bx := boxFor(a.caps)
		pane.New(w-1, 0, 1, h).Fill(vaxis.Cell{
			Character: vaxis.Character{Grapheme: bx.vertical, Width: 1},
			Style: vaxis.Style{
				Foreground: a.theme.Border, Background: a.theme.BackgroundPanel,
			},
		})
		w--
	}

	// Nothing is drawn off a socket that is gone, and nothing off an account
	// that is. Every row in the list is the daemon's claim about right now: who
	// said what last, how much of it is unread, which order any of it is in.
	// With nothing on the other end those are claims nobody is standing behind,
	// and a list that cannot change is worse than no list, because it looks
	// exactly like one that can. A phone somebody unpaired is the same thing
	// from the other side: the rows are a stranger's now.
	if !live {
		fill(pane.New(0, 0, w, h), a.theme.BackgroundPanel)
		a.drawEmptyList(pane, false)
		return
	}

	perRow := l.ChatRowHeight()
	drawn := 0
	a.chats.Read(func(items []view.Item[*v2.ChatRow], state view.State) {
		if len(items) == 0 {
			fill(pane.New(0, 0, w, h), a.theme.BackgroundPanel)
			a.drawEmptyList(pane, state.Ready)
			return
		}
		for i := 0; ; i++ {
			row := i * perRow
			if row+perRow > h || top+i >= len(items) {
				break
			}
			drawn = row + perRow
			it := items[top+i]
			a.drawChatRow(pane, row, w, perRow, it.Value, rowState{
				selected: top+i == selected && focus == FocusList,
				active:   it.ID == active,
				hovered:  top+i == hovered,
				rail:     rail,
			})
		}
	})
	// Only the ground the rows did not cover, rather than the whole pane
	// before they did.
	if drawn < h {
		fill(pane.New(0, drawn, w, h-drawn), a.theme.BackgroundPanel)
	}
}

func (a *App) drawEmptyList(pane vaxis.Window, ready bool) {
	style := vaxis.Style{Foreground: a.theme.TextMuted, Background: a.theme.BackgroundPanel}
	a.mu.Lock()
	transport, linked := a.transport, a.linkedLocked()
	a.mu.Unlock()
	if transport != proto.Ready {
		a.print(pane, 1, 1, style, "waiting for whatevrd")
		return
	}
	// The same words the notice beside it uses, so the two halves of the screen
	// are saying one thing.
	if !linked {
		a.print(pane, 1, 1, style, "not paired with a phone")
		return
	}
	if !ready {
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

func (a *App) drawChatRow(pane vaxis.Window, row, w, height int, c *v2.ChatRow, st rowState) {
	bg := a.ground(st)
	fill(pane.New(0, row, w, height), bg)
	line := pane.New(0, row, w, 1)

	unread := c.GetUnread() > 0
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

	// A bar in the first column, for the chat the transcript is showing. It
	// runs the whole height of the row: half a bar down the side of a two row
	// entry marks the name rather than the chat.
	if st.active || st.selected {
		pane.New(0, row, 1, height).Fill(vaxis.Cell{
			Character: vaxis.Character{Grapheme: "▎", Width: 1},
			Style:     vaxis.Style{Foreground: a.theme.Accent, Background: bg},
		})
	}

	col := a.drawAvatar(pane, row, height, c, bg) + 1

	badge := ""
	if unread {
		badge = strconv.Itoa(int(c.GetUnread()))
	}

	// The time goes on the name's line and the badge on the preview's, which
	// is where every chat application in the world puts them. With no preview
	// line the badge is worth more than the time, so it takes the slot.
	right, rightStyle := "", vaxis.Style{Foreground: a.theme.TextMuted, Background: bg}
	switch {
	case height < 2 && badge != "":
		right = badge
		rightStyle = vaxis.Style{Foreground: a.theme.Accent, Background: bg, Attribute: vaxis.AttrBold}
	case c.GetLastMs() > 0:
		right = relTime(time.UnixMilli(c.GetLastMs()))
	}
	rightW := a.width(right)
	nameRoom := w - col - rightW - 2
	if nameRoom < 1 {
		nameRoom = 1
	}
	a.print(line.New(col, 0, nameRoom, 1), 0, 0, nameStyle, c.GetName())
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
	room := w - col - badgeW - 1
	if badge != "" {
		// A preview that runs into the badge reads as one word, so the count
		// keeps a column of its own the way the timestamp does.
		room--
	}
	if room < 1 {
		room = 1
	}
	// The preview shares the name's left edge rather than the avatar's, so the
	// two lines of a row line up as one block.
	a.printLine(preview, col, 0, vaxis.Style{Foreground: a.theme.TextMuted, Background: bg},
		a.clipLine(a.linkLine(a.spelled(c.GetPreview().GetText())), room))
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

func (a *App) drawRailRow(line vaxis.Window, c *v2.ChatRow, bg vaxis.Color, unread bool) {
	a.avatar(line, 0, 0, 3, 1, c.GetId(), c.GetName(), bg)
	if unread {
		a.print(line, 3, 0, vaxis.Style{Foreground: a.theme.Accent, Background: bg}, "\u2022")
	}
}

// drawAvatar draws the disc a chat is known by, and answers the column after
// it. A row with the room draws it two rows tall with the initial at twice the
// size, which is the size an avatar is in every chat application there is.
func (a *App) drawAvatar(pane vaxis.Window, row, height int, c *v2.ChatRow, bg vaxis.Color) int {
	width, scale := a.avatarBox(height)
	a.avatar(pane, 1, row, width, scale, c.GetId(), c.GetName(), bg)
	return 1 + width
}

// avatarBox is how many cells a disc takes and how big the initial in it is.
//
// The letter has to land on the middle of the circle, which is an even number
// of columns and two rows at twice the size and an odd number and one row at
// natural size. A terminal with graphics but without OSC 66 scaling gets the
// smaller disc: the alternative is vaxis doing what it correctly does with a
// scale it cannot emit, which is to draw the letter unscaled in the top left
// of the block, and a big circle with a small letter in the corner of it is
// worse than a small circle with the letter in the middle.
func (a *App) avatarBox(rows int) (width, scale int) {
	if rows >= 2 && (a.painted() || a.caps.TextScale) {
		return 4, 2
	}
	return 3, 1
}

// avatar is the disc and the letter in the middle of it. The disc is an even
// number of cells around a scaled initial and an odd number around a plain
// one, for the same reason either way: the letter has to land on the centre
// of the circle rather than beside it.
func (a *App) avatar(win vaxis.Window, col, row, width, rows int, id, name string, bg vaxis.Color) {
	ident := a.theme.IdentityFor(id)
	letter := initial(name)
	if fill, ok := theme.Paint(bg, ident, discMix); ok && a.painted() {
		// The letter is drawn into the disc, not typed on top of it. Then the
		// circle and the thing in the middle of it are one image, centred on
		// each other exactly, at a size that has nothing to do with the cell.
		glyph, key := a.glyph(letter, a.avatarEm(width, rows), ident)
		a.paintRect(win, col, row, width, rows, func(pw, ph int) paint.Spec {
			return paint.Disc{W: pw, H: ph, Fill: fill, Glyph: glyph, GlyphKey: key}
		})
		if glyph != nil {
			return
		}
	}
	// No pixels, or no font yet: the terminal draws the letter. The block is
	// claimed either way, so nothing moves when the font arrives.
	win.New(col+(width-rows)/2, row, rows, rows).PrintScaled(0, vaxis.Segment{
		Text:  letter,
		Style: vaxis.Style{Foreground: ident, Background: bg},
		Size:  vaxis.Scaled(rows, 0),
	})
}

// avatarEm is how big the initial is drawn, in pixels: a little over half the
// circle it sits in, which is where a letter stops being a dot and stops
// touching the edge.
func (a *App) avatarEm(width, rows int) int {
	cw, ch := a.cellPix()
	return minInt(width*cw, rows*ch) * 3 / 5
}

// glyph is one rasterised letter, and the key that identifies it. Cached
// because the spec that carries it is rebuilt on every frame and rasterising
// is not a per-frame price.
func (a *App) glyph(text string, em int, ink vaxis.Color) (*image.NRGBA, string) {
	rgb, ok := theme.Paint(a.theme.Background, ink, 1)
	if !ok || text == "" {
		return nil, ""
	}
	k := glyphKey{text: text, em: em, ink: rgb}
	if img, ok := a.glyphs[k]; ok {
		return img, k.String()
	}
	img := a.shaper.Glyph(text, em, rgb)
	if img == nil {
		return nil, ""
	}
	a.glyphs[k] = img
	return img, k.String()
}

// discMix is how far an avatar's disc is from the row it sits on, toward the
// colour that chat is known by. Far enough to find, near enough that a list of
// them is not a bag of sweets.
const discMix = 0.22

// initial is the letter to stand in for a picture until there are pictures.
func initial(name string) string {
	// Sanitised first, or the letter in the disc is whatever control character
	// the name happened to start with.
	name = strings.TrimSpace(safe(name))
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
	live := a.transport == proto.Ready && a.linkedLocked()
	a.mu.Unlock()

	style := vaxis.Style{
		Foreground: a.theme.Text, Background: a.theme.BackgroundPanel,
		Attribute: vaxis.AttrBold,
	}
	// Our own name while there is no socket and no account, for the same reason
	// the list is empty: the chat this was open on is a chat nothing is reading
	// any more.
	// The name is the one thing on the frame saying a conversation is in front
	// of you, and it says it long after it stopped being true.
	title := "whattui"
	if active != "" && live {
		if row, ok := a.chatRow(active); ok {
			title = row.GetName()
		}
	}
	a.noteBlock(pane, 1, 0, maxInt(a.width(title), 1), 1)
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
	fill(pane, a.theme.Background)
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

	a.noteBlock(pane, left, top, widest, minInt(len(block), h-top))
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
	fill(pane, a.theme.Background)
	w, h := pane.Size()

	a.mu.Lock()
	c := a.conversation
	a.mu.Unlock()

	if c == nil {
		a.print(pane, 2, h/2, vaxis.Style{Foreground: a.theme.TextMuted},
			"pick a chat on the left, or press ctrl+p")
		return
	}

	above := a.topOf(c)
	c.msgs.Read(func(items []view.Item[*v2.MessageRow], state view.State) {
		if len(items) == 0 {
			msg := "loading messages"
			if state.Ready {
				msg = "no messages yet, say something"
			}
			a.print(pane, 2, h/2, vaxis.Style{
				Foreground: a.theme.TextMuted, Background: a.theme.Background,
			}, msg)
			return
		}
		a.refreshTranscript(c, items, state, above, w, h)
		c.keepHeld(h)
		if c.scroll > c.maxScroll(h) {
			c.scroll = c.maxScroll(h)
		}
		scroll := c.scroll

		// Laid out from the bottom up, because the live edge is where the
		// reader already is and a message arriving must not shift what they
		// are reading. The scroll pushes the first message below the pane,
		// and everything above it follows.
		bottom := h + scroll
		for i := 0; i < len(c.runs) && bottom > 0; i++ {
			r := c.runs[i]
			bottom -= r.height
			if bottom < h {
				a.drawRun(pane, c, r, bottom, w)
			}
			bottom--
		}
		a.drawScrollbar(pane, w, h, c.contentRows, scroll)
	})
}

// layoutMessage turns one row into a bubble. Every kind ends up here, and a
// kind whattui does not draw itself renders the daemon's fallback, so the
// transcript is never blank because of a message nobody taught it.
func (a *App) layoutMessage(m *v2.MessageRow, paneWidth int) block {
	quote := ""
	if m.HasReplyTo() {
		quote = m.GetReplyTo().GetText()
	}

	b := a.layoutBlock(quote, a.body(m), a.messageStamp(m), a.runRoom(paneWidth))
	b.muted = m.GetRevoked()
	b.outgoing = m.GetFromMe()
	b.mark, b.status = a.statusMark(m), statusOf(m)

	// A message nobody can read any more carries no reactions: WhatsApp drops
	// them when it goes, and a row of applause under a deleted message is
	// applause for a sentence that is not there.
	if !m.GetRevoked() {
		room := a.runRoom(paneWidth)
		var strip int
		b.reacts, strip = a.pillsFor(reactionGroups(m.GetReactionCounts(), nil), room)
		b.width = minInt(maxInt(b.width, strip), room)
	}

	// A message that is nothing but emoji draws big, the way it does in every
	// other chat client, because the size is what the message means.
	if n := emojiOnlyCount(m.GetText()); n > 0 && !m.GetRevoked() && len(b.body) == 1 &&
		a.caps.TextScale && bigEmojiWanted() {
		// Clamped to the room the column can grow into, not the room it
		// currently occupies: the message is sized by its content, and at
		// this point the content is about to get three times bigger.
		//
		// Clamping at all is not politeness. A terminal discards a multicell
		// character that does not fit, so an unclamped scale is not a big
		// emoji, it is a missing one.
		room := a.runRoom(paneWidth)
		glyphs := lineText(b.body[0])
		b.scale = layout.Clamp(bigEmojiScale(n), a.width(glyphs), room, a.transcriptPage())
		// Never narrower than the reaction strip under it, which is laid out
		// against the same room and is not part of the words.
		b.width = minInt(maxInt(a.width(glyphs)*b.scale, b.width), room)
	}
	return b
}

// messageStamp is when a message was sent. It stands in the gutter beside the
// message rather than at the end of its words, which is what makes a column of
// times down the edge of the transcript readable as a column.
func (a *App) messageStamp(m *v2.MessageRow) string {
	return time.UnixMilli(m.GetTMs()).Format("15:04")
}

// statusMark is how far a message you sent got, and nothing at all for one you
// did not: a message somebody else wrote has no delivery state of yours.
func (a *App) statusMark(m *v2.MessageRow) string {
	if !m.GetFromMe() {
		return ""
	}
	return a.statusGlyph(statusOf(m))
}

// statusGlyph is the mark itself. Every state is its own glyph and not just its
// own colour: one tick sent, two delivered, two heavy read. Colour reinforces
// it rather than carrying it, so the state survives NO_COLOR and a reader who
// cannot tell two of them apart.
//
// A font without dingbats in it draws a tick as a hole, and two holes beside
// two holes say nothing whatever colour they are. The letter every font has
// carries the same count there, and capitals carry the weight the heavy tick
// carries everywhere else.
func (a *App) statusGlyph(status v2.MessageStatus) string {
	if a.caps.PlainFont {
		switch status {
		case v2.MessageStatus_MESSAGE_STATUS_READ, v2.MessageStatus_MESSAGE_STATUS_PLAYED:
			return "VV"
		case v2.MessageStatus_MESSAGE_STATUS_DELIVERED:
			return "vv"
		case v2.MessageStatus_MESSAGE_STATUS_SENT:
			return "v"
		case v2.MessageStatus_MESSAGE_STATUS_FAILED:
			return "!"
		default:
			return "."
		}
	}
	switch status {
	case v2.MessageStatus_MESSAGE_STATUS_READ, v2.MessageStatus_MESSAGE_STATUS_PLAYED:
		return "✔✔"
	case v2.MessageStatus_MESSAGE_STATUS_DELIVERED:
		return "✓✓"
	case v2.MessageStatus_MESSAGE_STATUS_SENT:
		return "✓"
	case v2.MessageStatus_MESSAGE_STATUS_FAILED:
		return "!"
	default:
		return "·"
	}
}

// statusInk is the mark's colour. Read is the accent, which is where every
// chat application puts it, and a send that failed is the one delivery state
// worth interrupting somebody over.
func (a *App) statusInk(status v2.MessageStatus) vaxis.Color {
	switch status {
	case v2.MessageStatus_MESSAGE_STATUS_READ, v2.MessageStatus_MESSAGE_STATUS_PLAYED:
		return a.theme.Accent
	case v2.MessageStatus_MESSAGE_STATUS_FAILED:
		return a.theme.Error
	default:
		return a.theme.TextFaint
	}
}

func (a *App) drawComposer(win vaxis.Window, r layout.Rect) {
	pane := sub(win, r)
	w, h := pane.Size()

	a.mu.Lock()
	active, focus := a.activeChat, a.focus
	readOnly := a.conversation != nil && a.conversation.readOnly
	text, cursor := a.composer.String(), a.composer.cursor
	editing, targetName, targetText := a.composer.editing != "", a.composer.targetName, a.composer.targetText
	targetColour, targeted := a.composer.targetColour, a.composer.targeted()
	a.mu.Unlock()

	// The field carries its own ground where there is something to draw it
	// with, and the cells inside it keep the pane's so the image is not
	// tinted twice. Below the graphics tiers the cells are the field.
	ground := a.theme.BackgroundPanel
	if a.painted() && active != "" {
		ground = a.theme.Background
	}
	fill(pane, ground)
	if active == "" {
		return
	}
	if readOnly {
		// the field goes and the line takes its place, where the marker and
		// the draft would start, so the eye finds it where it looks to type
		a.print(pane, composerText, 0, vaxis.Style{
			Foreground: a.theme.TextMuted, Background: ground, Attribute: vaxis.AttrItalic,
		}, a.clip(readOnlyNote, w-composerGutter))
		return
	}
	a.drawField(pane, 1, 0, w-2, h, focus == FocusComposer)

	// The strip takes the top row of the field, so the draft starts under it.
	top, rows := 0, h
	if targeted {
		a.drawTarget(pane, w, editing, targetName, targetText, targetColour, ground)
		top, rows = 1, h-1
	}
	if rows < 1 {
		return
	}

	marker := vaxis.Style{Foreground: a.theme.Border, Background: ground}
	if focus == FocusComposer {
		marker = vaxis.Style{Foreground: a.theme.Accent, Background: ground}
	}
	a.print(pane, 2, top, marker, "\u203a")

	if text == "" {
		a.print(pane, composerText, top, vaxis.Style{
			Foreground: a.theme.TextFaint, Background: ground,
		}, "type a message")
		if focus == FocusComposer {
			win.ShowCursor(r.Col+composerText, r.Row+top, vaxis.CursorBeam)
		}
		return
	}

	style := vaxis.Style{Foreground: a.theme.Text, Background: ground}
	lines := a.wrap(text, w-composerGutter)
	a.noteBlock(pane, composerText, top, w-composerGutter, rows)
	// The tail is what is being written, so that is the end that stays on
	// screen when the draft outgrows the rows it has.
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for i, line := range lines {
		a.print(pane, composerText, top+i, style, line)
	}

	if focus == FocusComposer {
		col, row := a.cursorCell(text, cursor, w-composerGutter, len(lines), rows)
		win.ShowCursor(r.Col+composerText+col, r.Row+top+row, vaxis.CursorBeam)
	}
}

// drawTarget is the line above a draft that says which message it is about:
// the one being answered, or the one being rewritten. It shares the draft's
// left edge, because the two are one block and a reader should see one.
//
// Nothing else says so. Without it an edit looks exactly like a new message
// until it lands on top of an old one.
func (a *App) drawTarget(pane vaxis.Window, w int, editing bool, name, text string, accent, ground vaxis.Color) {
	mark := "↩"
	if editing {
		mark = "✎"
	}
	if a.caps.Tier <= term.TierPlain {
		mark = ">"
		if editing {
			mark = "*"
		}
	}
	a.print(pane, 2, 0, vaxis.Style{Foreground: accent, Background: ground}, mark)
	col := a.print(pane, composerText, 0, vaxis.Style{
		Foreground: accent, Background: ground, Attribute: vaxis.AttrBold,
	}, a.clip(name, maxInt(w/3, 8)))
	a.print(pane, col+1, 0, vaxis.Style{Foreground: a.theme.TextMuted, Background: ground},
		a.clip(text, maxInt(w-col-3, 1)))
}

// drawField is the rounded box a draft is typed into. A terminal cannot set a
// rounded background, so below the graphics tiers the panel colour fills the
// same cells and the shape is the only thing lost.
func (a *App) drawField(pane vaxis.Window, col, row, width, height int, focused bool) {
	if !a.painted() {
		return
	}
	fill, ok := theme.Paint(a.theme.Background, a.theme.BackgroundPanel, 1)
	if !ok {
		return
	}
	edge, _ := theme.Paint(a.theme.Background, a.theme.Border, 1)
	if focused {
		edge, _ = theme.Paint(a.theme.Background, a.theme.BorderActive, 1)
	}
	_, ch := a.cellPix()
	a.paintRect(pane, col, row, width, height, func(pw, ph int) paint.Spec {
		return paint.Bubble{W: pw, H: ph, Fill: fill, Edge: edge, Radius: ch / 2}
	})
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

// hint is one thing the hint line offers: a registry command, which brings its
// own key with it, or a bare phrase for the gestures no command owns.
type hint struct {
	id   commandID
	text string
}

func (a *App) drawHintBar(win vaxis.Window, r layout.Rect) {
	pane := sub(win, r)
	fill(pane, a.theme.Background)

	a.mu.Lock()
	focus := a.focus
	typed := !a.composer.empty()
	leader := a.leader
	modal := a.modal.kind
	message, pointing := a.selectedMessageLocked()
	readOnly := a.conversation != nil && a.conversation.readOnly
	a.mu.Unlock()

	newline := "s-\u23ce"
	if !a.caps.KittyKeyboard {
		// Without the kitty keyboard protocol the terminal cannot tell
		// shift+enter from enter, so the hint has to name the one that works.
		newline = "^j"
	}

	// An armed leader takes the line and says what it can still become. A
	// prefix nobody can see is a prefix nobody uses.
	if leader {
		a.drawLeaderHints(pane, r.Width)
		return
	}

	var hints []hint
	switch {
	// An open panel owns the keyboard, so the line says what the panel does
	// rather than what the pane behind it would have done.
	case modal != modalNone:
		hints = []hint{{"", "\u2191\u2193 move"}, {"", "\u23ce run"}, {"", "esc close"}}
		if modal == modalForward {
			// The one panel that takes more than one answer has to say so:
			// nothing else on the screen suggests a list can be marked.
			hints = []hint{
				{"", "\u2191\u2193 move"}, {"", "\u21e5 mark"},
				{"", "\u23ce forward"}, {"", "esc close"},
			}
		}
	// A lit message owns the letters, so the line is what those letters do to
	// it. Only the ones that apply: an edit hint over somebody else's message
	// is a key that answers with an excuse. Only in the panes the message is
	// in, because at the chat list those letters are not live.
	case pointing && focus != FocusList:
		star := "star"
		if message.GetStarred() {
			star = "unstar"
		}
		// What the marks in the transcript mean, in words, for the one message
		// they are about. The column and the underline are what you notice; the
		// line is where you find out what you noticed.
		for _, said := range flagWords(message) {
			hints = append(hints, hint{"", said})
		}
		state := a.commandState()
		for _, h := range []hint{
			{cmdReply, "reply"}, {cmdReact, "react"}, {cmdForward, "forward"},
			{cmdEditMessage, "edit"}, {cmdCopyMessage, "copy"},
			{cmdStar, star}, {cmdDelete, "delete"},
			// Last, because what it opens is this same list with room for all
			// of it, which is worth least on the line that already has room.
			{cmdMenu, "more"},
		} {
			if a.enabled(h.id, state) {
				hints = append(hints, h)
			}
		}
		hints = append(hints, hint{"", "esc drop"})
	case focus == FocusList:
		hints = []hint{{cmdOpenChat, "open"}, {"", "\u2191\u2193 move"}, {cmdFocusNext, "chat"}, {cmdQuit, "quit"}}
	case focus == FocusTranscript:
		hints = []hint{{"", "\u2191\u2193 scroll"}, {"", "esc composer"}, {cmdFocusNext, "chats"}}
	case readOnly:
		hints = []hint{{"", "\u2191 scroll back"}, {"", "esc chats"}, {cmdFocusNext, "list"}}
	default:
		hints = []hint{{cmdSend, "send"}, {"", newline + " newline"}, {"", "esc chats"}, {cmdFocusNext, "list"}}
		if !typed {
			hints = []hint{{cmdSend, "send"}, {"", "\u2191 scroll back"}, {"", "esc chats"}, {cmdFocusNext, "list"}}
		}
	}

	w, _ := pane.Size()
	col := 1
	style := vaxis.Style{Foreground: a.theme.TextFaint}
	for i, h := range hints {
		text := a.hintText(h)
		if col+a.width(text) >= w {
			// The line ran out mid-list. What is left of it goes to the menu if
			// the menu fits, because one key that leads to everything still to
			// come beats two words of the next action and silence about the
			// rest.
			if more, ok := a.hintMore(hints[i:]); ok && col+a.width(more) < w {
				a.print(pane, col, 0, style, more)
			}
			break
		}
		col = a.print(pane, col, 0, style, text) + 2
	}
}

// hintText is one hint as it reads on the line: the key the registry gives it,
// then what it does.
func (a *App) hintText(h hint) string {
	if h.id == "" {
		return h.text
	}
	a.initCommands()
	if c := a.commands.byID[h.id]; c != nil && c.Direct != "" {
		return c.Direct + " " + h.text
	}
	return h.text
}

// hintMore is the menu's own hint, if it is among the ones that did not fit.
func (a *App) hintMore(left []hint) (string, bool) {
	for _, h := range left {
		if h.id == cmdMenu {
			return a.hintText(h), true
		}
	}
	return "", false
}

// flagWords says what the transcript's marks mean, for the message the cursor
// is on. Each one leads with the mark it explains, so the line reads as a
// legend rather than as another key to press.
func flagWords(m *v2.MessageRow) []string {
	if m.GetRevoked() {
		return nil
	}
	var out []string
	if m.GetStarred() {
		out = append(out, "★ starred")
	}
	if m.GetEdited() {
		out = append(out, "edited")
	}
	return out
}

// drawLeaderHints projects the registry's leader bindings onto the hint line,
// so ctrl+x is a menu rather than a thing you had to read about.
func (a *App) drawLeaderHints(pane vaxis.Window, width int) {
	a.initCommands()
	col := a.print(pane, 1, 0, vaxis.Style{Foreground: a.theme.Accent, Attribute: vaxis.AttrBold}, "^x")
	col += 2
	state := a.commandState()
	for _, c := range a.commands.ordered {
		if c.Leader == "" {
			continue
		}
		style := vaxis.Style{Foreground: a.theme.TextFaint}
		if c.Enabled != nil {
			if ok, _ := c.Enabled(state); !ok {
				continue
			}
		}
		h := c.Leader + " " + strings.ToLower(c.Title)
		if col+a.width(h) >= width {
			break
		}
		col = a.print(pane, col, 0, style, h)
		col += 2
	}
}
