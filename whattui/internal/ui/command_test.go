package ui

import (
	"encoding/json"
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
)

func key(r rune, mods ...vaxis.ModifierMask) vaxis.Key {
	var mask vaxis.ModifierMask
	for _, mod := range mods {
		mask |= mod
	}
	text := string(r)
	if mask&vaxis.ModCtrl != 0 {
		text = ""
	}
	return vaxis.Key{Keycode: r, Text: text, Modifiers: mask}
}

func TestCommandRegistryIDsAreUniqueAndSurfacesResolve(t *testing.T) {
	a := benchApp(80, 24, 2, 0)
	a.initCommands()
	seen := map[commandID]bool{}
	for _, c := range a.commands.ordered {
		if c.ID == "" || seen[c.ID] {
			t.Fatalf("duplicate or empty command id %q", c.ID)
		}
		seen[c.ID] = true
		if a.commands.byID[c.ID] == nil || c.Run == nil {
			t.Fatalf("command %q has no registry executor", c.ID)
		}
	}
	for _, surface := range [][]modalChoice{
		a.commandChoices("", false, false),
		a.commandChoices("", false, true),
		a.commandChoices("", true, false),
	} {
		for _, choice := range surface {
			if a.commands.byID[choice.Command] == nil {
				t.Fatalf("surface references unknown command %q", choice.Command)
			}
		}
	}
}

func TestBindingsAndModalUseSameExecutor(t *testing.T) {
	a := benchApp(80, 24, 2, 0)
	a.initCommands()
	a.focus = FocusList
	runs := 0
	a.commands.byID[cmdHelp].Run = func() { runs++ }

	a.onKey(key('?'))
	if runs != 1 {
		t.Fatalf("direct help ran %d times, want 1", runs)
	}
	a.onKey(key('x', vaxis.ModCtrl))
	a.onKey(key('?'))
	if runs != 2 {
		t.Fatalf("leader help ran %d times, want 2", runs)
	}

	a.openModal(modalPalette)
	for a.modal.selector.selected < len(a.modal.selector.items)-1 && a.modal.selector.items[a.modal.selector.selected].Command != cmdHelp {
		a.modal.selector.Move(1, 20)
	}
	if current, _ := a.modal.selector.Current(); current.Command != cmdHelp {
		t.Fatal("help command missing from palette")
	}
	a.onKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if runs != 3 {
		t.Fatalf("palette help ran %d times, want 3", runs)
	}
}

func TestModalInputPrecedesPaneAndPreservesFocus(t *testing.T) {
	a := benchApp(80, 24, 2, 0)
	a.focus = FocusTranscript
	a.openModal(modalPalette)
	a.onKey(key('z'))
	if got := string(a.modal.query); got != "z" {
		t.Fatalf("modal query = %q, want z", got)
	}
	if a.focus != FocusTranscript || !a.composer.empty() {
		t.Fatalf("pane changed under modal: focus %d, draft %q", a.focus, a.composer.String())
	}
	a.onKey(vaxis.Key{Keycode: vaxis.KeyEsc})
	if a.modal.kind != modalNone || a.focus != FocusTranscript {
		t.Fatalf("escape modal kind %d focus %d", a.modal.kind, a.focus)
	}
}

func TestSlashMenuAndContextualHelp(t *testing.T) {
	a := benchApp(80, 24, 2, 0)
	a.focus = FocusComposer
	a.onKey(key('?'))
	if got := a.composer.String(); got != "?" || a.modal.kind != modalNone {
		t.Fatalf("composer ? gave draft %q modal %d", got, a.modal.kind)
	}
	a.composer.clear()
	a.onKey(key('/'))
	if a.modal.kind != modalSlash {
		t.Fatalf("slash modal = %d, want slash", a.modal.kind)
	}
	a.onKey(key('h'))
	if got := a.composer.String(); got != "/h" || string(a.modal.query) != "h" {
		t.Fatalf("slash typing gave draft %q query %q", got, a.modal.query)
	}
	a.onKey(vaxis.Key{Keycode: vaxis.KeyEsc})
	if got := a.composer.String(); got != "/h" {
		t.Fatalf("escape changed slash draft to %q", got)
	}
	if a.modal.kind != modalNone {
		t.Fatalf("escape left modal %d", a.modal.kind)
	}
	a.onKey(vaxis.Key{Keycode: vaxis.KeyBackspace})
	a.onKey(key('h'))
	a.onKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.modal.kind != modalHelp || !a.composer.empty() {
		t.Fatalf("slash enter left modal %d draft %q", a.modal.kind, a.composer.String())
	}
	a.closeModal()

	a.composer.clear()
	a.focus = FocusList
	a.onKey(key('?'))
	if a.modal.kind != modalHelp {
		t.Fatalf("list ? modal = %d, want help", a.modal.kind)
	}
}

func TestPaletteChatSearchPreservesDaemonOrderAndIgnoresStaleResponses(t *testing.T) {
	a := benchApp(80, 24, 2, 0)
	type call struct {
		query string
		cb    proto.ResponseFunc
	}
	var calls []call
	a.request = func(method string, params proto.Params, cb proto.ResponseFunc) {
		if method != "search.chats" {
			t.Fatalf("method = %q, want search.chats", method)
		}
		calls = append(calls, call{params["query"].(string), cb})
	}
	a.openModal(modalPalette)
	a.onKey(key('/'))
	a.onKey(key('a'))
	if len(calls) != 2 || calls[1].query != "a" {
		t.Fatalf("calls = %#v, want queries empty then a", calls)
	}

	respond := func(c call, chats ...proto.ChatRow) {
		raw, _ := json.Marshal(struct {
			Chats []proto.ChatRow `json:"chats"`
		}{chats})
		c.cb(raw, nil)
	}
	respond(calls[1], proto.ChatRow{ID: "2", Name: "second"}, proto.ChatRow{ID: "1", Name: "first"})
	respond(calls[0], proto.ChatRow{ID: "old", Name: "stale"})
	items := a.modal.selector.Items()
	if len(items) != 2 || items[0].ChatID != "2" || items[1].ChatID != "1" {
		t.Fatalf("chat order = %#v, want daemon order 2, 1", items)
	}
}

func TestPaletteIgnoresResponseFromPreviousOpen(t *testing.T) {
	a := benchApp(80, 24, 2, 0)
	var callbacks []proto.ResponseFunc
	a.request = func(_ string, _ proto.Params, cb proto.ResponseFunc) { callbacks = append(callbacks, cb) }
	a.openModal(modalPalette)
	a.onKey(key('/'))
	a.closeModal()
	a.openModal(modalPalette)
	a.onKey(key('/'))

	raw, _ := json.Marshal(struct {
		Chats []proto.ChatRow `json:"chats"`
	}{[]proto.ChatRow{{ID: "old", Name: "old"}}})
	callbacks[0](raw, nil)
	if got := a.modal.selector.Items(); len(got) != 0 {
		t.Fatalf("previous-open response replaced current results: %#v", got)
	}
}

func TestDisabledCommandsRemainVisibleWithReason(t *testing.T) {
	a := benchApp(80, 24, 0, 0)
	a.activeChat = ""
	choices := a.commandChoices("send", false, false)
	if len(choices) == 0 || choices[0].Command != cmdSend || choices[0].Disabled == "" {
		t.Fatalf("disabled send choice = %#v", choices)
	}
}

func TestModalMouseHoverClickWheelAndTinyPaint(t *testing.T) {
	a := benchApp(10, 3, 2, 0)
	a.openModal(modalHelp)
	a.paint()
	if a.modal.rect.Width <= 0 || a.modal.rect.Height <= 0 {
		t.Fatalf("tiny modal rect = %#v", a.modal.rect)
	}

	a = benchApp(80, 24, 2, 0)
	a.openModal(modalHelp)
	a.paint()
	m := vaxis.Mouse{Col: a.modal.rect.Col + 1, Row: a.modal.listRow, Button: vaxis.MouseNoButton, EventType: vaxis.EventMotion}
	if handled, dirty := a.onModalMouse(m); !handled || !dirty || a.modal.selector.hovered != 0 {
		t.Fatalf("hover handled=%v dirty=%v row=%d", handled, dirty, a.modal.selector.hovered)
	}
	a.onModalMouse(vaxis.Mouse{Col: m.Col, Row: m.Row, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})
	if a.modal.selector.top == 0 && len(a.modal.selector.items) > a.modal.visible {
		t.Fatal("wheel did not scroll modal")
	}
	a.onModalMouse(vaxis.Mouse{Col: m.Col, Row: m.Row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
	if a.modal.kind != modalPalette {
		t.Fatalf("click executed modal %d, want palette", a.modal.kind)
	}
}
