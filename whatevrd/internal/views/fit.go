package views

import (
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// what one item of each kind may take marshalled. a window holds what fits
// 15 MiB of them, see server.WindowCap.
const (
	messageBytes = 60 << 10
	chatBytes    = 4 << 10
	personBytes  = 1 << 10
	rowBytes     = 4 << 10
	typingBytes  = 16 << 10
	objectBytes  = 64 << 10
)

// minCut is the shortest string Fit cuts
const minCut = 256

// names says field n names something rather than saying it.
func names(n protoreflect.Name) bool {
	s := string(n)
	return s == "id" || s == "path" || strings.HasSuffix(s, "_id") || strings.HasSuffix(s, "_path")
}

var messageText = (&v2.MessageRow{}).ProtoReflect().Descriptor().Fields().ByName("text")

// Fit cuts m to at most limit bytes marshalled, the longest words first, again
// and again. a message's text is nearly always that one, and the row says so
// with text_truncated. ids and paths are never cut, nor anything short.
// false is a message no cut gets there.
func Fit(m proto.Message, limit int) bool {
	for {
		over := proto.Size(m) - limit
		if over <= 0 {
			return true
		}
		var at protoreflect.Message
		var fd protoreflect.FieldDescriptor
		n := minCut
		longest(m.ProtoReflect(), func(msg protoreflect.Message, f protoreflect.FieldDescriptor, s string) {
			if len(s) > n {
				at, fd, n = msg, f, len(s)
			}
		})
		if fd == nil {
			return false
		}
		s := at.Get(fd).String()
		k := max(len(s)-over, 0)
		for k > 0 && !utf8.RuneStart(s[k]) {
			k--
		}
		s = s[:k]
		at.Set(fd, protoreflect.ValueOfString(s))
		if fd == messageText {
			row := at.Interface().(*v2.MessageRow)
			row.SetTextTruncated(true)
			// a mention past the cut points at nothing
			runes := uint32(utf8.RuneCountInString(s))
			var kept []*v2.Mention
			for _, x := range row.GetMentions() {
				if x.GetEnd() <= runes {
					kept = append(kept, x)
				}
			}
			row.SetMentions(kept)
		}
	}
}

// longest calls see with every non-empty string field in m, nested ones too.
func longest(m protoreflect.Message, see func(protoreflect.Message, protoreflect.FieldDescriptor, string)) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
		case fd.IsList():
			if fd.Message() != nil {
				l := v.List()
				for i := range l.Len() {
					longest(l.Get(i).Message(), see)
				}
			}
		case fd.Message() != nil:
			longest(v.Message(), see)
		case fd.Kind() == protoreflect.StringKind && !names(fd.Name()):
			see(m, fd, v.String())
		}
		return true
	})
}
