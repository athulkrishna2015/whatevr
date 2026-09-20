package ui

import (
	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/proto"
	"whattui/internal/view"
)

// The input model, stated once: typing types. The composer holds focus inside
// a chat, and the navigation keys only take over while it is empty. Nothing
// here is modal, and nothing here is the only way to reach an action.
func (a *App) onKey(k vaxis.Key) {
	if k.EventType == vaxis.EventRelease {
		return
	}

	// Quit is the one binding that works from everywhere, because a terminal
	// application that can trap you is a terminal application people
	// uninstall. Ctrl+C clears the line first when there is one, which is
	// what it does in every shell.
	if k.Matches('q', vaxis.ModCtrl) {
		a.mu.Lock()
		a.quit = true
		a.mu.Unlock()
		return
	}
	if k.Matches('c', vaxis.ModCtrl) {
		a.mu.Lock()
		typed := !a.composer.empty()
		if typed {
			a.composer.clear()
		} else {
			a.quit = true
		}
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	focus := a.focus
	a.mu.Unlock()

	switch focus {
	case FocusList:
		a.onListKey(k)
	case FocusTranscript:
		a.onTranscriptKey(k)
	default:
		a.onComposerKey(k)
	}
}

func (a *App) onListKey(k vaxis.Key) {
	switch {
	case k.Matches(vaxis.KeyUp) || k.Matches('k'):
		a.moveSelection(-1)
	case k.Matches(vaxis.KeyDown) || k.Matches('j'):
		a.moveSelection(1)
	case k.Matches(vaxis.KeyHome) || k.Matches('g'):
		a.setSelection(0)
	case k.Matches(vaxis.KeyEnd) || k.Matches('G'):
		a.setSelection(a.chats.Len() - 1)
	case k.Matches(vaxis.KeyPgUp):
		a.moveSelection(-a.listPage())
	case k.Matches(vaxis.KeyPgDown):
		a.moveSelection(a.listPage())
	case k.Matches(vaxis.KeyEnter) || k.Matches(vaxis.KeyRight) || k.Matches('l'):
		a.openSelected()
	case k.Matches(vaxis.KeyTab):
		a.cycleFocus(1)
	case k.Matches(vaxis.KeyTab, vaxis.ModShift):
		a.cycleFocus(-1)
	default:
		// Typing in a list is trying to talk, not trying to navigate. Hand
		// the keystroke to the composer rather than swallowing it.
		if k.Text != "" && a.activeChat != "" && !isListNav(k) {
			a.setFocus(FocusComposer)
			a.onComposerKey(k)
		}
	}
}

// isListNav are the vim aliases, which stay aliases rather than becoming text.
func isListNav(k vaxis.Key) bool {
	switch k.Text {
	case "j", "k", "g", "G", "l":
		return k.Modifiers == 0
	}
	return false
}

func (a *App) onTranscriptKey(k vaxis.Key) {
	switch {
	case k.Matches(vaxis.KeyUp) || k.Matches('k'):
		a.scrollTranscript(1)
	case k.Matches(vaxis.KeyDown) || k.Matches('j'):
		a.scrollTranscript(-1)
	case k.Matches(vaxis.KeyPgUp):
		a.scrollTranscript(a.transcriptPage())
	case k.Matches(vaxis.KeyPgDown):
		a.scrollTranscript(-a.transcriptPage())
	case k.Matches(vaxis.KeyEsc):
		a.setFocus(FocusComposer)
	case k.Matches(vaxis.KeyTab):
		a.cycleFocus(1)
	case k.Matches(vaxis.KeyTab, vaxis.ModShift):
		a.cycleFocus(-1)
	default:
		if k.Text != "" && a.activeChat != "" && !isTranscriptNav(k) {
			a.setFocus(FocusComposer)
			a.onComposerKey(k)
		}
	}
}

func isTranscriptNav(k vaxis.Key) bool {
	switch k.Text {
	case "j", "k":
		return k.Modifiers == 0
	}
	return false
}

func (a *App) cycleFocus(by int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	order := []Focus{FocusList, FocusTranscript, FocusComposer}
	if a.activeChat == "" {
		order = []Focus{FocusList}
	}
	at := 0
	for i, f := range order {
		if f == a.focus {
			at = i
		}
	}
	a.focus = order[((at+by)%len(order)+len(order))%len(order)]
}

func (a *App) setFocus(f Focus) {
	a.mu.Lock()
	if f != FocusList && a.activeChat == "" {
		f = FocusList
	}
	a.focus = f
	a.mu.Unlock()
}

func (a *App) moveSelection(by int) { a.setSelection(a.selection() + by) }

func (a *App) selection() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selected
}

func (a *App) setSelection(to int) {
	n := a.chats.Len()
	if n == 0 {
		return
	}
	if to < 0 {
		to = 0
	}
	if to >= n {
		to = n - 1
	}

	a.mu.Lock()
	a.selected = to
	// Keep the selection on screen. The list scrolls to follow the cursor
	// rather than the cursor being confined to the visible rows.
	page := a.listPageLocked()
	if to < a.listTop {
		a.listTop = to
	}
	if page > 0 && to >= a.listTop+page {
		a.listTop = to - page + 1
	}
	a.mu.Unlock()
}

func (a *App) openSelected() {
	at := a.selection()
	var id string
	a.chats.Read(func(items []view.Item[proto.ChatRow]) {
		if at >= 0 && at < len(items) {
			id = items[at].ID
		}
	})
	if id != "" {
		a.openChat(id)
	}
}

// scrollTranscript moves up the history. Positive is back in time, because
// that is the direction the reader thinks in.
func (a *App) scrollTranscript(by int) {
	a.mu.Lock()
	c := a.conversation
	if c == nil {
		a.mu.Unlock()
		return
	}
	c.scroll += by
	if c.scroll < 0 {
		c.scroll = 0
	}
	if n := c.msgs.Len(); c.scroll > n-1 {
		c.scroll = n - 1
		if c.scroll < 0 {
			c.scroll = 0
		}
	}
	near := c.scroll > c.msgs.Len()-messagePageSize/2
	a.mu.Unlock()

	// Reaching for history the window does not hold yet is what asks for more
	// of it. Asking early keeps the scroll from ever hitting a wall.
	if near {
		a.loadOlder()
	}
}

func (a *App) listPage() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listPageLocked()
}

func (a *App) listPageLocked() int {
	cols, rows := a.vx.Window().Size()
	return layout.Compute(cols, rows, true, 1).VisibleChats()
}

func (a *App) transcriptPage() int {
	l := a.layout()
	if l.Transcript.Height < 1 {
		return 1
	}
	return l.Transcript.Height
}

// onMouse is the other half of the promise: everything the keyboard can do,
// the pointer can do too. It reports whether the frame needs drawing again,
// because motion arrives for every pixel and most of it changes nothing.
func (a *App) onMouse(m vaxis.Mouse) bool {
	l := a.layout()
	over := -1
	if inRect(m, l.ChatList) && !l.ChatList.Empty() {
		a.mu.Lock()
		top := a.listTop
		a.mu.Unlock()
		if at := l.ChatAt(m.Row); at >= 0 && top+at < a.chats.Len() {
			over = top + at
		}
	}
	dirty := a.hover(over)

	switch m.Button {
	case vaxis.MouseWheelUp:
		if inRect(m, l.ChatList) {
			a.scrollList(-1)
			return true
		}
		a.scrollTranscript(1)
		return true
	case vaxis.MouseWheelDown:
		if inRect(m, l.ChatList) {
			a.scrollList(1)
			return true
		}
		a.scrollTranscript(-1)
		return true
	case vaxis.MouseLeftButton:
		if m.EventType != vaxis.EventPress {
			return dirty
		}
		if over >= 0 {
			a.setFocus(FocusList)
			a.setSelection(over)
			a.openSelected()
			return true
		}
		if inRect(m, l.Composer) {
			a.setFocus(FocusComposer)
			return true
		}
		if inRect(m, l.Transcript) {
			a.setFocus(FocusTranscript)
			return true
		}
	}
	return dirty
}

// scrollList moves the window without moving the selection, which is what a
// wheel does everywhere else.
func (a *App) scrollList(by int) {
	n := a.chats.Len()
	a.mu.Lock()
	defer a.mu.Unlock()
	page := a.listPageLocked()
	top := a.listTop + by
	if top > n-page {
		top = n - page
	}
	if top < 0 {
		top = 0
	}
	a.listTop = top
}

func inRect(m vaxis.Mouse, r layout.Rect) bool {
	if r.Empty() {
		return false
	}
	return m.Col >= r.Col && m.Col < r.Col+r.Width &&
		m.Row >= r.Row && m.Row < r.Row+r.Height
}
