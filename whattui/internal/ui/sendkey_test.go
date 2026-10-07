package ui

import (
	"testing"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// a send whose answer never came back is sent again under the same key, and
// the daemon makes one message of the two
func TestASendAgainAfterALostAnswerIsOneMessage(t *testing.T) {
	a := mockApp(t, "busy", "Vikram", 100, 30)
	var keys []string
	lose := true
	a.request = func(req *v2.Request, cb proto.ResponseFunc) {
		if req.HasSendText() {
			keys = append(keys, req.GetSendText().GetKey())
		}
		if req.HasSendText() && lose {
			// it reaches the daemon; the answer is what goes missing
			lose = false
			a.client.Do(req, func(*v2.Response, *proto.Error) {})
			cb(nil, &proto.Error{Message: "no answer in time"})
			return
		}
		a.client.Do(req, cb)
	}

	const text = "said once, sent twice"
	for _, r := range text {
		a.onKey(key(r))
	}
	a.onKey(arrow(vaxis.KeyEnter))
	if string(a.composer.text) != text {
		t.Fatalf("the draft is %q after the lost answer, want it back", string(a.composer.text))
	}
	a.onKey(arrow(vaxis.KeyEnter))
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("the two sends went with keys %q, want the same one twice", keys)
	}

	c := a.conv()
	count := func() int {
		n := 0
		c.msgs.Read(func(items []view.Item[*v2.MessageRow], _ view.State) {
			for _, it := range items {
				if it.Value.GetText() == text {
					n++
				}
			}
		})
		return n
	}
	waitFor(t, "the message", func() bool { return count() > 0 })
	settle(t, a)
	if n := count(); n != 1 {
		t.Errorf("the chat holds the message %d times", n)
	}

	// other words are another send, under a key of their own
	for _, r := range "something else" {
		a.onKey(key(r))
	}
	a.onKey(arrow(vaxis.KeyEnter))
	if len(keys) != 3 || keys[2] == keys[0] {
		t.Errorf("a different message went with keys %q", keys)
	}
}
