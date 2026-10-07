//go:build whatevr_mock

package wamock

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/binary/token"
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

// withID is a recorded frame with its top level id swapped, every other byte
// as the server sent it. decoding and encoding again is not the same frame:
// whatsmeow writes a <0/> as an empty list, which nothing can read back.
func withID(frame []byte, id string) ([]byte, bool) {
	want, err := waBinary.Marshal(waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"id": id}})
	if err != nil {
		return nil, false
	}
	ns, ne, ok := attrSpan(want[1:], "id")
	if !ok {
		return nil, false
	}
	s, e, ok := attrSpan(frame, "id")
	if !ok {
		return nil, false
	}
	out := make([]byte, 0, len(frame)-(e-s)+(ne-ns))
	out = append(out, frame[:s]...)
	out = append(out, want[1+ns:1+ne]...)
	return append(out, frame[e:]...), true
}

// attrSpan is where the value of the top level attribute key sits in frame.
func attrSpan(b []byte, key string) (int, int, bool) {
	if len(b) < 2 {
		return 0, 0, false
	}
	var size, p int
	switch int(b[0]) {
	case token.List8:
		size, p = int(b[1]), 2
	case token.List16:
		if len(b) < 3 {
			return 0, 0, false
		}
		size, p = int(b[1])<<8|int(b[2]), 3
	default:
		return 0, 0, false
	}
	p, ok := valueEnd(b, p)
	if !ok {
		return 0, 0, false
	}
	for range (size - 1) / 2 {
		k := p
		if p, ok = valueEnd(b, p); !ok {
			return 0, 0, false
		}
		name := tokenString(b[k:p])
		v := p
		if p, ok = valueEnd(b, p); !ok {
			return 0, 0, false
		}
		if name == key {
			return v, p, true
		}
	}
	return 0, 0, false
}

// valueEnd is the end of the string or jid that starts at p.
func valueEnd(b []byte, p int) (int, bool) {
	if p >= len(b) {
		return 0, false
	}
	t := int(b[p])
	p++
	end := p
	switch {
	case t == token.ListEmpty:
	case t == token.Binary8:
		if p >= len(b) {
			return 0, false
		}
		end = p + 1 + int(b[p])
	case t == token.Binary20:
		if p+3 > len(b) {
			return 0, false
		}
		end = p + 3 + (int(b[p]&0x0f)<<16 | int(b[p+1])<<8 | int(b[p+2]))
	case t == token.Binary32:
		if p+4 > len(b) {
			return 0, false
		}
		end = p + 4 + int(binary.BigEndian.Uint32(b[p:]))
	case t >= token.Dictionary0 && t <= token.Dictionary3:
		end = p + 1
	case t == token.Nibble8 || t == token.Hex8:
		if p >= len(b) {
			return 0, false
		}
		end = p + 1 + int(b[p]&127)
	case t == token.JIDPair:
		user, ok := valueEnd(b, p)
		if !ok {
			return 0, false
		}
		return valueEnd(b, user)
	case t == token.ADJID:
		return valueEnd(b, p+2)
	case t == token.FBJID, t == token.InteropJID:
		user, ok := valueEnd(b, p)
		if !ok {
			return 0, false
		}
		skip := 2
		if t == token.InteropJID {
			skip = 4
		}
		return valueEnd(b, user+skip)
	case t >= 1 && t < len(token.SingleByteTokens):
	default:
		return 0, false
	}
	return end, end <= len(b)
}

// tokenString is an attribute name as written: a token, or plain bytes.
func tokenString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	t := int(b[0])
	switch {
	case t >= token.Dictionary0 && t <= token.Dictionary3 && len(b) == 2:
		s, _ := token.GetDoubleToken(t-token.Dictionary0, int(b[1]))
		return s
	case t == token.Binary8 && len(b) >= 2:
		return string(b[2:])
	case len(b) == 1 && t >= 1 && t < len(token.SingleByteTokens):
		return token.SingleByteTokens[t]
	}
	return ""
}
