// Package ui is the whattui interface: the event loop, the focus ring and the
// panes.
package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/textrun"
	"whattui/internal/theme"
	"whattui/internal/view"
)

// Focus is which pane the keyboard is talking to.
type Focus int

const (
	FocusList Focus = iota
	FocusTranscript
	FocusComposer
)

// App is the whole interface.
type App struct {
	vx     *vaxis.Vaxis
	caps   term.Caps
	theme  theme.Theme
	client *proto.Client

	chats    *view.Collection[proto.ChatRow]
	chatsSub *proto.Subscription

	conn    *view.Object[proto.Connection]
	connSub *proto.Subscription

	// The rasteriser for the scripts a cell grid cannot hold, and the images
	// it has already produced. shaping is snapshotted once per frame so
	// measuring and drawing cannot disagree across the moment it comes up.
	shaper     *textrun.Shaper
	shaping    bool
	placements []placement
	images     map[imgKey]*vaxis.KittyImage
	seen       map[imgKey]bool

	// What the pointer has taken, what it is taking, and the runs of text a
	// triple click can take whole. All three are screen coordinates from the
	// last frame, so all three are dropped whenever the screen moves.
	sel    selection
	drag   drag
	blocks []layout.Rect

	// toastText is the transient line that says what just happened, and
	// toastUntil is when it stops being true.
	toastText  string
	toastUntil time.Time

	// State the UI owns. Presentation only: scroll positions, what is
	// selected, what is typed. Everything renderable lives in a view.
	mu         sync.Mutex
	focus      Focus
	selected   int
	activeChat string
	listTop    int
	// hovered is the chat under the pointer, as an index into the list, or
	// -1. Anything clickable answers the pointer, and a chat row is the most
	// clickable thing whattui has.
	hovered int
	// shape is the mouse cursor the terminal was last told to wear.
	shape     vaxis.MouseShape
	transport proto.State
	lastErr   error
	quit      bool

	composer composer
	commands commandRegistry
	modal    modalState
	leader   bool
	// slashDismissed prevents an escaped slash menu reopening until the draft
	// changes. Escape closes UI, never text.
	slashDismissed string
	request        func(string, proto.Params, proto.ResponseFunc)
	searchRequest  uint64

	conversation *conversation
}

// New wires an app to a terminal and a daemon. It does not connect.
func New(vx *vaxis.Vaxis, caps term.Caps, client *proto.Client) *App {
	a := &App{
		vx:      vx,
		caps:    caps,
		theme:   paletteFor(vx, caps),
		client:  client,
		chats:   view.NewCollection[proto.ChatRow](),
		conn:    view.NewObject[proto.Connection](),
		focus:   FocusList,
		hovered: -1,
		shape:   vaxis.MouseShapeDefault,
		images:  map[imgKey]*vaxis.KittyImage{},
		seen:    map[imgKey]bool{},
		drag:    drag{chat: -1},
	}
	a.shaper = shaperFor(vx, caps)
	a.request = client.Do
	a.initCommands()
	a.setCell()

	client.OnState = a.onTransport
	client.OnDrain = func() { vx.PostEvent(redraw{}) }
	client.OnOpenChat = func(ev proto.OpenChat) {
		a.openChat(ev.ChatID)
		vx.PostEvent(redraw{})
	}
	return a
}

// paletteFor asks the terminal what it is wearing and builds the palette out
// of that. A terminal that will not say, or cannot show more than sixteen
// colours, gets the indexed palette, which is its own colours anyway.
//
// The queries are round trips and must not happen from the render loop, so
// they happen exactly here, once, before there is a loop.
func paletteFor(vx *vaxis.Vaxis, caps term.Caps) theme.Theme {
	if caps.Tier < term.TierColor {
		return theme.Default()
	}
	var bg, fg vaxis.Color
	if caps.ReportsBG {
		bg, fg = vx.QueryBackground(), vx.QueryForeground()
	}
	return theme.Derive(bg, fg)
}

// shaperFor builds the complex-script rasteriser, or does not.
//
// It needs kitty graphics to place what it draws, so below that tier the text
// stays terminal text: wrong, but honestly wrong, and the same wrong every
// other terminal application is.
func shaperFor(vx *vaxis.Vaxis, caps term.Caps) *textrun.Shaper {
	if !caps.CanPaint() {
		return nil
	}
	kitty := ""
	if strings.HasPrefix(os.Getenv("TERM"), "xterm-kitty") {
		// Only kitty is asked for its own metrics, and only because it will
		// answer. Everything else derives them from the face we resolve.
		kitty = "kitty"
	}
	return textrun.New(textrun.Options{
		Kitty:         kitty,
		Family:        os.Getenv("WHATTUI_FONT"),
		BaselineNudge: atoi(os.Getenv("WHATTUI_BASELINE_NUDGE")),
		// The shaper comes up in the background, so this fires off the main
		// goroutine and only asks for a repaint.
		Notify: func() { vx.PostEvent(redraw{}) },
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// toast says what just happened, briefly, without taking the screen. It is
// posted from wherever the thing happened, including off the event loop.
func (a *App) toast(msg string) {
	a.mu.Lock()
	a.toastText = msg
	a.toastUntil = time.Now().Add(toastLife)
	a.mu.Unlock()
	// One wake-up when it expires, so the line actually leaves rather than
	// sitting there until the next keystroke.
	time.AfterFunc(toastLife, func() { a.vx.PostEvent(redraw{}) })
	a.vx.PostEvent(redraw{})
}

const toastLife = 2500 * time.Millisecond

func (a *App) toastNow() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Now().After(a.toastUntil) {
		return ""
	}
	return a.toastText
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// setCell hands the shaper the terminal's cell in pixels. Everything it draws
// is measured off the cell, so this is what keeps a rasterised word the same
// size as the text beside it.
func (a *App) setCell() {
	if a.shaper == nil {
		return
	}
	size := a.vx.Size()
	if size.Cols <= 0 || size.Rows <= 0 {
		return
	}
	a.shaper.SetCellSize(size.XPixel/size.Cols, size.YPixel/size.Rows)
}

// recell re-measures the cell and throws the rasterised images away if it
// moved, because every one of them is now the wrong size.
func (a *App) recell() {
	before, beforeH := a.shaper.CellSize()
	a.setCell()
	if w, h := a.shaper.CellSize(); w != before || h != beforeH {
		a.dropRuns()
	}
}

// redraw wakes the event loop after the client applied a batch.
type redraw struct{}

func (a *App) onTransport(s proto.State, _ *proto.ServerInfo, err error) {
	a.mu.Lock()
	a.transport = s
	if err != nil {
		a.lastErr = err
	}
	if s == proto.Ready {
		a.lastErr = nil
	}
	a.mu.Unlock()
	a.vx.PostEvent(redraw{})
}

// Run connects, subscribes and renders until the user quits.
func (a *App) Run() error {
	a.client.Start()
	defer a.client.Stop()

	a.connSub = a.client.Subscribe("connection", nil, a.conn)
	a.subscribeChats()

	a.draw()
	events := a.vx.Events()
	for ev := range events {
		dirty := a.handle(ev)
		if a.done() {
			return nil
		}
		// Everything already queued is handled before anything is drawn.
		// Pointer motion arrives for every pixel crossed, and a frame per
		// pixel is a pointer the highlight trails behind.
		drained := true
		for drained {
			select {
			case next, ok := <-events:
				if !ok {
					return nil
				}
				dirty = a.handle(next) || dirty
				if a.done() {
					return nil
				}
			default:
				drained = false
			}
		}
		if dirty {
			a.draw()
		}
	}
	return nil
}

// handle applies one event and reports whether the screen has to be drawn
// again because of it.
func (a *App) handle(ev vaxis.Event) bool {
	switch ev := ev.(type) {
	case vaxis.Key:
		a.onKey(ev)
	case vaxis.Mouse:
		return a.onMouse(ev)
	case vaxis.Resize:
		// vaxis does not resize itself: the size arrives as an event and the
		// application says when to believe it. Geometry follows the size and
		// nothing else, so believing it is the whole handler.
		a.vx.Resize(ev)
		a.clearSelection()
		// A resize can also be a font size change, and every rasterised word
		// is measured off the cell.
		a.recell()
	case vaxis.QuitEvent:
		a.mu.Lock()
		a.quit = true
		a.mu.Unlock()
		return false
	}
	return true
}

func (a *App) done() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quit
}

func (a *App) subscribeChats() {
	if a.chatsSub != nil {
		a.chatsSub.Close()
		a.chats.Reset()
	}
	a.chatsSub = a.client.Subscribe("chats", proto.Params{
		"filter":   "all",
		"archived": false,
		"limit":    chatPageSize,
	}, a.chats)
}

const chatPageSize = 50

// status is the one line that says what is wrong, or nothing at all. The two
// failures are different and a reader has to be able to tell them apart: the
// socket being down is whattui's problem, WhatsApp being down is not.
func (a *App) status() (string, vaxis.Color, bool) {
	a.mu.Lock()
	transport, lastErr := a.transport, a.lastErr
	a.mu.Unlock()

	if transport != proto.Ready {
		switch {
		case lastErr == nil:
			return "connecting to whatevrd", a.theme.TextMuted, true
		case proto.NotRunning(lastErr):
			return "whatevrd is not running", a.theme.Error, true
		default:
			return fmt.Sprintf("whatevrd unreachable: %v", lastErr), a.theme.Error, true
		}
	}

	c, ok := a.conn.Value()
	if !ok {
		return "", 0, false
	}
	switch c.State {
	case "online":
		return "", 0, false
	case "need_login":
		return "not logged in: run whatkevr to pair, or wait for the qr screen", a.theme.Warning, true
	case "connecting", "starting":
		return "connecting to whatsapp", a.theme.TextMuted, true
	case "reconnecting":
		return "reconnecting to whatsapp", a.theme.Warning, true
	case "offline":
		return "whatsapp is offline", a.theme.Error, true
	default:
		return c.State, a.theme.TextMuted, true
	}
}

// hover records what the pointer is over and reports whether that changed.
func (a *App) hover(chat int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hovered == chat {
		return false
	}
	a.hovered = chat
	return true
}

// notice is the panel that replaces the transcript when there is nothing to
// show and a reason for it. It returns nothing at all when the reason is that
// the reader has simply not picked a chat yet, which the transcript says in
// its own words.
//
// This is the moment most terminal clients get wrong: a dial error and an
// exit, where what the reader needs is the name of the service and the line
// that starts it.
func (a *App) notice() (title string, colour vaxis.Color, body []string, ok bool) {
	a.mu.Lock()
	transport, lastErr := a.transport, a.lastErr
	a.mu.Unlock()

	if transport == proto.Ready {
		c, have := a.conn.Value()
		if !have {
			return "", 0, nil, false
		}
		switch c.State {
		case "need_login":
			return "not paired with a phone", a.theme.Warning, []string{
				"whattui cannot show a chat until whatevr is linked",
				"to your phone. pair it, then come back:",
				"",
				"  whatkevr",
			}, true
		case "offline":
			return "whatsapp is offline", a.theme.Error, []string{
				"the daemon is running and is not connected.",
				"it retries on its own.",
			}, true
		}
		return "", 0, nil, false
	}

	if lastErr != nil && proto.NotRunning(lastErr) {
		return "whatevrd is not running", a.theme.Error, []string{
			"whattui talks to the daemon, never to whatsapp.",
			"nothing is listening on",
			"",
			"  " + a.client.SocketPath(),
			"",
			"start it and whattui connects on its own:",
			"",
			"  systemctl --user start whatevrd",
		}, true
	}
	if lastErr != nil {
		return "cannot reach whatevrd", a.theme.Error, []string{
			lastErr.Error(),
			"",
			"retrying every second.",
		}, true
	}
	return "connecting to whatevrd", a.theme.TextMuted, []string{a.client.SocketPath()}, true
}

func (a *App) layout() layout.Layout {
	win := a.vx.Window()
	cols, rows := win.Size()
	a.mu.Lock()
	listFocused := a.focus == FocusList || a.activeChat == ""
	a.mu.Unlock()

	// Twice, because the composer's height depends on the width it gets and
	// the width it gets depends on nothing but the shape. The first pass is
	// only ever read for that width.
	l := layout.Compute(cols, rows, listFocused, 1)
	return layout.Compute(cols, rows, listFocused, a.composerRows(l.Composer.Width))
}
