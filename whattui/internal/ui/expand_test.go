package ui

import (
	"strings"
	"testing"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	gproto "google.golang.org/protobuf/proto"

	"whattui/internal/proto"
)

const whole = "the start of it and then all the rest the daemon left off"

// cut puts the pointed message back as a row the daemon cut short
func cut(a *App, m *v2.MessageRow) *v2.MessageRow {
	row := gproto.Clone(m).(*v2.MessageRow)
	row.SetText("the start of it")
	row.SetTextTruncated(true)
	it, _ := a.conversation.msgs.Get(m.GetId())
	putMsg(a.conversation.msgs, it.Sort, row)
	a.paint()
	return row
}

// answers is a daemon that has the whole text, counting how often it is asked
func answers(a *App) *int {
	asked := 0
	a.request = func(req *v2.Request, cb proto.ResponseFunc) {
		resp := &v2.Response{}
		if req.HasMessageText() {
			asked++
			resp.SetMessageText(v2.MessageTextResult_builder{Text: whole}.Build())
		}
		cb(resp, nil)
	}
	return &asked
}

func TestExpandShowsTheRestUntilTheRowChanges(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	_, m := pointAt(a, t, false)
	row := cut(a, m)
	asked := answers(a)

	if !strings.HasSuffix(a.body(row), "…") || strings.Contains(screenText(a), "the rest") {
		t.Fatalf("a cut row reads %q", a.body(row))
	}
	if !a.execute(cmdExpand) || *asked != 1 {
		t.Fatalf("/expand asked the daemon %d times", *asked)
	}
	a.paint()
	if !strings.Contains(screenText(a), "the rest") {
		t.Error("the transcript does not show the rest")
	}
	a.execute(cmdExpand)
	if *asked != 1 || a.toastText != "that message is already expanded" {
		t.Errorf("/expand again asked %d times and said %q", *asked, a.toastText)
	}

	// a newer row is a different message as far as the expansion knows
	putMsg(a.conversation.msgs, func() string { it, _ := a.conversation.msgs.Get(m.GetId()); return it.Sort }(), gproto.Clone(row).(*v2.MessageRow))
	a.paint()
	if strings.Contains(screenText(a), "the rest") || len(a.expanded) != 0 {
		t.Error("the expansion outlived the row it was for")
	}
}

func TestCopyAndEditTakeTheWholeText(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	_, m := pointAt(a, t, true)
	cut(a, m)
	asked := answers(a)

	a.onKey(key('y'))
	if *asked != 1 || !strings.Contains(a.toastText, "copied 57 characters") {
		t.Errorf("copy asked %d times and said %q", *asked, a.toastText)
	}

	a.onKey(key('e'))
	if *asked != 2 || string(a.composer.text) != whole {
		t.Errorf("edit asked %d times and loaded %q", *asked, string(a.composer.text))
	}
}

func TestExpandIsOnlyForCutMessages(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	pointAt(a, t, false)
	asked := answers(a)
	a.execute(cmdExpand)
	if *asked != 0 || a.toastText != "that message is already whole" {
		t.Errorf("/expand on a whole message asked %d times and said %q", *asked, a.toastText)
	}

	_, m := pointAt(a, t, false)
	cut(a, m)
	without(a, "message_text")
	for _, c := range a.commandChoices("expand", true, false) {
		if c.Command == cmdExpand {
			t.Error("/expand is offered by a daemon without message_text")
		}
	}
}
