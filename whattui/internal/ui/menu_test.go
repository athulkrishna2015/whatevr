package ui

import (
	"strings"
	"testing"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"
)

// only finds the one request made with this method, and complains if the
// gesture made any other number of them.
func only(t *testing.T, calls []sentRequest, method string) sentRequest {
	t.Helper()
	var found []sentRequest
	for _, call := range calls {
		if call.method == method {
			found = append(found, call)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d %s requests, want one: %#v", len(found), method, calls)
	}
	return found[0]
}

// Reacting is the same gesture as everything else that acts on a message: point
// at it, press the key, pick from the one selector widget the whole application
// uses.
func TestReactingSendsTheEmojiThePickerWasLeftOn(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	id, _ := pointAt(a, t, false)

	a.onKey(key('+'))
	if a.modal.kind != modalReact {
		t.Fatalf("pressing + opened %v, want the reaction picker", a.modal.kind)
	}
	if a.modal.message != id {
		t.Fatalf("the picker is about %q, want the message the cursor is on %q", a.modal.message, id)
	}
	first, ok := a.modal.selector.Current()
	if !ok || first.Emoji == "" {
		t.Fatalf("the picker opened on %#v, want an emoji", first)
	}

	a.onKey(arrow(vaxis.KeyEnter))
	call := only(t, *calls, "message_react")
	if r := call.req.GetMessageReact(); r.GetMessageId() != id || r.GetEmoji() != first.Emoji {
		t.Fatalf("the reaction went out as %v, want %q on %q", r, first.Emoji, id)
	}
	if a.modal.kind != modalNone {
		t.Error("the picker is still open after picking")
	}
}

// One reaction per person, so the one you already put there is not another
// reaction to add: it is the one to take off, and pressing it does that.
func TestPressingYourOwnReactionTakesItBack(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	c := a.conversation
	reset(c.msgs)
	putMsg(c.msgs, "00000000000000000001", (v2.MessageRow_builder{
		Id: "m", TextBody: &v2.Text{}, Text: "already agreed with",
		Sender:         person("x", "someone"),
		ReactionCounts: counts([]rx{{"🔥", "", true}}),
	}.Build()))
	ready(c.msgs, true)
	a.paint()
	a.setCursor("m")

	a.onKey(key('+'))
	a.onKey(arrow(vaxis.KeyEnter))

	call := only(t, *calls, "message_react")
	if got := call.req.GetMessageReact().GetEmoji(); got != "" {
		t.Fatalf("pressing your own reaction sent %q, want the empty one that removes it", got)
	}
}

// A forward goes to as many chats as you like, so the panel takes more than one
// answer. Tab marks, because this panel is also a search box and a space in a
// search box is a space.
func TestForwardingGoesToEveryChatThatWasMarked(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	id, _ := pointAt(a, t, false)

	a.onKey(key('f'))
	if a.modal.kind != modalForward {
		t.Fatalf("pressing f opened %v, want the forward picker", a.modal.kind)
	}
	if len(a.modal.selector.Items()) != a.chats.Len() {
		t.Fatalf("the picker offers %d chats, want the %d we hold", len(a.modal.selector.Items()), a.chats.Len())
	}

	first := a.modal.selector.Items()[0].ChatID
	second := a.modal.selector.Items()[1].ChatID
	a.onKey(vaxis.Key{Keycode: vaxis.KeyTab})
	a.onKey(vaxis.Key{Keycode: vaxis.KeyTab})
	if len(a.modal.marked) != 2 {
		t.Fatalf("two tabs marked %d chats", len(a.modal.marked))
	}

	a.onKey(arrow(vaxis.KeyEnter))
	call := only(t, *calls, "message_forward")
	if got := call.req.GetMessageForward().GetMessageId(); got != id {
		t.Fatalf("the forward is about %v, want %q", got, id)
	}
	sent := call.req.GetMessageForward().GetChatIds()
	if len(sent) != 2 || !has(sent, first) || !has(sent, second) {
		t.Fatalf("forwarded to %v, want both marked chats", sent)
	}
}

// Nothing marked is not nothing chosen: the row the panel is on is the answer,
// which is what enter means in every other list in the application.
func TestForwardingWithNothingMarkedGoesToTheRowItIsOn(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	pointAt(a, t, false)

	a.onKey(key('f'))
	want := a.modal.selector.Items()[0].ChatID
	a.onKey(arrow(vaxis.KeyEnter))

	call := only(t, *calls, "message_forward")
	if sent := call.req.GetMessageForward().GetChatIds(); len(sent) != 1 || sent[0] != want {
		t.Fatalf("forwarded to %v, want the highlighted chat %q", sent, want)
	}
}

// Right-click is the oldest gesture a pointer has, and what it opens is the
// registry filtered to the message under it, so the menu can never offer
// something the key would refuse.
func TestRightClickOpensTheActionsForTheMessageUnderThePointer(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	records(a)
	a.paint()
	at := a.messages[0]

	a.onMouse(vaxis.Mouse{
		Col: at.at.Col + 2, Row: at.at.Row,
		Button: vaxis.MouseRightButton, EventType: vaxis.EventRelease,
	})

	if a.modal.kind != modalMenu {
		t.Fatalf("right-click opened %v, want the menu", a.modal.kind)
	}
	if a.cursor() != at.id || a.modal.message != at.id {
		t.Fatalf("the menu is about %q and the cursor is on %q, want both on %q", a.modal.message, a.cursor(), at.id)
	}

	var names []string
	for _, item := range a.modal.selector.Items() {
		names = append(names, string(item.Command))
		if item.Command == cmdMenu {
			t.Error("the menu offers to open itself")
		}
	}
	for _, want := range []commandID{cmdReply, cmdReact, cmdForward, cmdCopyMessage} {
		if !has(names, string(want)) {
			t.Errorf("the menu does not offer %q: %v", want, names)
		}
	}

	// And the key it names beside an action is the key that does it.
	a.onKey(key('r'))
	if a.modal.kind != modalNone {
		t.Fatal("the menu stayed open after an action was chosen")
	}
	if a.composer.replyTo != at.id {
		t.Fatalf("the draft answers %q, want %q", a.composer.replyTo, at.id)
	}
}

// A message somebody deleted for everybody is a note saying one was here.
// Every action answers no to it, so the pointer opens nothing over it rather
// than a menu of refusals, and it does not become the thing the keyboard is
// pointing at either.
func TestADeletedMessageHasNoMenuAndIsNotWorthPointingAt(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	records(a)
	c := a.conversation
	reset(c.msgs)
	putMsg(c.msgs, "00000000000000000001", (v2.MessageRow_builder{
		Id: "gone", TextBody: &v2.Text{}, Revoked: true,
		Sender: person("x", "someone"),
	}.Build()))
	ready(c.msgs, true)
	a.paint()
	at := a.messages[0]
	if at.id != "gone" {
		t.Fatalf("the transcript drew %q, want the deleted message", at.id)
	}

	a.onMouse(vaxis.Mouse{
		Col: at.at.Col + 2, Row: at.at.Row,
		Button: vaxis.MouseRightButton, EventType: vaxis.EventRelease,
	})
	if a.modal.kind != modalNone {
		t.Fatalf("right-click on a deleted message opened %v", a.modal.kind)
	}
	if a.cursor() != "" {
		t.Errorf("right-click on a deleted message pointed at %q", a.cursor())
	}

	// And with the cursor put on it by hand, every action but taking the note
	// off this device says no.
	a.setCursor("gone")
	state := a.commandState()
	for _, id := range []commandID{cmdReply, cmdReact, cmdForward, cmdCopyMessage, cmdStar, cmdMenu} {
		if a.enabled(id, state) {
			t.Errorf("%q applies to a message that is already deleted", id)
		}
	}
	if !a.enabled(cmdDelete, state) {
		t.Error("a deleted message cannot be taken off this device")
	}
}

// The menu goes where it was asked for, and stays on the screen: a menu that
// spills off the bottom is a menu with its last action missing.
func TestTheMenuStaysOnTheScreenWhereverItIsAskedFor(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()
	cols, rows := a.vx.Window().Size()

	for _, at := range []point{{2, 2}, {cols - 1, rows - 1}, {cols - 1, 0}, {0, rows - 1}} {
		a.setCursor(a.messages[0].id)
		a.openMessageModal(modalMenu, at)
		a.paint()
		r := a.modal.rect
		if r.Col < 0 || r.Row < 0 || r.Col+r.Width > cols || r.Row+r.Height > rows {
			t.Errorf("a menu asked for at %v landed at %#v, off a %dx%d screen", at, r, cols, rows)
		}
		a.closeModal()
	}
}

// A panel is dismissed by looking away from it, and a wheel off it is looking
// away. Anything else means a notch over the transcript scrolls a list on the
// other side of the screen.
func TestScrollingOffAPanelPutsItAway(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	records(a)
	a.paint()
	at := a.messages[0]

	for _, open := range []func(){
		func() {
			a.setCursor(at.id)
			a.openMessageModal(modalMenu, point{at.at.Col + 2, at.at.Row})
		},
		func() { a.openModal(modalPalette) },
	} {
		open()
		a.paint()
		r := a.modal.rect
		if r.Empty() {
			t.Fatal("the panel drew nothing to scroll off")
		}

		// Inside it, the wheel is the list's own.
		a.onMouse(vaxis.Mouse{
			Col: r.Col + 1, Row: r.Row + 1,
			Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress,
		})
		if a.modal.kind == modalNone {
			t.Fatal("a wheel inside the panel closed it")
		}

		a.onMouse(vaxis.Mouse{
			Col: r.Col, Row: r.Row + r.Height + 1,
			Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress,
		})
		if a.modal.kind != modalNone {
			t.Fatalf("a wheel off the panel left %v open", a.modal.kind)
		}
	}
}

// The menu is a projection of the registry, so an action that does not apply is
// not in it. An edit of somebody else's message is the one everybody hits.
func TestTheMenuOnlyOffersWhatTheMessageCanActuallyDo(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	records(a)
	_, m := pointAt(a, t, false)
	if m.GetFromMe() {
		t.Skip("no incoming message in the window")
	}

	a.execute(cmdMenu)
	for _, item := range a.modal.selector.Items() {
		if item.Command == cmdEditMessage {
			t.Error("the menu offers to edit a message somebody else sent")
		}
	}
	if strings.TrimSpace(a.modal.selector.Items()[0].Detail) == "" {
		t.Error("the menu names no key beside its first action")
	}
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
