package model

import (
	"slices"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// contextOf finds the ContextInfo of whatever the message is: every content
// type carries it in a field of that name.
func contextOf(m *waE2E.Message) *waE2E.ContextInfo {
	var out *waE2E.ContextInfo
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() || silent[string(fd.Name())] {
			return true
		}
		sub := v.Message()
		ci := sub.Descriptor().Fields().ByName("contextInfo")
		if ci == nil || !sub.Has(ci) {
			return true
		}
		if c, ok := sub.Get(ci).Message().Interface().(*waE2E.ContextInfo); ok {
			out = c
			return false
		}
		return true
	})
	return out
}

// searchFields are the string fields that hold words a person wrote or would
// look for, wherever they sit in a message.
var searchFields = map[string]bool{
	"conversation": true, "text": true, "caption": true, "fileName": true, "title": true,
	"name": true, "address": true, "displayName": true, "description": true, "optionName": true,
	"groupName": true, "body": true, "footer": true, "contentText": true, "footerText": true,
	"selectedDisplayText": true, "hydratedContentText": true, "hydratedTitleText": true,
}

// searchText is what a search over messages matches this one by.
func searchText(m *waE2E.Message) string {
	var parts []string
	type field struct {
		fd protoreflect.FieldDescriptor
		v  protoreflect.Value
	}
	var walk func(protoreflect.Message, int)
	walk = func(pm protoreflect.Message, depth int) {
		if depth > 4 {
			return
		}
		// the set fields only (a message type has a hundred), in field order,
		// so the text comes out the same every time
		var buf [8]field
		set := buf[:0]
		pm.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			if !fd.IsExtension() {
				set = append(set, field{fd, v})
			}
			return true
		})
		slices.SortFunc(set, func(a, b field) int { return a.fd.Index() - b.fd.Index() })
		for _, f := range set {
			fd, v := f.fd, f.v
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
	// valid utf-8, so sqlite's LIKE and the trigram index read it the same
	return strings.ToValidUTF8(strings.Join(parts, "\n"), "\uFFFD")
}
