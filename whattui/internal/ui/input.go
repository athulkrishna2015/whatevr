package ui

import (
	"time"

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
	if a.onModalKey(k) {
		return
	}

	// Ctrl+X is a level, not a timer. It remains armed until one key resolves
	// it, or Escape explicitly cancels it.
	a.mu.Lock()
	leader := a.leader
	a.mu.Unlock()
	if leader {
		if k.Matches(vaxis.KeyEsc) {
			a.mu.Lock()
			a.leader = false
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		a.leader = false
		a.mu.Unlock()
		if id, ok := a.leaderCommand(k); ok {
			a.execute(id)
		} else {
			a.refuse("unknown ^x command")
		}
		return
	}
	if k.Matches('x', vaxis.ModCtrl) {
		a.mu.Lock()
		a.leader = true
		a.mu.Unlock()
		return
	}

	if id, ok := a.directCommand(k); ok {
		a.execute(id)
		return
	}
	// the panes are not on screen, so nothing typed may land in one
	if a.pairing() {
		return
	}

	// Esc pops exactly one level, and a live selection is the outermost one.
	if k.Matches(vaxis.KeyEsc) && a.clearSelection() {
		return
	}

	a.mu.Lock()
	focus := a.focus
	a.mu.Unlock()

	// The message actions are bare letters, which only works because a cursor
	// and a draft are never both on screen: with a message lit, a letter acts
	// on it, and with none it is a letter.
	//
	// They belong to the panes the message is in. A letter pressed at the chat
	// list is about a chat, whatever is still lit over in the transcript.
	if focus != FocusList && a.cursor() != "" {
		if id, ok := a.messageCommand(k); ok {
			a.execute(id)
			return
		}
	}
	if k.Matches('?') && focus != FocusComposer {
		if id, ok := a.commandForDirect("?"); ok {
			a.execute(id)
		}
		return
	}

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
		a.execute(cmdOpenChat)
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
	// With a cursor the arrows move it and the screen follows; without one
	// they scroll. Which of the two is live is on the screen rather than in a
	// flag: either a message is lit or none is.
	pointing := a.cursor() != ""
	switch {
	case k.Matches(vaxis.KeyUp) || k.Matches('k'):
		if pointing {
			a.moveCursor(1)
			return
		}
		a.scrollTranscript(1)
	case k.Matches(vaxis.KeyDown) || k.Matches('j'):
		if pointing {
			a.moveCursor(-1)
			return
		}
		a.scrollTranscript(-1)
	case k.Matches(vaxis.KeyPgUp):
		a.scrollTranscript(a.transcriptPage())
	case k.Matches(vaxis.KeyPgDown):
		a.scrollTranscript(-a.transcriptPage())
	case k.Matches(vaxis.KeyEsc):
		// One level at a time: the cursor first, the pane after it.
		if a.clearCursor() {
			return
		}
		a.setFocus(FocusComposer)
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
	a.chats.Read(func(items []view.Item[proto.ChatRow], _ view.State) {
		if at >= 0 && at < len(items) {
			id = items[at].ID
		}
	})
	if id != "" {
		a.openChat(id)
	}
}

// scrollTranscript moves up the history, in rows. Positive is back in time,
// because that is the direction the reader thinks in.
func (a *App) scrollTranscript(by int) {
	a.clearSelection()
	viewport := a.transcriptPage()

	a.mu.Lock()
	c := a.conversation
	a.mu.Unlock()
	if c == nil {
		return
	}

	// Reaching for history the window does not hold yet is what asks for more
	// of it, which clampScroll does a page early so the scroll never hits a
	// wall.
	c.scroll += by
	a.clampScroll(c, viewport)
}

// wheelRows is how far one notch of the wheel moves the transcript. Three is
// what every other application does.
const wheelRows = 3

// wheelPane is which pane a wheel notch moves: the one the gesture started in,
// for as long as the gesture lasts.
//
// A touchpad goes on sending notches after the fingers have left it, and the
// pointer is free to wander over the other pane while it glides. Routing every
// notch by where the pointer is at that instant hands the rest of the glide to
// whatever it wandered onto, which stops one pane mid-flick and shoves another.
// So the first notch picks the pane and the rest of the burst follows it.
//
// A burst ends three ways, and the middle one is what makes this bearable:
//
//   - the notches stop for long enough to be over;
//   - they speed up, which a glide never does. A notch that arrives faster than
//     the one before it is a hand back on the pad, and a hand over the other
//     pane means that pane, right now, with no wait;
//   - they have been going for longer than anything glides, so whatever this
//     is, it is somebody scrolling.
func (a *App) wheelPane(overList bool) bool {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	gap := now.Sub(a.wheelAt)
	elsewhere := overList != a.wheelList
	if gap > wheelIdle || now.Sub(a.wheelSince) > wheelBurst || (elsewhere && gap*2 < a.wheelGap) {
		a.wheelList, a.wheelSince = overList, now
		// A burst that has only had one notch has no rate yet, and one
		// interval cannot be faster than the interval before it.
		gap = 0
	}
	a.wheelAt, a.wheelGap = now, gap
	return a.wheelList
}

const (
	// wheelIdle is how long after a notch the next one is still the same
	// gesture. Long enough for the slow tail of a glide, short enough that two
	// deliberate flicks are two gestures.
	wheelIdle = 250 * time.Millisecond
	// wheelBurst is the longest one gesture holds a pane. Nothing coasts for a
	// second, so past that the pointer decides again whatever else is true.
	wheelBurst = time.Second
)

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
	m = a.onScreen(m)
	if handled, dirty := a.onModalMouse(m); handled {
		if m.EventType == vaxis.EventMotion || m.Button == vaxis.MouseNoButton {
			a.pointer(a.modalShape(m))
		}
		return dirty
	}
	if a.pairing() {
		// nothing under the pointer but the code
		a.pointer(vaxis.MouseShapeDefault)
		return false
	}
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
	if a.hoverMessage(a.messageUnder(m)) {
		dirty = true
	}
	a.pointer(a.shapeFor(m, over))

	switch m.Button {
	case vaxis.MouseWheelUp, vaxis.MouseWheelDown:
		// The content moves out from under a selection made on the screen, so
		// the selection goes with it.
		a.clearSelection()
		list := a.wheelPane(inRect(m, l.ChatList))
		switch {
		case list && m.Button == vaxis.MouseWheelUp:
			a.scrollList(-1)
		case list:
			a.scrollList(1)
		case m.Button == vaxis.MouseWheelUp:
			a.scrollTranscript(wheelRows)
		default:
			a.scrollTranscript(-wheelRows)
		}
		return true
	case vaxis.MouseRightButton:
		// The gesture every pointer has meant since pointers had two buttons:
		// the things this one can do, here. It acts on the message under the
		// pointer rather than the one the cursor was on, so right-clicking is
		// one gesture and not two.
		if m.EventType != vaxis.EventRelease {
			return true
		}
		id := a.messageUnder(m)
		if id == "" || !inRect(m, l.Transcript) {
			return dirty
		}
		// Nothing opens over something there is nothing to do to. A message
		// somebody deleted for everybody is a note saying one was here, and a
		// menu over it would be a list of things that all answer no. The same
		// goes for the lines nobody wrote: a day, a system notice or a call
		// never lands in the pointer's list of messages to begin with.
		if row, ok := a.messageRow(id); !ok || row.Revoked {
			return dirty
		}
		a.setCursor(id)
		a.setFocus(FocusTranscript)
		a.openMessageModal(modalMenu, point{m.Col, m.Row})
		return true
	case vaxis.MouseLeftButton:
		p := point{m.Col, m.Row}
		switch m.EventType {
		case vaxis.EventPress:
			// A press is only ever the start of a selection. What the click
			// meant waits for the release, because a click that acts on the
			// way down cannot also be a drag.
			if a.onSelectPress(p) {
				return true
			}
			a.mu.Lock()
			a.drag.chat = over
			a.mu.Unlock()
			return true
		case vaxis.EventMotion:
			return a.onSelectMotion(p)
		case vaxis.EventRelease:
			if a.onSelectRelease() {
				return true
			}
			a.mu.Lock()
			chat := a.drag.chat
			a.drag.chat = -1
			a.mu.Unlock()
			switch {
			case chat >= 0:
				a.setFocus(FocusList)
				a.setSelection(chat)
				a.execute(cmdOpenChat)
			case inRect(m, l.Composer):
				a.setFocus(FocusComposer)
			case inRect(m, l.Transcript):
				// Pointing at a message is what the pointer is for, so a click
				// on one is the same cursor the arrows move. A click on the
				// ground beside them puts it down again.
				if id := a.messageUnder(m); id != "" {
					a.setCursor(id)
				} else {
					a.clearCursor()
				}
				a.setFocus(FocusTranscript)
			}
			return true
		}
	}
	return dirty
}

// onScreen pulls a pointer position inside the screen it is reported against.
//
// A mouse event carries the size the terminal had when the pointer moved, and
// the terminal is free to have been resized since. Everything downstream reads
// cells and blocks at that position, so it has to be a position that exists;
// clamped rather than dropped, because a release that lands outside still has
// to end the drag it started.
func (a *App) onScreen(m vaxis.Mouse) vaxis.Mouse {
	size := a.vx.Size()
	if size.Cols <= 0 || size.Rows <= 0 {
		return m
	}
	m.Col = minInt(maxInt(m.Col, 0), size.Cols-1)
	m.Row = minInt(maxInt(m.Row, 0), size.Rows-1)
	return m
}

// shapeFor is what the pointer looks like where it is: a beam over text you
// can select, a hand over something you can click, and the terminal's own
// arrow over everything else. A pointer that never changes is a pointer that
// never tells you anything, and one that is always a beam is the same thing.
func (a *App) shapeFor(m vaxis.Mouse, overChat int) vaxis.MouseShape {
	p := point{m.Col, m.Row}
	switch {
	case overChat >= 0:
		return vaxis.MouseShapeClickable
	case a.vx.Cell(p.col, p.row).Style.Hyperlink != "":
		return vaxis.MouseShapeClickable
	default:
		// A block, rather than whether there is a glyph here: the gaps inside
		// one are still text as far as a drag across them is concerned, and a
		// beam over anything else would promise a selection that never comes.
		if _, ok := a.blockAt(p); ok {
			return vaxis.MouseShapeTextInput
		}
		return vaxis.MouseShapeDefault
	}
}

// pointer sets the mouse shape, and only when it changed: the escape goes out
// of band, so writing it on every motion event is a write per pixel.
func (a *App) pointer(shape vaxis.MouseShape) {
	a.mu.Lock()
	same := a.shape == shape
	a.shape = shape
	a.mu.Unlock()
	if !same {
		a.vx.SetMouseShape(shape)
	}
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
