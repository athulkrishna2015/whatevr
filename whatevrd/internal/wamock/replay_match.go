//go:build whatevr_mock

package wamock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	waBinary "go.mau.fi/whatsmeow/binary"
)

// volatileAttrs differ between two runs that ask the same thing: request ids,
// usync session ids, timestamps taken from the clock.
var volatileAttrs = map[string]bool{"id": true, "sid": true, "t": true}

// requestKey is the shape of a client request with everything that differs
// between runs taken out. children are sorted, so a usync over the same set of
// jids matches whatever order a map gave it.
func requestKey(n *waBinary.Node) string {
	var b strings.Builder
	writeKey(&b, n)
	return b.String()
}

func writeKey(b *strings.Builder, n *waBinary.Node) {
	b.WriteString("<" + n.Tag)
	keys := make([]string, 0, len(n.Attrs))
	for k := range n.Attrs {
		if !volatileAttrs[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, " %s=%v", k, n.Attrs[k])
	}
	b.WriteString(">")
	switch c := n.Content.(type) {
	case []waBinary.Node:
		parts := make([]string, len(c))
		for i := range c {
			var cb strings.Builder
			writeKey(&cb, &c[i])
			parts[i] = cb.String()
		}
		sort.Strings(parts)
		b.WriteString(strings.Join(parts, ""))
	case []byte:
		sum := sha256.Sum256(c)
		b.WriteString(hex.EncodeToString(sum[:8]))
	case string:
		sum := sha256.Sum256([]byte(c))
		b.WriteString(hex.EncodeToString(sum[:8]))
	case nil:
	default:
		fmt.Fprintf(b, "%v", c)
	}
	b.WriteString("</" + n.Tag + ">")
}

// stanzaClass is what ordering counts by: a push waits until the client sent
// as many stanzas of each class as it had when the push was recorded. a query
// or an answer to the server never makes it push anything, it gets an answer
// at most, so those do not count: the daemon makes more or fewer of them
// (avatars, pings) depending on timing.
func stanzaClass(n *waBinary.Node) string {
	attr := func(k string) string { v, _ := n.Attrs[k].(string); return v }
	switch n.Tag {
	case "ack":
		return ""
	case "iq":
		if attr("type") != "set" {
			return ""
		}
		return "iq/" + attr("xmlns") + "/set"
	case "receipt":
		if t := attr("type"); t != "" {
			return "receipt/" + t
		}
		return "receipt/delivery"
	case "presence":
		return "presence/" + attr("type")
	case "ib":
		if c := n.GetChildren(); len(c) > 0 {
			return "ib/" + c[0].Tag
		}
		return "ib"
	}
	return n.Tag
}

// answerBook holds the recorded response to every recorded client request.
// the same request asked again takes the next recorded answer, and the last
// one once they run out.
type answerBook struct {
	mu    sync.Mutex
	byKey map[string][][]byte
	used  map[string]int
}

func newAnswerBook() *answerBook {
	return &answerBook{byKey: map[string][][]byte{}, used: map[string]int{}}
}

func (a *answerBook) add(key string, frame []byte) {
	a.byKey[key] = append(a.byKey[key], frame)
}

func (a *answerBook) take(key string) ([]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := a.byKey[key]
	if len(list) == 0 {
		return nil, false
	}
	i := a.used[key]
	if i >= len(list) {
		i = len(list) - 1
	}
	a.used[key] = i + 1
	return list[i], true
}
