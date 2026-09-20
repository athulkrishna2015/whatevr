// Package ui is the whattui interface: the event loop, the focus ring and the
// panes.
package ui

import (
	"fmt"
	"sync"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/proto"
	"whattui/internal/term"
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

	// State the UI owns. Presentation only: scroll positions, what is
	// selected, what is typed. Everything renderable lives in a view.
	mu         sync.Mutex
	focus      Focus
	selected   int
	activeChat string
	listTop    int
	transport  proto.State
	lastErr    error
	quit       bool

	conversation *conversation
}

// New wires an app to a terminal and a daemon. It does not connect.
func New(vx *vaxis.Vaxis, caps term.Caps, client *proto.Client) *App {
	a := &App{
		vx:     vx,
		caps:   caps,
		theme:  theme.Default(),
		client: client,
		chats:  view.NewCollection[proto.ChatRow](),
		conn:   view.NewObject[proto.Connection](),
		focus:  FocusList,
	}

	client.OnState = a.onTransport
	client.OnDrain = func() { vx.PostEvent(redraw{}) }
	client.OnOpenChat = func(ev proto.OpenChat) {
		a.openChat(ev.ChatID)
		vx.PostEvent(redraw{})
	}
	return a
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
	for ev := range a.vx.Events() {
		switch ev := ev.(type) {
		case vaxis.Key:
			a.onKey(ev)
		case vaxis.Mouse:
			a.onMouse(ev)
		case vaxis.Resize:
			// Geometry follows the size and nothing else, so a resize is
			// just another draw.
		case vaxis.Redraw, redraw:
		case vaxis.QuitEvent:
			return nil
		}

		a.mu.Lock()
		quit := a.quit
		a.mu.Unlock()
		if quit {
			return nil
		}
		a.draw()
	}
	return nil
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
		msg := "connecting to whatevrd"
		if lastErr != nil {
			msg = fmt.Sprintf("whatevrd unreachable: %v", lastErr)
		}
		return msg, a.theme.Error, true
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

func (a *App) layout() layout.Layout {
	win := a.vx.Window()
	cols, rows := win.Size()
	a.mu.Lock()
	listFocused := a.focus == FocusList || a.activeChat == ""
	a.mu.Unlock()
	return layout.Compute(cols, rows, listFocused)
}
