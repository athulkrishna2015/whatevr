package ui

import (
	"image/color"
	"strings"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/paint"
	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/view"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalPalette
	modalSlash
	modalHelp
	// modalConfirm is a question with its answers under it, and no line to
	// type on: the choices are the whole panel.
	modalConfirm
	// modalReact picks the emoji that goes on a message, starting with the ones
	// already there.
	modalReact
	// modalForward picks the chats a message is sent on to. The one panel in
	// the application that takes more than one answer.
	modalForward
	// modalMenu is the context menu: the actions that apply to one message,
	// where the pointer asked for them.
	modalMenu
)

type modalChoice struct {
	Command  commandID
	ChatID   string
	Emoji    string
	Label    string
	Detail   string
	Disabled string
	// Mine marks the reaction this account already put on the message, which is
	// the row that takes it off again.
	Mine bool
}

type modalState struct {
	kind modalKind
	// prompt is what a confirmation asks about, in place of the line the other
	// panels are typed into.
	prompt string
	// message is what the panel is about, for the ones opened on the message
	// the cursor is on. Held rather than asked for again, so the panel acts on
	// the message it was opened for whatever has happened to the cursor since.
	message string
	// marked is the chats a forward has been pointed at, by id, and it is keyed
	// rather than indexed because the list under it is re-filtered on every
	// keystroke.
	marked map[string]bool
	// at is where a context menu was asked for, in screen cells.
	at       point
	query    []rune
	cursor   int
	selector selector[modalChoice]
	rect     layout.Rect
	// list is where the results actually are: the rows inside the frame, not
	// the panel. A pointer anywhere else is over the panel, not over a row.
	list    layout.Rect
	visible int
	request uint64
}

func (a *App) openModal(kind modalKind) {
	a.initCommands()
	a.mu.Lock()
	a.modal = modalState{kind: kind}
	search, generation, issue := a.refreshModalLocked()
	a.mu.Unlock()
	if issue {
		a.searchChats(search, generation)
	}
}

// openMessageModal opens a panel about one message, at a place on the screen.
// The message is captured here: everything the panel does afterwards is about
// the message it was opened for, not about wherever the cursor has got to.
func (a *App) openMessageModal(kind modalKind, at point) {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	a.initCommands()
	a.mu.Lock()
	a.modal = modalState{kind: kind, message: m.GetId(), at: at, marked: map[string]bool{}}
	search, generation, issue := a.refreshModalLocked()
	a.mu.Unlock()
	if issue {
		a.searchChats(search, generation)
	}
}

// modalPage is how many rows of results a panel shows when what it holds could
// be anything.
const modalPage = 10

// dismissModalLocked closes whatever is open without acting on it.
func (a *App) dismissModalLocked() { a.modal = modalState{} }

func (a *App) closeModal() {
	a.mu.Lock()
	a.modal = modalState{}
	a.mu.Unlock()
}

func (a *App) refreshModalLocked() (string, uint64, bool) {
	query := string(a.modal.query)
	switch a.modal.kind {
	case modalPalette:
		a.searchRequest++
		a.modal.request = a.searchRequest
		if strings.HasPrefix(query, "/") {
			a.modal.selector.Set(nil)
			return strings.TrimPrefix(query, "/"), a.modal.request, true
		} else {
			a.modal.selector.Set(a.commandChoicesFor(query, false, false, a.commandStateLocked()))
		}
	case modalSlash:
		a.modal.selector.Set(a.commandChoicesFor(query, true, false, a.commandStateLocked()))
	case modalHelp:
		a.modal.selector.Set(a.commandChoicesFor(query, false, true, a.commandStateLocked()))
	case modalMenu:
		// The menu is the registry, filtered to the message it was opened on.
		// It is not typed into, so it is built once and never refiltered.
		a.modal.selector.Set(a.messageChoicesLocked())
	case modalReact:
		message, _ := a.selectedMessageLocked()
		a.modal.selector.Set(a.reactChoices(message, query))
	case modalForward:
		// The chat list until somebody types, and the daemon's search after
		// that. Both are the same call: a frontend that filtered its own window
		// would be offering the fifty chats it happens to hold as though they
		// were all of them.
		a.searchRequest++
		a.modal.request = a.searchRequest
		a.modal.selector.Set(nil)
		return query, a.modal.request, true
	}
	return "", 0, false
}

// chatChoices is the chat list as a panel reads it, in the order the daemon
// sorted it. Asked outside App.mu, which is where a collection is always
// asked anything.
func (a *App) chatChoices() []modalChoice {
	var out []modalChoice
	a.chats.Read(func(items []view.Item[*v2.ChatRow], _ view.State) {
		out = make([]modalChoice, 0, len(items))
		for _, it := range items {
			out = append(out, modalChoice{ChatID: it.ID, Label: it.Value.GetName(), Detail: a.spelled(it.Value.GetPreview().GetText())})
		}
	})
	return out
}

// searchChats fills a panel with chats: the ones we already hold while there is
// nothing to search for, and the daemon's answer once there is. Nothing typed
// is not a search for nothing, it is the list, and the list is already here.
func (a *App) searchChats(query string, generation uint64) {
	if strings.TrimSpace(query) == "" {
		a.showChats(a.chatChoices(), generation)
		return
	}
	request := a.request
	if request == nil {
		return
	}
	req := &v2.Request{}
	req.SetSearchChats(v2.SearchChats_builder{Query: query, Limit: 50}.Build())
	request(req, func(resp *v2.Response, err *proto.Error) {
		if err != nil {
			a.refuse(err.Message)
			return
		}
		chats := resp.GetSearchChats().GetChats()
		choices := make([]modalChoice, 0, len(chats))
		for _, chat := range chats {
			choices = append(choices, modalChoice{ChatID: chat.GetId(), Label: chat.GetName(), Detail: a.spelled(chat.GetPreview().GetText())})
		}
		a.showChats(choices, generation)
		a.vx.PostEvent(redraw{})
	})
}

// showChats puts an answer in the panel that asked for it, and nowhere else: a
// search that lands after the panel moved on is an answer to a question nobody
// is asking any more.
func (a *App) showChats(choices []modalChoice, generation uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if generation == a.modal.request {
		a.modal.selector.Set(choices)
	}
}

// chooseLocked resolves what choosing a row means and answers the work to do
// once the lock is gone, with the panel already closed. Both the keyboard and
// the pointer come through here, so the two can never disagree about what a row
// does.
func (a *App) chooseLocked(choice modalChoice) func() {
	kind, message := a.modal.kind, a.modal.message
	marked := make([]string, 0, len(a.modal.marked))
	for id := range a.modal.marked {
		marked = append(marked, id)
	}
	slash := kind == modalSlash
	a.modal = modalState{}
	if slash {
		// The slash that opened the menu is still in the draft, and what it
		// opened has just happened.
		a.composer.clear()
	}

	switch kind {
	case modalReact:
		emoji := choice.Emoji
		if choice.Mine {
			// Pressing the reaction you already put there takes it back, the
			// same gesture as pressing it in every other client.
			emoji = ""
		}
		return func() { a.react(message, emoji) }
	case modalForward:
		if len(marked) == 0 && choice.ChatID != "" {
			marked = []string{choice.ChatID}
		}
		return func() { a.forward(message, marked) }
	}
	if choice.ChatID != "" {
		return func() { a.openChat(choice.ChatID) }
	}
	return func() { a.execute(choice.Command) }
}

// menuKeyLocked resolves a keystroke against the actions a menu is showing.
func (a *App) menuKeyLocked(k vaxis.Key) (commandID, bool) {
	if k.Text == "" {
		return "", false
	}
	for _, choice := range a.modal.selector.items {
		if c := a.commands.byID[choice.Command]; c != nil && c.Direct == k.Text {
			return c.ID, true
		}
	}
	return "", false
}

func (a *App) onModalKey(k vaxis.Key) bool {
	a.mu.Lock()
	if a.modal.kind == modalNone {
		a.mu.Unlock()
		return false
	}
	visible := maxInt(a.modal.visible, 1)
	switch {
	// Escape pops one level, and the two keys every terminal user reaches for
	// when they want out pop the same one. A panel is dismissed the same way
	// wherever it came from.
	case k.Matches(vaxis.KeyEsc), k.Matches('c', vaxis.ModCtrl), k.Matches('g', vaxis.ModCtrl):
		a.dismissModalLocked()
		a.mu.Unlock()
		return true
	case k.Matches(vaxis.KeyUp), k.Matches('p', vaxis.ModCtrl):
		a.modal.selector.Move(-1, visible)
		a.mu.Unlock()
		return true
	case k.Matches(vaxis.KeyDown), k.Matches('n', vaxis.ModCtrl):
		a.modal.selector.Move(1, visible)
		a.mu.Unlock()
		return true
	case k.Matches(vaxis.KeyEnter):
		choice, ok := a.modal.selector.Current()
		if !ok {
			a.mu.Unlock()
			return true
		}
		if choice.Disabled != "" {
			a.mu.Unlock()
			a.refuse(choice.Disabled)
			return true
		}
		act := a.chooseLocked(choice)
		a.mu.Unlock()
		act()
		return true
	// Tab marks a chat and moves on, which is what every multiple-choice list a
	// terminal user already has does. It cannot be the space bar: this panel is
	// also a search box, and a space in a search box is a space.
	case a.modal.kind == modalForward && k.Matches(vaxis.KeyTab):
		if choice, ok := a.modal.selector.Current(); ok && choice.ChatID != "" {
			if a.modal.marked[choice.ChatID] {
				delete(a.modal.marked, choice.ChatID)
			} else {
				a.modal.marked[choice.ChatID] = true
			}
			a.modal.selector.Move(1, visible)
		}
		a.mu.Unlock()
		return true
	// A menu names the key beside every action it lists, so pressing that key
	// is the other way of choosing the row. It is not typed into otherwise.
	case a.modal.kind == modalMenu:
		if id, ok := a.menuKeyLocked(k); ok {
			a.modal = modalState{}
			a.mu.Unlock()
			a.execute(id)
			return true
		}
		a.mu.Unlock()
		return true
	case a.modal.kind == modalConfirm:
		// A confirmation is two lines and a way out. Everything that is not one
		// of those is swallowed rather than filtering a list this short.
		a.mu.Unlock()
		return true
	case k.Matches(vaxis.KeyBackspace):
		// Rubbing out the last of a slash query rubs out the slash that opened
		// the menu, which is the menu closing.
		if a.modal.kind == modalSlash && len(a.modal.query) == 0 {
			a.dismissModalLocked()
			a.mu.Unlock()
			return true
		}
		var search string
		var generation uint64
		var issue bool
		if a.modal.cursor > 0 {
			a.modal.query = append(a.modal.query[:a.modal.cursor-1], a.modal.query[a.modal.cursor:]...)
			a.modal.cursor--
			search, generation, issue = a.refreshModalLocked()
		}
		a.mu.Unlock()
		if issue {
			a.searchChats(search, generation)
		}
		return true
	case k.Matches(vaxis.KeyLeft):
		if a.modal.cursor > 0 {
			a.modal.cursor--
		}
		a.mu.Unlock()
		return true
	case k.Matches(vaxis.KeyRight):
		if a.modal.cursor < len(a.modal.query) {
			a.modal.cursor++
		}
		a.mu.Unlock()
		return true
	default:
		var search string
		var generation uint64
		var issue bool
		if k.Text != "" {
			rs := []rune(k.Text)
			a.modal.query = append(a.modal.query[:a.modal.cursor], append(rs, a.modal.query[a.modal.cursor:]...)...)
			a.modal.cursor += len(rs)
			search, generation, issue = a.refreshModalLocked()
		}
		a.mu.Unlock()
		if issue {
			a.searchChats(search, generation)
		}
		return true
	}
}

// modalShape is the pointer over an open panel: a hand on a row that will do
// something, and the ordinary arrow everywhere else, panel or not.
func (a *App) modalShape(m vaxis.Mouse) vaxis.MouseShape {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.modal.kind == modalNone {
		return vaxis.MouseShapeDefault
	}
	if inRect(m, a.modal.list) {
		if at := a.modal.selector.top + m.Row - a.modal.list.Row; at < len(a.modal.selector.items) {
			return vaxis.MouseShapeClickable
		}
	}
	// The query line is a line you type on.
	if m.Row == a.modal.list.Row-2 && m.Col >= a.modal.list.Col &&
		m.Col < a.modal.list.Col+a.modal.list.Width {
		return vaxis.MouseShapeTextInput
	}
	return vaxis.MouseShapeDefault
}

func (a *App) onModalMouse(m vaxis.Mouse) (bool, bool) {
	a.mu.Lock()
	if a.modal.kind == modalNone {
		a.mu.Unlock()
		return false, false
	}
	r, list, visible := a.modal.rect, a.modal.list, a.modal.visible
	inside := inRect(m, r)
	itemRow := -1
	if inRect(m, list) {
		itemRow = m.Row - list.Row
	}
	dirty := false
	if m.EventType == vaxis.EventMotion || m.Button == vaxis.MouseNoButton {
		dirty = a.modal.selector.Hover(itemRow, visible)
	}
	switch m.Button {
	case vaxis.MouseWheelUp, vaxis.MouseWheelDown:
		// A wheel off the panel is somebody looking at what is behind it, which
		// is the same thing a click off the panel says. The notch is spent
		// closing it rather than also moving what it uncovers: one gesture, one
		// thing, and the next notch scrolls.
		if !inside {
			a.dismissModalLocked()
			a.mu.Unlock()
			return true, true
		}
		by := 1
		if m.Button == vaxis.MouseWheelUp {
			by = -1
		}
		a.modal.selector.Wheel(by, visible)
		dirty = true
	case vaxis.MouseLeftButton:
		// Clicking off a panel dismisses it, the way clicking off a dialog
		// does everywhere else. The press is swallowed as well, so the click
		// that closes it never also lands on what was behind it.
		if m.EventType == vaxis.EventRelease && !inside {
			a.dismissModalLocked()
			a.mu.Unlock()
			return true, true
		}
		if m.EventType == vaxis.EventRelease && inside {
			choice, ok := a.modal.selector.Click(itemRow, visible)
			if ok && choice.Disabled != "" {
				reason := choice.Disabled
				a.mu.Unlock()
				a.refuse(reason)
				return true, true
			}
			if ok && choice.Disabled == "" {
				act := a.chooseLocked(choice)
				a.mu.Unlock()
				act()
				return true, true
			}
			dirty = true
		}
	}
	a.mu.Unlock()
	return true, dirty
}

// modalRect is where a panel sits: a third of the way down, centred, and the
// whole screen when the screen is too small to have an outside.
//
// rows is how many results it has to hold. A list that could be anything asks
// for a page of them; two answers to a question are two answers tall, because
// a dozen rows of frame around them says the panel is waiting for something
// else.
func (a *App) modalRect(w, h, rows int) layout.Rect {
	width := minInt(72, w-2)
	if width < 8 {
		width = w
	}
	// The frame, the line that says what this is about, the blank under it,
	// and the rows themselves.
	height := minInt(rows+4, minInt(14, h-2))
	if height < 3 {
		height = h
	}
	if width <= 0 || height <= 0 {
		return layout.Rect{}
	}
	return layout.Rect{Col: maxInt((w-width)/2, 0), Row: maxInt((h-height)/3, 0), Width: width, Height: height}
}

// menuRect is where a context menu sits: under the cell it was asked for, as
// wide as the longest thing in it, and pulled back onto the screen when there
// is not enough screen that way. That last part is the whole of what makes a
// menu feel native: every menu on every desktop flips rather than spills.
func (a *App) menuRect(w, h int, items []modalChoice, at point) layout.Rect {
	label, detail := 0, 0
	for _, item := range items {
		label = maxInt(label, a.width(item.Label))
		detail = maxInt(detail, a.width(item.Detail))
	}
	// The label, the gap, the key, and the frame with a column of air inside it.
	width := minInt(maxInt(label+menuGap+detail+6, 18), minInt(40, w-2))
	height := minInt(len(items)+2, h)
	if width <= 0 || height <= 0 {
		return layout.Rect{}
	}
	col, row := at.col, at.row+1
	if col+width > w {
		col = maxInt(w-width, 0)
	}
	if row+height > h {
		// Above the pointer rather than below it, so the row it is about stays
		// visible under the menu about it.
		row = maxInt(at.row-height, 0)
	}
	return layout.Rect{Col: col, Row: row, Width: width, Height: height}
}

// menuGap is the air between what an action is called and the key that does it.
const menuGap = 3

func (a *App) drawModal(win vaxis.Window) {
	a.mu.Lock()
	if a.modal.kind == modalNone {
		a.mu.Unlock()
		return
	}
	kind, query := a.modal.kind, string(a.modal.query)
	ask, answers := a.modal.prompt, len(a.modal.selector.items)
	selected, hovered := a.modal.selector.selected, a.modal.selector.hovered
	items := append([]modalChoice(nil), a.modal.selector.items...)
	marked, at := a.modal.marked, a.modal.at
	a.mu.Unlock()

	// A list of commands or chats is as long as the query makes it, so it takes
	// a page. A question, a menu and the reactions on a message are exactly as
	// long as they are.
	rows := modalPage
	switch kind {
	case modalConfirm, modalMenu:
		rows = answers
	}
	w, h := win.Size()
	outer := a.modalRect(w, h, rows)
	if kind == modalMenu {
		outer = a.menuRect(w, h, items, at)
	}
	if outer.Empty() {
		return
	}
	width, height := outer.Width, outer.Height
	pane := sub(win, outer)
	panel := a.theme.BackgroundPanel
	fill(pane, panel)
	// A run and a bubble are both images over the cell background: covering
	// one with a panel hides the text under it and leaves the picture.
	a.occlude(outer)
	a.occludeSurfaces(outer)
	a.guardSpill(win, outer)

	title, prompt := "Commands", "> "
	switch kind {
	case modalSlash:
		title, prompt = "Slash commands", "/"
	case modalHelp:
		title, prompt = "Keyboard help", "? "
	case modalConfirm:
		// The message itself stands where the query line does, because which
		// message this is about is the thing worth saying.
		title, prompt = "Delete message", ask
	case modalReact:
		title, prompt = "React", "> "
	case modalMenu:
		title = "Message"
	case modalForward:
		title, prompt = "Forward to", "> "
		if len(marked) > 0 {
			title = "Forward to " + plural(len(marked), "chat")
		}
	}

	// The frame, so the panel reads as something on top of the transcript
	// rather than a hole in it, and in the focused border colour because a
	// modal is the only thing with focus while it is open. A terminal too
	// small for a frame gets the content instead of the decoration.
	inner := outer
	if width > 4 && height > 4 {
		bx := boxFor(a.caps)
		edge := vaxis.Style{Foreground: a.theme.BorderActive, Background: panel}
		a.print(pane, 0, 0, edge, bx.topLeft+strings.Repeat(bx.horizontal, width-2)+bx.topRight)
		a.print(pane, 0, height-1, edge, bx.bottomLeft+strings.Repeat(bx.horizontal, width-2)+bx.bottomRight)
		for row := 1; row < height-1; row++ {
			a.print(pane, 0, row, edge, bx.vertical)
			a.print(pane, width-1, row, edge, bx.vertical)
		}
		// The title sits in the top edge, the way a framed panel names itself.
		a.print(pane, 2, 0, vaxis.Style{Foreground: a.theme.Accent, Background: panel, Attribute: vaxis.AttrBold},
			" "+a.clip(title, maxInt(width-6, 1))+" ")
		inner = layout.Rect{Col: outer.Col + 2, Row: outer.Row + 1, Width: width - 4, Height: height - 2}
	} else {
		a.print(pane, 0, 0, vaxis.Style{Foreground: a.theme.Accent, Background: panel, Attribute: vaxis.AttrBold},
			a.clip(title, width))
		inner = layout.Rect{Col: outer.Col, Row: outer.Row + 1, Width: width, Height: height - 1}
	}
	if inner.Width < 1 || inner.Height < 1 {
		return
	}

	// Everything else fades, the way a dialog fades the page behind it.
	a.dimBehind(win, outer)

	body := sub(win, inner)
	// A menu is the rows and nothing else: there is no query, and a blank line
	// where one would be is a menu with a hole in the top of it.
	head := 2
	if kind == modalMenu {
		head = 0
	} else {
		line := vaxis.Style{Foreground: a.theme.Text, Background: panel}
		if kind == modalConfirm {
			// Not a line anybody types on, and it must not look like one.
			line = vaxis.Style{Foreground: a.theme.TextMuted, Background: panel, Attribute: vaxis.AttrItalic}
		}
		a.print(body, 0, 0, line, a.clip(prompt+query, inner.Width))
	}

	// One blank row under the query, then the results. The list is what the
	// panel is for, so it takes every row that is left.
	listRow := inner.Row + head
	visible := maxInt(inner.Height-head, 0)

	a.mu.Lock()
	a.modal.rect = outer
	a.modal.list = layout.Rect{Col: inner.Col, Row: listRow, Width: inner.Width, Height: visible}
	a.modal.visible = visible
	shown := append([]modalChoice(nil), a.modal.selector.Visible(visible)...)
	top := a.modal.selector.top
	a.mu.Unlock()

	// Two columns need room for two columns. Below that the label takes the
	// panel, because a name cut in half helps nobody. A menu is the exception:
	// its second column is one key, and the key is the point of showing it.
	split := inner.Width
	switch {
	case kind == modalMenu:
		keys := 0
		for _, item := range shown {
			keys = maxInt(keys, a.width(item.Detail))
		}
		if keys > 0 {
			split = maxInt(inner.Width-keys, 1)
		}
	case inner.Width >= 44:
		split = inner.Width / 2
	}

	for i, item := range shown {
		absolute := top + i
		bg := panel
		if absolute == selected {
			bg = a.theme.BackgroundActive
		} else if absolute == hovered {
			bg = a.theme.BackgroundHover
		}
		line := body.New(0, head+i, inner.Width, 1)
		fill(line, bg)
		style := vaxis.Style{Foreground: a.theme.Text, Background: bg}
		detail := item.Detail
		if item.Disabled != "" {
			style.Foreground = a.theme.TextFaint
			detail = item.Disabled
		}
		col := 0
		if kind == modalForward {
			// The mark is a glyph and a colour, so a marked row is still marked
			// on a terminal with sixteen of them.
			col = a.print(line, 0, 0, vaxis.Style{Foreground: a.theme.Accent, Background: bg},
				a.markGlyph(marked[item.ChatID]))
		}
		a.print(line, col, 0, style, a.clip(item.Label, maxInt(split-col-1, 1)))
		if split < inner.Width {
			a.print(line, split, 0, vaxis.Style{Foreground: a.theme.TextMuted, Background: bg},
				a.clip(detail, inner.Width-split))
		}
	}
}

// markGlyph is the two columns in front of a row that can be picked more than
// once: what it looks like when it is, and the same two columns of nothing when
// it is not, so no row ever moves as it is marked.
func (a *App) markGlyph(marked bool) string {
	if !marked {
		return "  "
	}
	if a.caps.Tier <= term.TierPlain {
		return "* "
	}
	return "✓ "
}

// dimBehind fades every cell the panel does not cover, and the rasterised
// words with them: a phrase is an image, and an image ignores an attribute.
func (a *App) dimBehind(win vaxis.Window, r layout.Rect) {
	w, h := win.Size()
	for row := 0; row < h; row++ {
		if row >= r.Row && row < r.Row+r.Height {
			a.dimCells(win, 0, r.Col, row)
			a.dimCells(win, r.Col+r.Width, w, row)
			continue
		}
		a.dimCells(win, 0, w, row)
	}
	for i := range a.placements {
		p := &a.placements[i]
		if p.row >= r.Row && p.row < r.Row+r.Height &&
			p.col+p.span > r.Col && p.col < r.Col+r.Width {
			continue
		}
		p.ink.A = uint8(uint32(p.ink.A) * dimPercent / 100)
	}
	// Whatever chrome is still on the frame is chrome the panel did not
	// cover, because covering is what the occlusion pass already did.
	ground := color.NRGBA{a.theme.InkGround[0], a.theme.InkGround[1], a.theme.InkGround[2], 0xff}
	for i := range a.surfaces {
		a.surfaces[i].spec = paint.Fade{Spec: a.surfaces[i].spec, Toward: ground, Percent: dimPercent}
	}
}

// dimPercent is how much of the ink survives behind a panel. Enough to read
// what is there, little enough that the panel is plainly in front of it.
const dimPercent = 45

func (a *App) dimCells(win vaxis.Window, from, to, row int) {
	for col := from; col < to; col++ {
		c := a.vx.Cell(col, row)
		if c.Style.Attribute&vaxis.AttrDim != 0 {
			continue
		}
		c.Style.Attribute |= vaxis.AttrDim
		// The attribute alone is a hint the terminal is free to ignore, and
		// several do. Where the colours are real the fade is computed.
		c.Style.Foreground = a.faded(c.Style.Foreground, c.Style.Background)
		win.SetCell(col, row, c)
	}
}

// faded mixes an ink toward the ground it sits on. Indexed colours are the
// terminal's own and cannot be mixed, so those keep the attribute and nothing
// else.
func (a *App) faded(ink, ground vaxis.Color) vaxis.Color {
	return a.mixInk(ink, ground, dimPercent)
}

// mixInk is percent of one colour over the rest of another. Indexed colours are
// the terminal's own and cannot be mixed, so those come back untouched and
// whatever they were carrying carries it alone.
func (a *App) mixInk(ink, ground vaxis.Color, percent int) vaxis.Color {
	if percent >= 100 {
		return ink
	}
	fg, bg := ink.Params(), ground.Params()
	if len(fg) != 3 {
		return ink
	}
	if len(bg) != 3 {
		if bg = a.theme.Background.Params(); len(bg) != 3 {
			return ink
		}
	}
	mix := func(f, b uint8) uint8 {
		return uint8((uint32(f)*uint32(percent) + uint32(b)*uint32(100-percent)) / 100)
	}
	return vaxis.RGBColor(mix(fg[0], bg[0]), mix(fg[1], bg[1]), mix(fg[2], bg[2]))
}
