package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
)

// sentRequest is one request the frontend made, for the tests that care that a
// gesture reached the daemon as the right command.
type sentRequest struct {
	method string
	params proto.Params
}

// records swaps the daemon for a notebook. Every request is written down and
// answered yes, which is what the daemon does for all of these.
func records(a *App) *[]sentRequest {
	var out []sentRequest
	a.request = func(method string, params proto.Params, cb proto.ResponseFunc) {
		out = append(out, sentRequest{method: method, params: params})
		if cb != nil {
			cb(json.RawMessage(`{}`), nil)
		}
	}
	return &out
}

// pointAt walks the cursor back until it is on a message we sent, or one
// somebody else did, and answers which message that is.
func pointAt(a *App, t *testing.T, ours bool) (string, proto.MessageRow) {
	t.Helper()
	a.paint()
	for i := 0; i < 12; i++ {
		a.onKey(arrow(vaxis.KeyUp))
		m, ok := a.selectedMessage()
		if ok && m.Outgoing() == ours {
			return a.cursor(), m
		}
	}
	t.Skip("no such message in the window")
	return "", proto.MessageRow{}
}

// Reply is the whole reason the cursor exists: point at a message, type, send,
// and the daemon is told which message the answer is to.
func TestReplyingCarriesTheMessageThroughToTheSend(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	id, _ := pointAt(a, t, false)

	a.onKey(key('r'))
	if a.composer.replyTo != id {
		t.Fatalf("the draft answers %q, want %q", a.composer.replyTo, id)
	}
	if !a.composer.targeted() || a.composer.targetName == "" {
		t.Fatal("nothing on the composer says which message this is about")
	}
	if a.cursor() != "" {
		t.Error("the cursor is still lit, with the strip saying the same thing")
	}
	if a.focus != FocusComposer {
		t.Errorf("focus is %v after starting a reply, want the composer", a.focus)
	}

	a.onKey(key('h'))
	a.onKey(key('i'))
	a.execute(cmdSend)
	if len(*calls) != 1 {
		t.Fatalf("send made %d requests, want one", len(*calls))
	}
	call := (*calls)[0]
	if call.method != "send.text" {
		t.Fatalf("send used %q", call.method)
	}
	if call.params["reply_to"] != id {
		t.Fatalf("the send answers %v, want %q", call.params["reply_to"], id)
	}
	if call.params["text"] != "hi" {
		t.Fatalf("the send carries %v, want the draft", call.params["text"])
	}
	// And the next message is a new message, not another answer.
	if a.composer.targeted() {
		t.Error("the reply target survived the send")
	}
}

// The strip is the only thing that says a draft is an answer, and it costs the
// composer a row.
func TestTheComposerSaysWhatTheDraftIsAbout(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	records(a)
	before := a.layout().Composer.Height

	_, m := pointAt(a, t, false)
	a.onKey(key('r'))
	a.paint()

	if got := a.layout().Composer.Height; got != before+1 {
		t.Fatalf("the composer is %d rows with a reply strip, was %d", got, before)
	}
	strip := a.composerRowText(0)
	if !strings.Contains(strip, replyName(m)) {
		t.Errorf("the strip says %q, want the name it is answering", strip)
	}
	// And escape takes it back off, one level at a time.
	a.onKey(arrow(vaxis.KeyEsc))
	if a.composer.targeted() {
		t.Fatal("escape left the reply target on the draft")
	}
}

// An edit rewrites a message rather than saying another one, and it is the
// same gesture: point, press, type, send.
func TestEditingAMessageSendsAnEdit(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	id, m := pointAt(a, t, true)

	a.onKey(key('e'))
	if a.composer.editing != id {
		t.Fatalf("the draft edits %q, want %q", a.composer.editing, id)
	}
	if a.composer.String() != m.Text {
		t.Fatalf("the draft holds %q, want the message being edited", a.composer.String())
	}

	a.onKey(key('!'))
	a.execute(cmdSend)
	if len(*calls) != 1 {
		t.Fatalf("sending an edit made %d requests, want one", len(*calls))
	}
	if got := (*calls)[0].method; got != "message.edit" {
		t.Fatalf("sending an edit used %q", got)
	}
	if got := (*calls)[0].params["message_id"]; got != id {
		t.Fatalf("the edit names %v, want %q", got, id)
	}
	if got := (*calls)[0].params["text"]; got != m.Text+"!" {
		t.Fatalf("the edit carries %v, want the rewritten text", got)
	}
}

// Somebody else's message is not yours to rewrite, and the key that would say
// so has to say it rather than doing something else.
func TestEditingSomebodyElsesMessageIsRefused(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	pointAt(a, t, false)

	a.onKey(key('e'))
	if a.composer.editing != "" {
		t.Fatal("edit took somebody else's message")
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused edit still made %d requests", len(*calls))
	}
	if msg, _, _ := a.toastNow(); msg == "" {
		t.Error("the key did nothing and said nothing")
	}
}

// A refused send answers in the corner and hands the words back. The field is
// where the draft lives, so a reason standing in it is a reason standing where
// the thing it is about should be, and the reader cannot see what to fix.
func TestARefusedSendGivesTheWordsBackAndAnswersInTheCorner(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	var reply proto.ResponseFunc
	a.request = func(_ string, _ proto.Params, done proto.ResponseFunc) { reply = done }

	for _, r := range "the socket blinked" {
		a.onKey(key(r))
	}
	a.execute(cmdSend)
	if reply == nil {
		t.Fatal("the send never reached the daemon")
	}
	reply(nil, &proto.Error{Message: "not connected"})
	a.paint()

	if a.composer.String() != "the socket blinked" {
		t.Fatalf("the draft came back as %q", a.composer.String())
	}
	msg, refused, _ := a.toastNow()
	if !strings.Contains(msg, "not connected") || !refused {
		t.Fatalf("the corner says %q (refused=%v)", msg, refused)
	}
	if got := a.composerRowText(0); !strings.Contains(got, "the socket blinked") {
		t.Fatalf("the field shows %q, want the draft itself", got)
	}
}

// Copy is the one action that never reaches the daemon: the message is already
// here, and OSC 52 is what carries it out of a terminal over ssh.
func TestCopyingAMessageNeverAsksTheDaemon(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	pointAt(a, t, false)

	a.onKey(key('y'))
	if len(*calls) != 0 {
		t.Fatalf("copy asked the daemon: %+v", *calls)
	}
	if got, _, _ := a.toastNow(); !strings.Contains(got, "clipboard") {
		t.Fatalf("copy said %q", got)
	}
}

func TestStarringSendsTheOppositeOfWhatTheMessageIs(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	id, _ := pointAt(a, t, false)

	a.onKey(key('s'))
	if len(*calls) != 1 || (*calls)[0].method != "message.star" {
		t.Fatalf("star made %+v", *calls)
	}
	if got := (*calls)[0].params["message_id"]; got != id {
		t.Fatalf("star names %v, want %q", got, id)
	}
	if got := (*calls)[0].params["starred"]; got != true {
		t.Fatalf("star sends starred=%v for a message with no star", got)
	}
}

// Delete is two different things wearing one word, so the key asks which.
func TestDeleteAsksBeforeItActs(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	id, _ := pointAt(a, t, true)

	a.onKey(key('d'))
	if a.modal.kind != modalConfirm {
		t.Fatalf("delete opened modal %d, want the confirmation", a.modal.kind)
	}
	if len(*calls) != 0 {
		t.Fatal("delete acted before it asked")
	}
	if len(a.modal.selector.items) != 2 {
		t.Fatalf("the confirmation offers %d answers for our own message, want both",
			len(a.modal.selector.items))
	}
	if a.modal.prompt == "" {
		t.Error("the confirmation does not say which message it is about")
	}

	// Escape is an answer too, and it is the one that does nothing.
	a.onKey(arrow(vaxis.KeyEsc))
	if a.modal.kind != modalNone {
		t.Fatal("escape left the confirmation open")
	}
	if len(*calls) != 0 {
		t.Fatalf("escaping the confirmation deleted something: %+v", *calls)
	}

	a.execute(cmdRevoke)
	a.execute(cmdDeleteForMe)
	if len(*calls) != 2 {
		t.Fatalf("the two answers made %d requests, want one each", len(*calls))
	}
	if (*calls)[0].method != "message.revoke" || (*calls)[0].params["message_id"] != id {
		t.Errorf("delete for everyone sent %+v", (*calls)[0])
	}
	if (*calls)[1].method != "message.delete" || (*calls)[1].params["message_id"] != id {
		t.Errorf("delete for me sent %+v", (*calls)[1])
	}
}

// Somebody else's message can only be taken off this device, so there is
// nothing to ask about.
func TestDeletingSomebodyElsesMessageOffersOnlyTheLocalOne(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	records(a)
	pointAt(a, t, false)

	a.onKey(key('d'))
	if len(a.modal.selector.items) != 1 {
		t.Fatalf("the confirmation offers %d answers, want the local one alone",
			len(a.modal.selector.items))
	}
	if got := a.modal.selector.items[0].Command; got != cmdDeleteForMe {
		t.Fatalf("the one answer is %q", got)
	}
}

// A bare letter is an action only while a message is lit. The rest of the time
// it is a letter, which is the rule that lets the actions have letters at all.
func TestWithoutACursorTheActionLettersAreLetters(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	a.paint()

	for _, letter := range []rune{'r', 'e', 'y', 's', 'd'} {
		a.onKey(key(letter))
	}
	if got := a.composer.String(); got != "reysd" {
		t.Fatalf("the draft holds %q, want the letters that were typed", got)
	}
	if len(*calls) != 0 {
		t.Fatalf("typing acted on a message: %+v", *calls)
	}
	if a.composer.targeted() {
		t.Fatal("typing started a reply")
	}
}

// A message can stay lit while the keyboard is somewhere else, but its letters
// do not follow it there. At the chat list a letter is about a chat.
func TestTheMessageLettersStopAtTheChatList(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	pointAt(a, t, false)
	a.setFocus(FocusList)

	a.onKey(key('s'))
	if len(*calls) != 0 {
		t.Fatalf("a letter at the chat list acted on the message: %+v", *calls)
	}
	if a.composer.targeted() {
		t.Fatal("a letter at the chat list started a reply")
	}

	// And the hint line agrees, because it is the same rule read twice.
	a.setFocus(FocusList)
	a.setCursor(a.messages[0].id)
	a.paint()
	if hints := a.hintBarText(); strings.Contains(hints, "r reply") {
		t.Errorf("the chat list hint line %q offers the message actions", hints)
	}
}

// Every action is reachable three ways, and a letter that only works with a
// cursor must still be in the palette with the reason it does not.
func TestMessageActionsAreInThePaletteWithTheirReason(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	a.paint()

	want := map[commandID]bool{
		cmdReply: false, cmdEditMessage: false, cmdCopyMessage: false,
		cmdStar: false, cmdDelete: false, cmdDeleteForMe: false, cmdRevoke: false,
	}
	for _, choice := range a.commandChoices("", false, false) {
		if _, ours := want[choice.Command]; !ours {
			continue
		}
		want[choice.Command] = true
		if choice.Disabled == "" {
			t.Errorf("%s offers itself with nothing to act on", choice.Command)
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("%s is not in the palette, so it is keybind only", id)
		}
	}
}

// The hint line is where the letters are written down before anybody needs
// them, so it has to name the ones that apply and none of the ones that do not.
func TestTheHintLineNamesTheActionsThatApply(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	records(a)
	pointAt(a, t, false)
	a.paint()

	hints := a.hintBarText()
	for _, want := range []string{"r reply", "y copy", "d delete", "esc drop"} {
		if !strings.Contains(hints, want) {
			t.Errorf("the hint line %q does not offer %q", hints, want)
		}
	}
	if strings.Contains(hints, "e edit") {
		t.Errorf("the hint line %q offers an edit of somebody else's message", hints)
	}
}

// hintBarText is what the hint line currently says.
func (a *App) hintBarText() string {
	r := a.layout().HintBar
	out := ""
	for col := r.Col; col < r.Col+r.Width; col++ {
		out += a.vx.Cell(col, r.Row).Grapheme
	}
	return out
}

// composerRowText is one row of the composer pane.
func (a *App) composerRowText(row int) string {
	r := a.layout().Composer
	out := ""
	for col := r.Col; col < r.Col+r.Width; col++ {
		out += a.vx.Cell(col, r.Row+row).Grapheme
	}
	return out
}
