package views

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	"whatevrd/internal/status"
)

func TestALongTextComesCutAndWhole(t *testing.T) {
	long := strings.Repeat("radhe ", 20000)
	f := open(t, msg("L", ashaPN, ashaPN, "", false, 1, long))
	_, chats := f.subscribe(func(s *v2.Subscribe) { s.SetChats(&v2.ChatsView{}) })
	if len(chats) != 1 {
		t.Fatalf("%d chats", len(chats))
	}
	if n := proto.Size(chats[0]); n > chatBytes {
		t.Fatalf("chat row is %d bytes", n)
	}
	_, rows := f.subscribe(func(s *v2.Subscribe) {
		s.SetMessages(v2.MessagesView_builder{ChatId: chats[0].GetId()}.Build())
	})
	if len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	row := rows[0].GetMessage()
	if !row.GetTextTruncated() || proto.Size(rows[0]) > messageBytes || !strings.HasPrefix(long, row.GetText()) {
		t.Fatalf("cut %v, %d bytes, text %d bytes", row.GetTextTruncated(), proto.Size(rows[0]), len(row.GetText()))
	}
	id := f.send(func(r *v2.Request) {
		r.SetMessageText(v2.MessageText_builder{MessageId: row.GetId()}.Build())
	})
	resp := f.read().GetResponse()
	for resp == nil || resp.GetId() != id {
		resp = f.read().GetResponse()
	}
	if resp.GetMessageText().GetText() != long {
		t.Fatalf("message_text gave %d bytes of %d", len(resp.GetMessageText().GetText()), len(long))
	}
}

func react(id, target, sender string, sec int, emoji string) core.Input {
	b, err := proto.Marshal(&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
		Key:               &waCommon.MessageKey{RemoteJID: proto.String(grp), ID: proto.String(target), FromMe: proto.Bool(false), Participant: proto.String(boL)},
		Text:              proto.String(emoji),
		SenderTimestampMS: proto.Int64((base.Unix() + int64(sec)) * 1000),
	}})
	if err != nil {
		panic(err)
	}
	return in(core.KindMessage, core.MessageHead{
		Source: core.Source{Chat: grp, Sender: sender, Group: true},
		ID:     id, T: base.Unix() + int64(sec), PushName: "R", Exact: true,
	}, b, sec)
}

func TestReactionsCountEveryoneAndListAFew(t *testing.T) {
	ins := []core.Input{msg("G1", grp, boL, boPN, false, 1, "react to me")}
	for i := range 40 {
		emoji := "👍"
		if i%4 == 0 {
			emoji = "🙏"
		}
		ins = append(ins, react(fmt.Sprint("R", i), "G1", fmt.Sprintf("1000000009%02d@lid", i), 2+i, emoji))
	}
	f := open(t, ins...)
	_, chats := f.subscribe(func(s *v2.Subscribe) { s.SetChats(&v2.ChatsView{}) })
	_, rows := f.subscribe(func(s *v2.Subscribe) {
		s.SetMessages(v2.MessagesView_builder{ChatId: chats[0].GetId()}.Build())
	})
	row := rows[len(rows)-1].GetMessage()
	counts := map[string]uint32{}
	for _, c := range row.GetReactionCounts() {
		counts[c.GetEmoji()] = c.GetCount()
	}
	if len(row.GetReactions()) != listed || counts["👍"] != 30 || counts["🙏"] != 10 {
		t.Fatalf("%d listed, counts %v", len(row.GetReactions()), counts)
	}
	if row.GetReactionCounts()[0].GetEmoji() != "👍" {
		t.Fatal("counts not most first")
	}
	_, all := f.subscribe(func(s *v2.Subscribe) {
		s.SetReactions(v2.ReactionsView_builder{MessageId: row.GetId()}.Build())
	})
	if len(all) != 40 {
		t.Fatalf("reactions view has %d of 40", len(all))
	}
}

// anchoredFixture is a messages chat and its reads, for windows built by hand.
func anchoredFixture(t *testing.T, ins ...core.Input) (*fixture, *Reads, string) {
	t.Helper()
	f := open(t, ins...)
	ctx := context.Background()
	ids, err := model.LoadIDs(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	rs := New(Options{Core: f.db, IDs: ids, Live: live.New(), Board: status.New(), MediaDir: t.TempDir(), Log: zerolog.Nop()})
	w, err := rs.World(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ids.Ensure(ctx, w, ashaPN); err != nil {
		t.Fatal(err)
	}
	return f, rs, ids.Of(w, ashaPN)
}

func (rs *Reads) anchoredAt(t *testing.T, chatID string, limit uint32, id string) *anchoredWin {
	t.Helper()
	ctx := context.Background()
	w, err := rs.World(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ch, ok, err := rs.r.ChatIn(ctx, w, ashaPN)
	if err != nil || !ok {
		t.Fatalf("chat: %v %v", ok, err)
	}
	m, ok, err := rs.r.Message(ctx, ch.Addrs, id)
	if err != nil || !ok {
		t.Fatalf("anchor: %v %v", ok, err)
	}
	req := v2.Subscribe_builder{Limit: limit, Messages: v2.MessagesView_builder{ChatId: chatID, MessageId: proto.String(MessageToken(ashaPN, id))}.Build()}.Build()
	return rs.anchored(req, chatID, ch, m)
}

func itemIDs(t *testing.T, w *anchoredWin) []string {
	t.Helper()
	items, err := w.Items(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		_, id, _ := SplitToken(it.GetMessage().GetId())
		out = append(out, id)
	}
	return out
}

func TestAOneMessageWindowHoldsOne(t *testing.T) {
	_, rs, chat := anchoredFixture(t, msg("A", ashaPN, ashaPN, "", false, 1, "before"),
		msg("B", ashaPN, ashaPN, "", false, 2, "anchor"), msg("C", ashaPN, ashaPN, "", false, 3, "after"))
	if got := itemIDs(t, rs.anchoredAt(t, chat, 1, "B")); strings.Join(got, ",") != "B" {
		t.Fatalf("got %v", got)
	}
}

func TestAnAnchoredWindowAtTheLiveEdgeStaysUnderTheCap(t *testing.T) {
	defer func(n int) { anchoredCap = n }(anchoredCap)
	anchoredCap = 5
	var ins []core.Input
	for i, id := range strings.Split("ABCDEFGH", "") {
		ins = append(ins, msg(id, ashaPN, ashaPN, "", false, i+1, id))
	}
	f, rs, chat := anchoredFixture(t, ins...)
	w := rs.anchoredAt(t, chat, 3, "A")
	if got := strings.Join(itemIDs(t, w), ""); got != "AB" {
		t.Fatalf("first %v", got)
	}
	w.Extend(v2.Direction_DIRECTION_NEWER, 10)
	if got := strings.Join(itemIDs(t, w), ""); got != "DEFGH" || w.Size() != 5 {
		t.Fatalf("at the edge %v, size %d", got, w.Size())
	}
	f.feed(msg("I", ashaPN, ashaPN, "", false, 20, "I"))
	if got := strings.Join(itemIDs(t, w), ""); got != "EFGHI" || w.Size() != 5 {
		t.Fatalf("after a new one %v, size %d", got, w.Size())
	}
	// more than a window at once is read as the newest window, not walked
	var burst []core.Input
	for i, id := range strings.Split("JKLMNOP", "") {
		burst = append(burst, msg(id, ashaPN, ashaPN, "", false, 30+i, id))
	}
	f.feed(burst...)
	if got := strings.Join(itemIDs(t, w), ""); got != "LMNOP" || w.Size() != 5 {
		t.Fatalf("after a burst %v, size %d", got, w.Size())
	}
}
