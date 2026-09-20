package ui

import (
	"encoding/json"
	"strings"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/proto"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalPalette
	modalSlash
	modalHelp
)

type modalChoice struct {
	Command  commandID
	ChatID   string
	Label    string
	Detail   string
	Disabled string
}

type modalState struct {
	kind     modalKind
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
	if kind == modalSlash {
		draft := a.composer.String()
		a.modal.query = []rune(strings.TrimPrefix(draft, "/"))
		a.modal.cursor = len(a.modal.query)
	}
	a.refreshModalLocked()
	a.mu.Unlock()
}

// dismissModalLocked closes whatever is open without acting on it, and
// remembers a slash draft so the menu does not spring straight back up.
func (a *App) dismissModalLocked() {
	if a.modal.kind == modalSlash {
		a.slashDismissed = a.composer.String()
	}
	a.modal = modalState{}
}

func (a *App) closeModal() {
	a.mu.Lock()
	if a.modal.kind == modalSlash {
		a.slashDismissed = a.composer.String()
	}
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
	}
	return "", 0, false
}

func (a *App) searchChats(query string, generation uint64) {
	request := a.request
	if request == nil {
		return
	}
	request("search.chats", proto.Params{"query": query, "limit": 50}, func(raw json.RawMessage, err *proto.Error) {
		a.mu.Lock()
		current := a.modal.kind == modalPalette && generation == a.modal.request
		a.mu.Unlock()
		if !current {
			return
		}
		if err != nil {
			a.toast(err.Message)
			return
		}
		var result struct {
			Chats []proto.ChatRow `json:"chats"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return
		}
		choices := make([]modalChoice, 0, len(result.Chats))
		for _, chat := range result.Chats {
			choices = append(choices, modalChoice{ChatID: chat.ID, Label: chat.Name, Detail: chat.Preview})
		}
		a.mu.Lock()
		if a.modal.kind == modalPalette && generation == a.modal.request {
			a.modal.selector.Set(choices)
		}
		a.mu.Unlock()
		a.vx.PostEvent(redraw{})
	})
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
			a.toast(choice.Disabled)
			return true
		}
		kind := a.modal.kind
		a.modal = modalState{}
		if kind == modalSlash {
			a.composer.clear()
		}
		a.mu.Unlock()
		if choice.ChatID != "" {
			a.openChat(choice.ChatID)
		} else {
			a.execute(choice.Command)
		}
		return true
	case k.Matches(vaxis.KeyBackspace):
		if a.modal.kind == modalSlash {
			a.mu.Unlock()
			return false
		}
		var search string
		var generation uint64
		var issue bool
		if a.modal.kind != modalSlash && a.modal.cursor > 0 {
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
		if a.modal.kind == modalSlash {
			a.mu.Unlock()
			return false
		}
		if a.modal.cursor > 0 {
			a.modal.cursor--
		}
		a.mu.Unlock()
		return true
	case k.Matches(vaxis.KeyRight):
		if a.modal.kind == modalSlash {
			a.mu.Unlock()
			return false
		}
		if a.modal.cursor < len(a.modal.query) {
			a.modal.cursor++
		}
		a.mu.Unlock()
		return true
	default:
		if a.modal.kind == modalSlash {
			a.mu.Unlock()
			return false
		}
		var search string
		var generation uint64
		var issue bool
		if a.modal.kind != modalSlash && k.Text != "" {
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

func (a *App) syncSlashModal() {
	a.mu.Lock()
	draft := a.composer.String()
	if a.focus != FocusComposer || !strings.HasPrefix(draft, "/") {
		if a.modal.kind == modalSlash {
			a.modal = modalState{}
		}
		if !strings.HasPrefix(draft, "/") {
			a.slashDismissed = ""
		}
		a.mu.Unlock()
		return
	}
	opened := false
	if a.modal.kind == modalNone && draft != a.slashDismissed {
		a.modal = modalState{kind: modalSlash}
		opened = true
	}
	if a.modal.kind == modalSlash {
		query := strings.TrimPrefix(draft, "/")
		// A menu that just opened has no results yet, and a bare slash is the
		// query that matches everything rather than the one that matches
		// nothing.
		if opened || string(a.modal.query) != query {
			a.modal.query = []rune(query)
			a.modal.cursor = len(a.modal.query)
			a.refreshModalLocked()
		}
	}
	a.mu.Unlock()
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
	case vaxis.MouseWheelUp:
		a.modal.selector.Wheel(-1, visible)
		dirty = true
	case vaxis.MouseWheelDown:
		a.modal.selector.Wheel(1, visible)
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
				a.toast(reason)
				return true, true
			}
			if ok && choice.Disabled == "" {
				kind := a.modal.kind
				a.modal = modalState{}
				if kind == modalSlash {
					a.composer.clear()
				}
				a.mu.Unlock()
				if choice.ChatID != "" {
					a.openChat(choice.ChatID)
				} else {
					a.execute(choice.Command)
				}
				return true, true
			}
			dirty = true
		}
	}
	a.mu.Unlock()
	return true, dirty
}

func (a *App) drawModal(win vaxis.Window) {
	a.mu.Lock()
	if a.modal.kind == modalNone {
		a.mu.Unlock()
		return
	}
	kind, query := a.modal.kind, string(a.modal.query)
	selected, hovered := a.modal.selector.selected, a.modal.selector.hovered
	a.mu.Unlock()

	w, h := win.Size()
	width := minInt(72, w-2)
	if width < 8 {
		width = w
	}
	height := minInt(14, h-2)
	if height < 3 {
		height = h
	}
	if width <= 0 || height <= 0 {
		return
	}
	outer := layout.Rect{Col: maxInt((w-width)/2, 0), Row: maxInt((h-height)/3, 0), Width: width, Height: height}
	pane := sub(win, outer)
	panel := a.theme.BackgroundPanel
	fill(pane, panel)
	// A run is an image over the cell background: covering it with a panel
	// hides the text under it and leaves the picture.
	a.occlude(outer)
	a.guardSpill(win, outer)

	title, prompt := "Commands", "> "
	switch kind {
	case modalSlash:
		title, prompt = "Slash commands", "/"
	case modalHelp:
		title, prompt = "Keyboard help", "? "
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
	a.print(body, 0, 0, vaxis.Style{Foreground: a.theme.Text, Background: panel}, a.clip(prompt+query, inner.Width))

	// One blank row under the query, then the results. The list is what the
	// panel is for, so it takes every row that is left.
	listRow := inner.Row + 2
	visible := maxInt(inner.Height-2, 0)

	a.mu.Lock()
	a.modal.rect = outer
	a.modal.list = layout.Rect{Col: inner.Col, Row: listRow, Width: inner.Width, Height: visible}
	a.modal.visible = visible
	shown := append([]modalChoice(nil), a.modal.selector.Visible(visible)...)
	top := a.modal.selector.top
	a.mu.Unlock()

	for i, item := range shown {
		absolute := top + i
		bg := panel
		if absolute == selected {
			bg = a.theme.BackgroundActive
		} else if absolute == hovered {
			bg = a.theme.BackgroundHover
		}
		line := body.New(0, 2+i, inner.Width, 1)
		fill(line, bg)
		style := vaxis.Style{Foreground: a.theme.Text, Background: bg}
		detail := item.Detail
		if item.Disabled != "" {
			style.Foreground = a.theme.TextFaint
			detail = item.Disabled
		}
		split := inner.Width / 2
		a.print(line, 0, 0, style, a.clip(item.Label, maxInt(split-1, 1)))
		if inner.Width > 20 {
			a.print(line, split, 0, vaxis.Style{Foreground: a.theme.TextMuted, Background: bg},
				a.clip(detail, inner.Width-split))
		}
	}
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
		return uint8((uint32(f)*dimPercent + uint32(b)*(100-dimPercent)) / 100)
	}
	return vaxis.RGBColor(mix(fg[0], bg[0]), mix(fg[1], bg[1]), mix(fg[2], bg[2]))
}
