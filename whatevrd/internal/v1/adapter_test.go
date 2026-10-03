package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/store"
)

const (
	mePN   = "919000000000@s.whatsapp.net"
	meLID  = "200000000000@lid"
	ashaPN = "917770000001@s.whatsapp.net"
	ashaL  = "100000000001@lid"
	grp    = "120363000000000001@g.us"
)

var base = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }

func in(kind string, h any, body []byte, when time.Time) core.Input {
	raw, err := json.Marshal(h)
	if err != nil {
		panic(err)
	}
	return core.Input{Kind: kind, V: 1, At: when, Head: raw, Body: body}
}

func pb(m proto.Message) []byte {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

func msgIn(id, chat, sender string, fromMe bool, t int, m *waE2E.Message) core.Input {
	return in(core.KindMessage, core.MessageHead{
		Source: core.Source{Chat: chat, Sender: sender, FromMe: fromMe, Group: strings.HasSuffix(chat, "g.us")},
		ID:     id, T: base.Unix() + int64(t), Exact: true,
	}, pb(m), at(t))
}

func text(s string) *waE2E.Message { return &waE2E.Message{Conversation: proto.String(s)} }

func open(t *testing.T, onChange func(core.Change)) *core.DB {
	t.Helper()
	db, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"),
		core.Options{Domains: model.Domains(), Log: zerolog.Nop(), OnChange: onChange})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func feed(t *testing.T, db *core.DB, ins ...core.Input) {
	t.Helper()
	ctx := context.Background()
	var last int64
	for _, x := range ins {
		seq, err := db.Append(ctx, x)
		if err != nil {
			t.Fatal(err)
		}
		last = seq
	}
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := db.WaitFolded(wctx, last); err != nil {
		t.Fatal(err)
	}
	if f, err := db.FoldFailures(ctx); err != nil || len(f) > 0 {
		t.Fatalf("fold failures %v %v", f, err)
	}
}

// account is asha writing from her number and her lid, a group, and the
// facts that hang off both.
func account() []core.Input {
	key := func(id, chat string, fromMe bool) *waCommon.MessageKey {
		return &waCommon.MessageKey{ID: proto.String(id), RemoteJID: proto.String(chat), FromMe: proto.Bool(fromMe)}
	}
	return []core.Input{
		in(core.KindLIDMapping, core.LIDMappingHead{LID: meLID, PN: mePN, Self: true}, nil, at(0)),
		in(core.KindLIDMapping, core.LIDMappingHead{LID: ashaL, PN: ashaPN}, nil, at(1)),
		in(core.KindAppState, core.AppStateHead{Collection: "critical_unblock_low", Version: 1, Op: "set", Index: []string{"contact", ashaPN}},
			pb(&waSyncAction.SyncActionValue{Timestamp: proto.Int64(at(1).UnixMilli()), ContactAction: &waSyncAction.ContactAction{FullName: proto.String("Asha")}}), at(1)),
		msgIn("A1", ashaPN, ashaPN, false, 10, text("from her number")),
		msgIn("A2", ashaL, ashaL, false, 20, text("from her lid")),
		msgIn("M1", ashaPN, mePN, true, 30, text("mine")),
		msgIn("R1", ashaL, ashaL, false, 31, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
			Key: key("M1", ashaL, true), Text: proto.String("👍"), SenderTimestampMS: proto.Int64(at(31).UnixMilli())}}),
		msgIn("E1", ashaPN, ashaPN, false, 40, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: key("A1", ashaPN, false),
			EditedMessage: text("from her number, edited"), TimestampMS: proto.Int64(at(40).UnixMilli())}}),
		msgIn("A3", ashaL, ashaL, false, 50, text("after my reply")),
		msgIn("G1", grp, ashaL, false, 60, text("in the group")),
		in(core.KindAppState, core.AppStateHead{Collection: "regular_low", Version: 2, Op: "set", Index: []string{"star", grp, "G1", "0", "0"}},
			pb(&waSyncAction.SyncActionValue{Timestamp: proto.Int64(at(61).UnixMilli()), StarAction: &waSyncAction.StarAction{Starred: proto.Bool(true)}}), at(61)),
	}
}

func TestMessageIDsSplitAtTheChat(t *testing.T) {
	for _, c := range []struct{ in, chat, id string }{
		{ashaPN + ":3EB0AA", ashaPN, "3EB0AA"},
		{grp + ":X:Y", grp, "X:Y"},
		{"123:4@lid:ID", "123:4@lid", "ID"},
	} {
		chat, id, ok := SplitMessageID(c.in)
		if !ok || chat != c.chat || id != c.id {
			t.Errorf("SplitMessageID(%q) = %q %q %v", c.in, chat, id, ok)
		}
	}
	if _, _, ok := SplitMessageID("nochat"); ok {
		t.Error("an id without a chat split")
	}
}

func TestV1ReadsOnePersonAsOneChat(t *testing.T) {
	ctx := context.Background()
	db := open(t, nil)
	feed(t, db, account()...)
	a := New(db, nil, nil, nil, zerolog.Nop())

	chats, err := a.ListChatsForView(ctx, store.ChatListFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, c := range chats {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, " ") != grp+" "+ashaPN {
		t.Fatalf("chats %v, want the group then asha by her number", ids)
	}
	asha := chats[1]
	if asha.Name != "Asha" || asha.NameSource != store.ChatNameSourceContact {
		t.Errorf("asha is %q from %q", asha.Name, asha.NameSource)
	}
	// only what came after my own message counts
	if asha.UnreadCount != 1 {
		t.Errorf("unread %d, want 1", asha.UnreadCount)
	}

	msgs, err := a.ListMessages(ctx, ashaPN, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, m := range msgs {
		got = append(got, m.ID)
	}
	want := []string{ashaPN + ":A1", ashaPN + ":A2", ashaPN + ":M1", ashaPN + ":A3"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("transcript %v, want %v (oldest first, both addresses, no fact rows)", got, want)
	}
	if !msgs[0].IsEdited || msgs[0].Text != "from her number, edited" {
		t.Errorf("edit not applied: %+v", msgs[0])
	}
	mine := msgs[2]
	if mine.Direction != store.DirectionOutgoing || mine.SenderID != "me" {
		t.Errorf("own message is %s from %s", mine.Direction, mine.SenderID)
	}
	if len(mine.Reactions) != 1 || mine.Reactions[0].Emoji != "👍" || mine.Reactions[0].SenderName != "Asha" {
		t.Errorf("reaction from her lid onto the number chat: %+v", mine.Reactions)
	}

	one, err := a.GetMessage(ctx, grp+":G1")
	if err != nil {
		t.Fatal(err)
	}
	if !one.IsStarred || one.SenderName != "Asha" {
		t.Errorf("group message %+v", one)
	}
	starred, err := a.ListStarredMessages(ctx, "", 50, "")
	if err != nil || len(starred) != 1 || starred[0].ID != grp+":G1" {
		t.Errorf("starred %+v %v", starred, err)
	}
}

func TestFoldsWakeTheViewsTheyTouched(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var woken atomic.Pointer[Adapter]
	db := open(t, func(c core.Change) {
		if a := woken.Load(); a != nil {
			a.OnChange(c)
		}
	})
	feed(t, db, account()...)
	daemon := app.NewDaemon(app.Paths{})
	a := New(db, nil, nil, daemon, zerolog.Nop())
	woken.Store(a)
	events, stop := daemon.SubscribeDaemonEvents()
	defer stop()
	go a.Run(ctx)

	feed(t, db, msgIn("A4", ashaL, ashaL, false, 70, text("one more")))
	var msg, chat bool
	deadline := time.After(10 * time.Second)
	for !msg || !chat {
		select {
		case e := <-events:
			switch e.Kind {
			case app.DaemonEventMessageUpdated:
				if e.Message.ID == ashaPN+":A4" && e.Message.ChatID == ashaPN {
					msg = true
				}
			case app.DaemonEventChatUpdated:
				if e.Chat.ID == ashaPN {
					chat = true
				}
			}
		case <-deadline:
			t.Fatalf("woke message %v chat %v", msg, chat)
		}
	}
}

// a message deleted for everyone keeps who, when and where, nothing it said
func TestRevokedMessageLosesItsContent(t *testing.T) {
	ctx := context.Background()
	db := open(t, nil)
	key := &waCommon.MessageKey{ID: proto.String("G2"), RemoteJID: proto.String(grp), Participant: proto.String(ashaL)}
	feed(t, db,
		msgIn("G2", grp, ashaL, false, 10, text("oops")),
		msgIn("R2", grp, mePN, true, 11, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
			Key: key, Text: proto.String("😮"), SenderTimestampMS: proto.Int64(at(11).UnixMilli())}}),
		msgIn("X2", grp, ashaL, false, 12, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: key}}),
	)
	m, err := New(db, nil, nil, nil, zerolog.Nop()).GetMessage(ctx, grp+":G2")
	if err != nil || !m.IsRevoked || m.Text != "" || len(m.Reactions) != 0 || m.SenderID != ashaL {
		t.Fatalf("revoked %+v %v", m, err)
	}

	full := store.Message{ID: "c:1", ChatID: "c", SenderID: "s", TimestampUnix: 5, Direction: store.DirectionIncoming,
		Text: "hi", MediaKind: store.MediaKindImage, MediaMimeType: "image/jpeg", MediaWidth: 9, PayloadJSON: "{}",
		ReplyTo: store.MessageReply{MessageID: "c:0"}, Mentions: []store.MessageMention{{JID: "x"}}, IsForwarded: true}
	got := revoked(full)
	want := store.Message{ID: "c:1", ChatID: "c", SenderID: "s", TimestampUnix: 5, Direction: store.DirectionIncoming,
		IsForwarded: true, IsRevoked: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("revoked kept %+v", got)
	}
}

// a number that moved to a new lid is two chats with two ids, never one id
// twice
func TestANumberOnTwoLIDsIsTwoChatIDs(t *testing.T) {
	ctx := context.Background()
	db := open(t, nil)
	second := "100000000002@lid"
	feed(t, db,
		in(core.KindLIDMapping, core.LIDMappingHead{LID: ashaL, PN: ashaPN}, nil, at(1)),
		msgIn("A1", ashaL, ashaL, false, 10, text("on the first lid")),
		in(core.KindLIDMapping, core.LIDMappingHead{LID: second, PN: ashaPN}, nil, at(20)),
		msgIn("A2", second, second, false, 30, text("on the second")),
	)
	chats, err := New(db, nil, nil, nil, zerolog.Nop()).ListChatsForView(ctx, store.ChatListFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, c := range chats {
		if ids[c.ID] {
			t.Fatalf("%s twice in %+v", c.ID, chats)
		}
		ids[c.ID] = true
	}
	if !ids[ashaPN] || !ids[ashaL] {
		t.Fatalf("ids %v, want the number for the lid that has it now and the old lid by itself", ids)
	}
}

// v1's sort key goes by second then chat id; two chats in one second come
// in that order, whatever their milliseconds
func TestChatsComeInSortKeyOrder(t *testing.T) {
	ctx := context.Background()
	db := open(t, nil)
	feed(t, db,
		msgIn("G1", grp, ashaL, false, 10, text("in the group")),
		in(core.KindOutbox, core.OutboxHead{Op: core.OutboxQueue, Chat: ashaPN, ID: "Q1"}, nil, at(10).Add(500*time.Millisecond)),
	)
	chats, err := New(db, nil, nil, nil, zerolog.Nop()).ListChatsForView(ctx, store.ChatListFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 || chats[0].ID != grp || chats[1].ID != ashaPN {
		t.Fatalf("order %v, want the group (smaller id) first in the shared second", chats)
	}
}

// a window of one takes the chat v1 puts first, not the one the model's
// milliseconds put first
func TestAWindowCutsInSortKeyOrder(t *testing.T) {
	ctx := context.Background()
	db := open(t, nil)
	feed(t, db,
		msgIn("G1", grp, ashaL, false, 10, text("in the group")),
		in(core.KindOutbox, core.OutboxHead{Op: core.OutboxQueue, Chat: ashaPN, ID: "Q1"}, nil, at(10).Add(500*time.Millisecond)),
	)
	chats, err := New(db, nil, nil, nil, zerolog.Nop()).ListChatsForView(ctx, store.ChatListFilter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].ID != grp {
		t.Fatalf("window %v, want the group alone", chats)
	}
}

// a kept preview moves on with its chat, and with a name it shows. the
// test adapter has no decoder, so the text is a placeholder: who sent it
// and which way is what tells previews apart here
func TestPreviewsFollowTheirChats(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var woken atomic.Pointer[Adapter]
	db := open(t, func(c core.Change) {
		if a := woken.Load(); a != nil {
			a.OnChange(c)
		}
	})
	feed(t, db, account()...)
	daemon := app.NewDaemon(app.Paths{})
	a := New(db, nil, nil, daemon, zerolog.Nop())
	woken.Store(a)
	events, stop := daemon.SubscribeDaemonEvents()
	defer stop()
	go a.Run(ctx)
	row := func(id string) store.Chat {
		t.Helper()
		chats, err := a.ListChatsForView(ctx, store.ChatListFilter{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range chats {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("no %s in %v", id, chats)
		return store.Chat{}
	}
	// what a view does: read again on every chat event
	until := func(id string, ok func(store.Chat) bool) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			if ok(row(id)) {
				return
			}
			select {
			case <-events:
			case <-deadline:
				t.Fatalf("%s row %+v", id, row(id))
			}
		}
	}
	until(ashaPN, func(c store.Chat) bool { return c.LastMessageDirection == store.DirectionIncoming })
	if _, ok := a.previews.get(ashaL); !ok {
		t.Fatal("the preview was not kept")
	}
	feed(t, db, msgIn("M2", ashaPN, mePN, true, 70, text("mine again")))
	until(ashaPN, func(c store.Chat) bool { return c.LastMessageDirection == store.DirectionOutgoing })
	until(grp, func(c store.Chat) bool { return strings.HasPrefix(c.LastMessage, "Asha:") })
	feed(t, db, in(core.KindAppState, core.AppStateHead{Collection: "critical_unblock_low", Version: 3, Op: "set", Index: []string{"contact", ashaPN}},
		pb(&waSyncAction.SyncActionValue{Timestamp: proto.Int64(at(80).UnixMilli()), ContactAction: &waSyncAction.ContactAction{FullName: proto.String("Asha R")}}), at(80)))
	until(grp, func(c store.Chat) bool { return strings.HasPrefix(c.LastMessage, "Asha R:") })
}

// a preview read before a drop is not kept after it
func TestAPreviewOlderThanADropIsNotKept(t *testing.T) {
	c := newPreviews()
	gen := c.begin()
	c.drop(ashaPN)
	c.put(gen, ashaL, []string{ashaL, ashaPN}, preview{ok: true, text: "old"})
	if _, ok := c.get(ashaL); ok {
		t.Fatal("kept a preview from before the drop")
	}
	gen = c.begin()
	c.put(gen, ashaL, []string{ashaL, ashaPN}, preview{ok: true, text: "new"})
	if p, ok := c.get(ashaL); !ok || p.text != "new" {
		t.Fatalf("got %+v %v", p, ok)
	}
	c.drop(ashaPN)
	if _, ok := c.get(ashaL); ok {
		t.Fatal("a drop by the chat's number left its preview")
	}
}

// TestPreviewsNeverKeepANameFromBeforeARename: views reading all the time
// while a sender is renamed again and again; each new name has to reach the
// group's preview, none may stick from a read that raced the rename.
func TestPreviewsNeverKeepANameFromBeforeARename(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var woken atomic.Pointer[Adapter]
	db := open(t, func(c core.Change) {
		if a := woken.Load(); a != nil {
			a.OnChange(c)
		}
	})
	feed(t, db, account()...)
	a := New(db, nil, nil, app.NewDaemon(app.Paths{}), zerolog.Nop())
	woken.Store(a)
	go a.Run(ctx)
	preview := func() string {
		chats, err := a.ListChatsForView(ctx, store.ChatListFilter{Limit: 50})
		if err != nil {
			if ctx.Err() == nil {
				t.Error(err)
			}
			return ""
		}
		for _, c := range chats {
			if c.ID == grp {
				return c.LastMessage
			}
		}
		return ""
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for ctx.Err() == nil {
				preview()
			}
		})
	}
	defer wg.Wait()
	defer cancel()
	for i := range 40 {
		name := fmt.Sprintf("Asha %d", i)
		feed(t, db, in(core.KindAppState, core.AppStateHead{Collection: "critical_unblock_low", Version: uint64(10 + i), Op: "set", Index: []string{"contact", ashaPN}},
			pb(&waSyncAction.SyncActionValue{Timestamp: proto.Int64(at(100 + i).UnixMilli()), ContactAction: &waSyncAction.ContactAction{FullName: proto.String(name)}}), at(100+i)))
		deadline := time.Now().Add(5 * time.Second)
		for !strings.HasPrefix(preview(), name+":") {
			if time.Now().After(deadline) {
				t.Fatalf("rename %d: preview stuck at %q", i, preview())
			}
			time.Sleep(time.Millisecond)
		}
	}
}

// a queued send is its body, decoded like any message, as far as the outbox
// got with it
func TestAQueuedSendShowsItsBody(t *testing.T) {
	ctx := context.Background()
	db := open(t, nil)
	queue := func(id, file string, m *waE2E.Message, sec int) core.Input {
		return in(core.KindOutbox, core.OutboxHead{Op: core.OutboxQueue, Chat: ashaPN, ID: id, File: file}, pb(m), at(sec))
	}
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("look"), Mimetype: proto.String("image/png"),
		Width: proto.Uint32(4), Height: proto.Uint32(3)}}
	feed(t, db, append(account(),
		queue("Q1", "", text("hello"), 100),
		queue("Q2", "/tmp/look.png", img, 101),
		in(core.KindOutbox, core.OutboxHead{Op: core.OutboxAttempt, Chat: ashaPN, ID: "Q2", Error: "too big", Final: true}, nil, at(102)),
	)...)
	a := New(db, nil, nil, nil, zerolog.Nop())
	m, err := a.GetMessage(ctx, ashaPN+":Q1")
	if err != nil || m.Text != "hello" || m.Status != store.StatusPending || m.Direction != store.DirectionOutgoing || m.SenderID != "me" {
		t.Fatalf("Q1 %+v %v", m, err)
	}
	m, err = a.GetMessage(ctx, ashaPN+":Q2")
	if err != nil || m.Text != "look" || m.MediaKind != store.MediaKindImage || m.MediaLocalPath != "/tmp/look.png" ||
		m.MediaWidth != 4 || m.Status != store.StatusFailed || m.LastSendError != "too big" || m.SendAttempts != 1 {
		t.Fatalf("Q2 %+v %v", m, err)
	}
}
