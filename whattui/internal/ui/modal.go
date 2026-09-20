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
	listRow  int
	visible  int
	request  uint64
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
	case k.Matches(vaxis.KeyEsc):
		if a.modal.kind == modalSlash {
			a.slashDismissed = a.composer.String()
		}
		a.modal = modalState{}
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
	if a.modal.kind == modalNone && draft != a.slashDismissed {
		a.modal = modalState{kind: modalSlash}
	}
	if a.modal.kind == modalSlash {
		query := strings.TrimPrefix(draft, "/")
		if string(a.modal.query) != query {
			a.modal.query = []rune(query)
			a.modal.cursor = len(a.modal.query)
			a.refreshModalLocked()
		}
	}
	a.mu.Unlock()
}

func (a *App) onModalMouse(m vaxis.Mouse) (bool, bool) {
	a.mu.Lock()
	if a.modal.kind == modalNone {
		a.mu.Unlock()
		return false, false
	}
	r, row, visible := a.modal.rect, a.modal.listRow, a.modal.visible
	inside := inRect(m, r)
	itemRow := m.Row - row
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

	body := sub(win, inner)
	a.print(body, 0, 0, vaxis.Style{Foreground: a.theme.Text, Background: panel}, a.clip(prompt+query, inner.Width))

	// One blank row under the query, then the results. The list is what the
	// panel is for, so it takes every row that is left.
	listRow := inner.Row + 2
	visible := maxInt(inner.Height-2, 0)

	a.mu.Lock()
	a.modal.rect = outer
	a.modal.listRow = listRow
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
