package model

import (
	"math/rand"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// slowSearchText is searchText as it was first written: every field of
// every message type asked whether it is set.
func slowSearchText(m *waE2E.Message) string {
	var parts []string
	var walk func(protoreflect.Message, int)
	walk = func(pm protoreflect.Message, depth int) {
		if depth > 4 {
			return
		}
		fields := pm.Descriptor().Fields()
		for i := range fields.Len() {
			fd := fields.Get(i)
			if !pm.Has(fd) {
				continue
			}
			v := pm.Get(fd)
			name := string(fd.Name())
			switch {
			case name == "contextInfo" || name == "messageContextInfo" || silent[name] && depth == 0:
			case fd.Kind() == protoreflect.StringKind && !fd.IsList() && searchFields[name]:
				if s := strings.TrimSpace(v.String()); s != "" {
					parts = append(parts, s)
				}
			case fd.Kind() == protoreflect.MessageKind && fd.IsList():
				l := v.List()
				for i := range l.Len() {
					walk(l.Get(i).Message(), depth+1)
				}
			case fd.Kind() == protoreflect.MessageKind && !fd.IsMap():
				walk(v.Message(), depth+1)
			}
		}
	}
	walk(m.ProtoReflect(), 0)
	return strings.ToValidUTF8(strings.Join(parts, "\n"), "\uFFFD")
}

// fill sets a random few of pm's fields, strings and messages and lists of
// them, a few levels down.
func fill(rnd *rand.Rand, pm protoreflect.Message, depth int) {
	fields := pm.Descriptor().Fields()
	words := []string{"radhe", " padded ", "", "café", "\xff\xfe bad", "line\nbreak"}
	for range 1 + rnd.Intn(4) {
		fd := fields.Get(rnd.Intn(fields.Len()))
		switch {
		case fd.IsMap():
		case fd.Kind() == protoreflect.StringKind && !fd.IsList():
			pm.Set(fd, protoreflect.ValueOfString(words[rnd.Intn(len(words))]))
		case fd.Kind() == protoreflect.MessageKind && fd.IsList() && depth < 7:
			l := pm.Mutable(fd).List()
			for range 1 + rnd.Intn(2) {
				e := l.NewElement()
				fill(rnd, e.Message(), depth+1)
				l.Append(e)
			}
		case fd.Kind() == protoreflect.MessageKind && depth < 7:
			fill(rnd, pm.Mutable(fd).Message(), depth+1)
		}
	}
}

func TestSearchTextTakesTheSameFields(t *testing.T) {
	rnd := rand.New(rand.NewSource(3))
	nonEmpty := 0
	for range 3000 {
		m := &waE2E.Message{}
		fill(rnd, m.ProtoReflect(), 0)
		got, want := searchText(m), slowSearchText(m)
		if got != want {
			t.Fatalf("%v:\n got %q\nwant %q", m, got, want)
		}
		if got != "" {
			nonEmpty++
		}
	}
	if nonEmpty < 300 {
		t.Fatalf("only %d of 3000 messages had any text, the test checks little", nonEmpty)
	}
}
