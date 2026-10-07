package ui

import (
	"testing"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
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
	a := stubApp(80, 24, 2, 0)
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
	a := stubApp(80, 24, 2, 0)
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
	a := stubApp(80, 24, 2, 0)
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
	a := stubApp(80, 24, 2, 0)
	a.focus = FocusComposer
	a.onKey(key('?'))
	if got := a.composer.String(); got != "?" || a.modal.kind != modalNone {
		t.Fatalf("composer ? gave draft %q modal %d", got, a.modal.kind)
	}
	// A slash on an empty line opens the menu, and from there the menu keeps
	// what is typed into it. The draft is not a command line.
	a.composer.clear()
	a.onKey(key('/'))
	if a.modal.kind != modalSlash {
		t.Fatalf("slash modal = %d, want slash", a.modal.kind)
	}
	a.onKey(key('h'))
	if got := a.composer.String(); got != "" || string(a.modal.query) != "h" {
		t.Fatalf("slash typing gave draft %q query %q, want it all in the menu", got, a.modal.query)
	}
	a.onKey(vaxis.Key{Keycode: vaxis.KeyEsc})
	if got := a.composer.String(); got != "" {
		t.Fatalf("escaping the menu left %q on the draft", got)
	}
	if a.modal.kind != modalNone {
		t.Fatalf("escape left modal %d", a.modal.kind)
	}

	a.onKey(key('/'))
	a.onKey(key('h'))
	a.onKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.modal.kind != modalHelp || !a.composer.empty() {
		t.Fatalf("slash enter left modal %d draft %q", a.modal.kind, a.composer.String())
	}
	a.closeModal()

	// Rubbing out the slash that opened it closes it again.
	a.composer.clear()
	a.onKey(key('/'))
	a.onKey(vaxis.Key{Keycode: vaxis.KeyBackspace})
	if a.modal.kind != modalNone {
		t.Fatalf("backspacing the slash left modal %d", a.modal.kind)
	}

	a.composer.clear()
	a.focus = FocusList
	a.onKey(key('?'))
	if a.modal.kind != modalHelp {
		t.Fatalf("list ? modal = %d, want help", a.modal.kind)
	}
}

// The one thing that opens the command menu is a slash typed onto an empty
// line. Text that arrives any other way is text, which is what makes pasting a
// path or a url into a chat safe.
func TestOnlyATypedSlashOnAnEmptyLineOpensTheMenu(t *testing.T) {
	a := stubApp(80, 24, 2, 0)
	a.focus = FocusComposer

	pasted := "/ajwndj"
	for _, r := range pasted {
		k := key(r)
		k.EventType = vaxis.EventPaste
		a.onKey(k)
	}
	if a.modal.kind != modalNone {
		t.Fatalf("a pasted slash opened modal %d", a.modal.kind)
	}
	if got := a.composer.String(); got != pasted {
		t.Fatalf("the paste left %q on the draft, want %q", got, pasted)
	}

	// Typed, but not onto an empty line.
	a.onKey(key('/'))
	if a.modal.kind != modalNone {
		t.Fatalf("a slash inside a draft opened modal %d", a.modal.kind)
	}
	if got := a.composer.String(); got != pasted+"/" {
		t.Fatalf("the draft is %q, want the slash typed into it", got)
	}
}

// A paste is text even when a message is lit, or pasting anything with an r in
// it would answer a message instead.
func TestPastedLettersAreNeverMessageActions(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	a.paint()
	a.onKey(arrow(vaxis.KeyUp))
	if a.cursor() == "" {
		t.Fatal("nothing was pointed at")
	}

	k := key('r')
	k.EventType = vaxis.EventPaste
	a.onKey(k)
	if a.composer.targeted() || len(*calls) != 0 {
		t.Fatalf("a pasted letter acted on the message: %+v", *calls)
	}
	if got := a.composer.String(); got != "r" {
		t.Fatalf("the draft is %q, want the pasted letter", got)
	}
}

func TestPaletteChatSearchPreservesDaemonOrderAndIgnoresStaleResponses(t *testing.T) {
	a := stubApp(80, 24, 2, 0)
	type call struct {
		query string
		cb    proto.ResponseFunc
	}
	var calls []call
	a.request = func(req *v2.Request, cb proto.ResponseFunc) {
		if !req.HasSearchChats() {
			t.Fatalf("method = %v, want search_chats", req.WhichMethod())
		}
		calls = append(calls, call{req.GetSearchChats().GetQuery(), cb})
	}
	a.openModal(modalPalette)
	// The slash alone is not a search for nothing: the daemon answers an empty
	// query with an empty list, and the chats we hold are already the chats.
	a.onKey(key('/'))
	if len(calls) != 0 {
		t.Fatalf("an empty query asked the daemon: %#v", calls)
	}
	if len(a.modal.selector.Items()) != 2 {
		t.Fatalf("the slash showed %d chats, want the list we hold", len(a.modal.selector.Items()))
	}
	a.onKey(key('a'))
	a.onKey(key('b'))
	if len(calls) != 2 || calls[0].query != "a" || calls[1].query != "ab" {
		t.Fatalf("calls = %#v, want queries a then ab", calls)
	}

	respond := func(c call, chats ...*v2.ChatRow) {
		c.cb(chatsAnswer(chats...), nil)
	}
	respond(calls[1], v2.ChatRow_builder{Id: "2", Name: "second"}.Build(), v2.ChatRow_builder{Id: "1", Name: "first"}.Build())
	respond(calls[0], v2.ChatRow_builder{Id: "old", Name: "stale"}.Build())
	items := a.modal.selector.Items()
	if len(items) != 2 || items[0].ChatID != "2" || items[1].ChatID != "1" {
		t.Fatalf("chat order = %#v, want daemon order 2, 1", items)
	}
}

func TestPaletteIgnoresResponseFromPreviousOpen(t *testing.T) {
	a := stubApp(80, 24, 2, 0)
	var callbacks []proto.ResponseFunc
	a.request = func(_ *v2.Request, cb proto.ResponseFunc) { callbacks = append(callbacks, cb) }
	a.openModal(modalPalette)
	a.onKey(key('/'))
	a.onKey(key('a'))
	a.closeModal()
	a.openModal(modalPalette)
	a.onKey(key('/'))
	a.onKey(key('a'))

	callbacks[0](chatsAnswer(v2.ChatRow_builder{Id: "old", Name: "old"}.Build()), nil)
	if got := a.modal.selector.Items(); len(got) != 0 {
		t.Fatalf("previous-open response replaced current results: %#v", got)
	}
}

func TestDisabledCommandsRemainVisibleWithReason(t *testing.T) {
	a := stubApp(80, 24, 0, 0)
	a.activeChat = ""
	choices := a.commandChoices("send", false, false)
	if len(choices) == 0 || choices[0].Command != cmdSend || choices[0].Disabled == "" {
		t.Fatalf("disabled send choice = %#v", choices)
	}
}

func TestModalMouseHoverClickWheelAndTinyPaint(t *testing.T) {
	a := stubApp(10, 3, 2, 0)
	a.openModal(modalHelp)
	a.paint()
	if a.modal.rect.Width <= 0 || a.modal.rect.Height <= 0 {
		t.Fatalf("tiny modal rect = %#v", a.modal.rect)
	}

	a = stubApp(80, 24, 2, 0)
	a.openModal(modalHelp)
	a.paint()
	m := vaxis.Mouse{Col: a.modal.list.Col, Row: a.modal.list.Row, Button: vaxis.MouseNoButton, EventType: vaxis.EventMotion}
	if handled, dirty := a.onModalMouse(m); !handled || !dirty || a.modal.selector.hovered != 0 {
		t.Fatalf("hover handled=%v dirty=%v row=%d", handled, dirty, a.modal.selector.hovered)
	}
	// A pointer that is not on a row highlights nothing, inside the panel or
	// out of it.
	for _, outside := range []vaxis.Mouse{
		{Col: 0, Row: a.modal.list.Row, Button: vaxis.MouseNoButton, EventType: vaxis.EventMotion},
		{Col: a.modal.list.Col, Row: a.modal.rect.Row, Button: vaxis.MouseNoButton, EventType: vaxis.EventMotion},
	} {
		if _, _ = a.onModalMouse(outside); a.modal.selector.hovered != -1 {
			t.Fatalf("pointer at %d,%d highlighted row %d", outside.Col, outside.Row, a.modal.selector.hovered)
		}
		a.onModalMouse(m)
	}
	a.onModalMouse(vaxis.Mouse{Col: m.Col, Row: m.Row, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})
	if a.modal.selector.top == 0 && len(a.modal.selector.items) > a.modal.visible {
		t.Fatal("wheel did not scroll modal")
	}
	// A click runs the row it lands on, wherever the wheel happens to have left
	// it, which is the whole contract between the pointer and the registry.
	at, target := -1, modalChoice{}
	for i, item := range a.modal.selector.Visible(a.modal.visible) {
		if item.Disabled == "" {
			at, target = i, item
			break
		}
	}
	if at < 0 {
		t.Fatal("no runnable row in the panel")
	}
	ran := false
	a.commands.byID[target.Command].Run = func() { ran = true }
	a.onModalMouse(vaxis.Mouse{
		Col: m.Col, Row: a.modal.list.Row + at,
		Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease,
	})
	if !ran {
		t.Fatalf("a click on %q ran nothing", target.Command)
	}
}

// Every action is reachable three ways, and the slash menu is one of them. An
// empty slash query lists all of them, not the two somebody remembered to
// name.
func TestEveryCommandHasASlashName(t *testing.T) {
	a := stubApp(80, 24, 2, 0)
	a.initCommands()
	seen := map[string]commandID{}
	for _, c := range a.commands.ordered {
		if c.Slash == "" {
			t.Errorf("%s has no slash name, so it is keybind only", c.ID)
			continue
		}
		if other, ok := seen[c.Slash]; ok {
			t.Errorf("/%s is both %s and %s", c.Slash, other, c.ID)
		}
		seen[c.Slash] = c.ID
	}
	if got := len(a.commandChoices("", true, false)); got != len(a.commands.ordered) {
		t.Errorf("an empty slash query listed %d of %d commands", got, len(a.commands.ordered))
	}
}

// A binding is written down once, in the registry, and every surface reads it
// from there. So the thing that reads it has to understand what is written
// rather than a list of what happened to be there when it was written: ^l was
// in the palette, in the help and on the hint line, and the key did nothing.
func TestEveryBindingTheRegistryWritesDownIsAKeyThatWorks(t *testing.T) {
	a := stubApp(90, 26, 4, 0)
	a.initCommands()
	for _, c := range a.commands.ordered {
		if c.Direct == "" {
			continue
		}
		key, ok := keyNamed(c.Direct)
		if !ok {
			t.Errorf("%s binds %q, which nothing here knows how to press", c.ID, c.Direct)
			continue
		}
		if !matchesBinding(key, c.Direct) {
			t.Errorf("%s binds %q and pressing it does nothing", c.ID, c.Direct)
		}
	}
}

// keyNamed builds the key a binding names, the way a reader of the registry
// would press it.
func keyNamed(binding string) (vaxis.Key, bool) {
	switch binding {
	case "tab":
		return vaxis.Key{Keycode: vaxis.KeyTab}, true
	case "s-tab":
		return vaxis.Key{Keycode: vaxis.KeyTab, Modifiers: vaxis.ModShift}, true
	case "enter":
		return vaxis.Key{Keycode: vaxis.KeyEnter}, true
	case "esc":
		return vaxis.Key{Keycode: vaxis.KeyEsc}, true
	}
	runes := []rune(binding)
	if len(runes) == 2 && runes[0] == '^' {
		return vaxis.Key{Keycode: runes[1], Modifiers: vaxis.ModCtrl}, true
	}
	if len(runes) == 1 {
		return vaxis.Key{Keycode: runes[0], Text: binding}, true
	}
	return vaxis.Key{}, false
}
