package whatsapp

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// our parse must read a history message the way whatsmeow's does, minus the
// sender of our own messages, which the row calls "me" anyway
func TestHistoryParseMatchesWhatsmeow(t *testing.T) {
	own := types.NewJID("15550001111", types.DefaultUserServer)
	cli := whatsmeow.NewClient(&store.Device{ID: &own}, nil)
	dm := types.NewJID("15550002222", types.DefaultUserServer)
	grp := types.NewJID("1203630000", types.GroupServer)
	text := &waE2E.Message{Conversation: proto.String("hi")}
	edit := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waCommon.MessageKey{ID: proto.String("ORIG")},
		EditedMessage: &waE2E.Message{Conversation: proto.String("fixed")},
	}}
	key := func(remote string, fromMe bool, participant string) *waCommon.MessageKey {
		k := &waCommon.MessageKey{RemoteJID: proto.String(remote), FromMe: proto.Bool(fromMe), ID: proto.String("ID1")}
		if participant != "" {
			k.Participant = proto.String(participant)
		}
		return k
	}
	cases := []struct {
		name string
		chat types.JID
		web  *waWeb.WebMessageInfo
	}{
		{"dm in", dm, &waWeb.WebMessageInfo{Key: key(dm.String(), false, ""), Message: text, MessageTimestamp: proto.Uint64(1700000000), PushName: proto.String("A")}},
		{"dm out", dm, &waWeb.WebMessageInfo{Key: key(dm.String(), true, ""), Message: text, MessageTimestamp: proto.Uint64(1700000001)}},
		{"group by participant", grp, &waWeb.WebMessageInfo{Key: key(grp.String(), false, ""), Participant: proto.String(dm.String()), Message: text}},
		{"group by key participant", grp, &waWeb.WebMessageInfo{Key: key(grp.String(), false, dm.String()), Message: text}},
		{"no chat given", types.JID{}, &waWeb.WebMessageInfo{Key: key(dm.String(), false, ""), Message: text}},
		{"edit", dm, &waWeb.WebMessageInfo{Key: key(dm.String(), false, ""), Message: edit}},
		{"thread", grp, &waWeb.WebMessageInfo{Key: key(grp.String(), false, dm.String()), Message: text,
			CommentMetadata: &waWeb.CommentMetadata{CommentParentKey: &waCommon.MessageKey{ID: proto.String("P"), Participant: proto.String(dm.String())}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := cli.ParseWebMessage(tc.chat, tc.web)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseWebMessage(tc.chat, tc.web)
			if err != nil {
				t.Fatal(err)
			}
			if want.Info.IsFromMe {
				want.Info.Sender = types.JID{}
			}
			if !reflect.DeepEqual(got.Info, want.Info) {
				t.Fatalf("info\n got %+v\nwant %+v", got.Info, want.Info)
			}
			if !proto.Equal(got.Message, want.Message) {
				t.Fatalf("message\n got %v\nwant %v", got.Message, want.Message)
			}
		})
	}
	if _, err := parseWebMessage(grp, &waWeb.WebMessageInfo{Key: key(grp.String(), false, ""), Message: text}); err == nil {
		t.Fatal("a group message with no sender parsed")
	}
}

// a page decodes the same message every time it is read: the thumbnail is
// written the first time only
func TestThumbnailIsWrittenOnce(t *testing.T) {
	d := newTestDecoder(t)
	path := d.saveMessageThumbnail("chat", "id", []byte("one"))
	if path == "" {
		t.Fatal("not written")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if again := d.saveMessageThumbnail("chat", "id", []byte("one")); again != path {
		t.Fatalf("second write went to %q, not %q", again, path)
	}
	// a rewrite renames a new file over the old one
	if st2, _ := os.Stat(path); !os.SameFile(st, st2) {
		t.Fatal("rewritten")
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, []byte("one")) {
		t.Fatalf("holds %q", b)
	}
	if NewDecoder(testNames{}, "").saveMessageThumbnail("chat", "id", []byte("x")) != "" {
		t.Fatal("wrote with no media dir")
	}
}
