//go:build whatevr_mock

package wamock

import (
	"encoding/json"
	"strings"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func TestRequestKeyIgnoresWhatDiffersBetweenRuns(t *testing.T) {
	usync := func(id, sid string, jids ...string) *waBinary.Node {
		var users []waBinary.Node
		for _, j := range jids {
			users = append(users, waBinary.Node{Tag: "user", Attrs: waBinary.Attrs{"jid": j}})
		}
		return &waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"id": id, "xmlns": "usync", "type": "get"}, Content: []waBinary.Node{
			{Tag: "usync", Attrs: waBinary.Attrs{"sid": sid}, Content: []waBinary.Node{{Tag: "list", Content: users}}},
		}}
	}
	a := requestKey(usync("1", "s1", "a@s.whatsapp.net", "b@s.whatsapp.net"))
	b := requestKey(usync("2", "s2", "b@s.whatsapp.net", "a@s.whatsapp.net"))
	if a != b {
		t.Fatalf("same question, two keys:\n%s\n%s", a, b)
	}
	if c := requestKey(usync("1", "s1", "a@s.whatsapp.net")); c == a {
		t.Fatal("different jids, same key")
	}
	bytesA := requestKey(&waBinary.Node{Tag: "query", Content: []byte("one")})
	bytesB := requestKey(&waBinary.Node{Tag: "query", Content: []byte("two")})
	if bytesA == bytesB {
		t.Fatal("content is part of the question")
	}
}

func TestStanzaClass(t *testing.T) {
	for _, c := range []struct {
		node waBinary.Node
		want string
	}{
		{waBinary.Node{Tag: "ack"}, ""},
		{waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"xmlns": "w:profile:picture", "type": "get"}}, ""},
		{waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"xmlns": "w:p", "type": "result"}}, ""},
		{waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"xmlns": "encrypt", "type": "set"}}, "iq/encrypt/set"},
		{waBinary.Node{Tag: "receipt"}, "receipt/delivery"},
		{waBinary.Node{Tag: "receipt", Attrs: waBinary.Attrs{"type": "retry"}}, "receipt/retry"},
		{waBinary.Node{Tag: "ib", Content: []waBinary.Node{{Tag: "offline_batch"}}}, "ib/offline_batch"},
		{waBinary.Node{Tag: "message"}, "message"},
	} {
		if got := stanzaClass(&c.node); got != c.want {
			t.Errorf("%s: got %q want %q", c.node.String(), got, c.want)
		}
	}
}

func TestAnswerBookTakesInOrderThenRepeatsTheLast(t *testing.T) {
	b := newAnswerBook()
	b.add("k", []byte("1"))
	b.add("k", []byte("2"))
	for _, want := range []string{"1", "2", "2"} {
		got, ok := b.take("k")
		if !ok || string(got) != want {
			t.Fatalf("got %q %v, want %q", got, ok, want)
		}
	}
	if _, ok := b.take("other"); ok {
		t.Fatal("answered a question never asked")
	}
}

func TestFrontendIDsCarryOver(t *testing.T) {
	p := &frontendPlayer{ids: map[string]string{}, subs: map[string]json.Number{}}
	p.learn(json.RawMessage(`{"id":3,"result":{"message_id":"OLD","sub":1,"state":"x"}}`),
		json.RawMessage(`{"id":3,"result":{"message_id":"NEW","sub":7,"state":"y"}}`))
	var req map[string]any
	d := json.NewDecoder(strings.NewReader(`{"message_id":"OLD","sub":1,"reply_to":"OLD","state":"x"}`))
	d.UseNumber()
	d.Decode(&req)
	got := p.remap(req, "params").(map[string]any)
	if got["message_id"] != "NEW" || got["reply_to"] != "NEW" || got["sub"] != json.Number("7") {
		t.Fatalf("not remapped: %v", got)
	}
	if got["state"] != "x" {
		t.Fatalf("a field that is not an id was learned: %v", got)
	}
}

func TestSentIDsMapOntoReplayedOnes(t *testing.T) {
	r := &replay{sent: map[string][]string{"a@s.whatsapp.net": {"OLD1", "OLD2"}}, ids: map[string]string{}, newID: map[string]bool{}, ackT: map[string]string{"OLD1": "123"}}
	msg := func(id string) *waBinary.Node {
		return &waBinary.Node{Tag: "message", Attrs: waBinary.Attrs{"id": id, "to": "a@s.whatsapp.net"}}
	}
	r.learnID(msg("NEW1"))
	r.learnID(msg("NEW1")) // a retry
	r.learnID(msg("NEW2"))
	if r.ids["OLD1"] != "NEW1" || r.ids["OLD2"] != "NEW2" {
		t.Fatalf("ids %v", r.ids)
	}
	if ts, ok := r.ackTime("NEW1"); !ok || ts != "123" {
		t.Fatalf("ack time %q %v", ts, ok)
	}
	receipt := &waBinary.Node{Tag: "receipt", Attrs: waBinary.Attrs{"id": "OLD1"}, Content: []waBinary.Node{
		{Tag: "list", Content: []waBinary.Node{{Tag: "item", Attrs: waBinary.Attrs{"id": "OLD2"}}}},
	}}
	if !r.rewriteIDs(receipt) || receipt.Attrs["id"] != "NEW1" || receipt.GetChildren()[0].GetChildren()[0].Attrs["id"] != "NEW2" {
		t.Fatalf("receipt %s", receipt.String())
	}
	if got := string(r.rewritePlaintext([]byte("quoting OLD1"))); got != "quoting NEW1" {
		t.Fatalf("plaintext %q", got)
	}
}
