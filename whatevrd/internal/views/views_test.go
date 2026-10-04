package views

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
	"whatevrd/internal/status"
	"whatevrd/internal/whatsapp"
)

const (
	mePN   = "919000000000@s.whatsapp.net"
	meLID  = "200000000000@lid"
	ashaPN = "917770000001@s.whatsapp.net"
	ashaL  = "100000000001@lid"
	boPN   = "917770000002@s.whatsapp.net"
	boL    = "100000000002@lid"
	grp    = "120363000000000001@g.us"
)

var base = time.Now().Add(-time.Hour).Truncate(time.Second)

func in(kind string, h any, body []byte, sec int) core.Input {
	raw, err := json.Marshal(h)
	if err != nil {
		panic(err)
	}
	return core.Input{Kind: kind, V: 1, At: base.Add(time.Duration(sec) * time.Second), Head: raw, Body: body}
}

func msg(id, chat, sender, alt string, fromMe bool, sec int, s string) core.Input {
	b, err := proto.Marshal(&waE2E.Message{Conversation: proto.String(s)})
	if err != nil {
		panic(err)
	}
	push := "Asha"
	if strings.HasSuffix(chat, "g.us") {
		push = "Bobby"
	}
	return in(core.KindMessage, core.MessageHead{
		Source: core.Source{Chat: chat, Sender: sender, SenderAlt: alt, FromMe: fromMe, Group: strings.HasSuffix(chat, "g.us")},
		ID:     id, T: base.Unix() + int64(sec), PushName: push, Exact: true,
	}, b, sec)
}

type fixture struct {
	t  *testing.T
	db *core.DB
	rs *Reads
	nc net.Conn
	r  *bufio.Reader
	id uint64
}

func open(t *testing.T, ins ...core.Input) *fixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var told atomic.Pointer[Reads]
	db, err := core.Open(ctx, filepath.Join(t.TempDir(), "core.db"), core.Options{Domains: model.Domains(), Log: zerolog.Nop(),
		OnChange: func(c core.Change) {
			if rs := told.Load(); rs != nil {
				rs.Tell(c)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db}
	f.feed(ins...)
	ids, err := model.LoadIDs(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	rs := New(Options{Core: db, IDs: ids, Live: live.New(), Board: status.New(), MediaDir: t.TempDir(), Log: zerolog.Nop()})
	path := filepath.Join(t.TempDir(), "s", "d.sock")
	srv, err := server.New(server.Options{SocketPath: path, Version: "test", Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	rs.Register(srv)
	f.rs = rs
	told.Store(rs)
	go rs.Run(ctx)
	srv.Serve(ctx)
	t.Cleanup(func() { cancel(); <-srv.Err(); db.Close() })
	f.nc, err = net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.nc.Close() })
	f.r = bufio.NewReader(f.nc)
	f.send(func(r *v2.Request) { r.SetHello(v2.Hello_builder{Client: "t", Protocol: 2}.Build()) })
	if f.read().GetResponse().GetHello().GetProtocol() != 2 {
		t.Fatal("no hello")
	}
	return f
}

func (f *fixture) feed(ins ...core.Input) {
	f.t.Helper()
	ctx := context.Background()
	var last int64
	for _, x := range ins {
		seq, err := f.db.Append(ctx, x)
		if err != nil {
			f.t.Fatal(err)
		}
		last = seq
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := f.db.WaitFolded(wctx, last); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) send(set func(*v2.Request)) uint64 {
	f.t.Helper()
	f.id++
	r := &v2.Request{}
	r.SetId(f.id)
	set(r)
	fr := &v2.Frame{}
	fr.SetRequest(r)
	if _, err := protodelim.MarshalTo(f.nc, fr); err != nil {
		f.t.Fatal(err)
	}
	return f.id
}

func (f *fixture) read() *v2.Frame {
	f.t.Helper()
	_ = f.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	var fr v2.Frame
	if err := protodelim.UnmarshalFrom(f.r, &fr); err != nil {
		f.t.Fatal(err)
	}
	return &fr
}

// subscribe opens a view and returns it with its first update's rows.
func (f *fixture) subscribe(set func(*v2.Subscribe)) (uint64, []*v2.Upsert) {
	f.t.Helper()
	f.send(func(r *v2.Request) {
		s := &v2.Subscribe{}
		set(s)
		r.SetSubscribe(s)
	})
	r := f.read().GetResponse()
	if e := r.GetError(); e != nil {
		f.t.Fatalf("subscribe: %v", e)
	}
	sub := r.GetSubscribe().GetSub()
	return sub, f.upserts(sub)
}

// upserts is the next update of sub, others skipped.
func (f *fixture) upserts(sub uint64) []*v2.Upsert {
	f.t.Helper()
	u := f.read().GetEvent().GetUpdate()
	for u != nil && u.GetSub() != sub {
		u = f.read().GetEvent().GetUpdate()
	}
	if u == nil {
		f.t.Fatal("not an update")
	}
	var out []*v2.Upsert
	for _, c := range u.GetChanges() {
		if c.HasUpsert() {
			out = append(out, c.GetUpsert())
		}
	}
	return out
}

func scenario() []core.Input {
	return []core.Input{
		in(core.KindLIDMapping, core.LIDMappingHead{LID: meLID, PN: mePN, Self: true}, nil, 0),
		in(core.KindLIDMapping, core.LIDMappingHead{LID: ashaL, PN: ashaPN}, nil, 0),
		in(core.KindLIDMapping, core.LIDMappingHead{LID: boL, PN: boPN}, nil, 0),
		msg("M1", ashaL, ashaL, ashaPN, false, 10, "hi there"),
		msg("M2", ashaPN, mePN, "", true, 11, "hello back"),
		msg("G1", grp, boL, boPN, false, 20, "group news"),
	}
}

func TestChatsListNewestFirstWithPreviews(t *testing.T) {
	f := open(t, scenario()...)
	sub, rows := f.subscribe(func(s *v2.Subscribe) { s.SetChats(&v2.ChatsView{}) })
	if len(rows) != 2 {
		t.Fatalf("chats %d", len(rows))
	}
	g, a := rows[0].GetChat(), rows[1].GetChat()
	if g.GetType() != v2.ChatType_CHAT_TYPE_GROUP || a.GetType() != v2.ChatType_CHAT_TYPE_DIRECT {
		t.Fatalf("types %v %v", g.GetType(), a.GetType())
	}
	if !strings.Contains(g.GetPreview().GetText(), "group news") || !strings.Contains(a.GetPreview().GetText(), "hello back") {
		t.Fatalf("previews %q %q", g.GetPreview().GetText(), a.GetPreview().GetText())
	}
	if rows[0].GetId() == "" || rows[0].GetId() == rows[1].GetId() {
		t.Fatalf("ids %q %q", rows[0].GetId(), rows[1].GetId())
	}

	// a new message in asha's chat takes it to the top
	f.feed(msg("M3", ashaL, ashaL, ashaPN, false, 30, "news"))
	up := f.upserts(sub)
	if len(up) == 0 || up[0].GetId() != rows[1].GetId() || !strings.Contains(up[0].GetChat().GetPreview().GetText(), "news") {
		t.Fatalf("after M3 %v", up)
	}
	if string(up[0].GetSort()) >= string(rows[0].GetSort()) {
		t.Fatal("asha did not move above the group")
	}
}

func TestAPreviewGoesOnlyWithSomeoneItNames(t *testing.T) {
	f := open(t, scenario()...)
	sub, rows := f.subscribe(func(s *v2.Subscribe) { s.SetChats(&v2.ChatsView{}) })
	if g := rows[0].GetChat().GetPreview().GetText(); g != "Bobby: group news" {
		t.Fatalf("group preview %q", g)
	}
	cached := func(key string) bool { _, ok := f.rs.previews.get(key); return ok }
	if !cached(grp) || !cached(ashaL) {
		t.Fatalf("not cached: group %v, asha %v", cached(grp), cached(ashaL))
	}
	touch := func(p string) {
		f.rs.apply(context.Background(), core.Change{Keys: map[string][]string{"person": {p}}})
	}
	touch(boPN)
	if cached(grp) || !cached(ashaL) {
		t.Fatalf("after bo's change: group %v, asha %v", cached(grp), cached(ashaL))
	}
	f.subscribe(func(s *v2.Subscribe) { s.SetChats(&v2.ChatsView{}) })
	touch(ashaPN)
	if !cached(grp) {
		t.Fatal("asha's change dropped a preview that doesn't name her")
	}
	touch("self")
	if cached(grp) {
		t.Fatal("our change kept a preview")
	}

	f.feed(in(core.KindPushName, core.PushNameHead{JID: boL, JIDAlt: boPN, T: base.Unix() + 40, New: "Bo"}, nil, 40))
	for {
		for _, u := range f.upserts(sub) {
			if u.GetId() == rows[0].GetId() && u.GetChat().GetPreview().GetText() == "Bo: group news" {
				return
			}
		}
	}
}

func TestMessagesOneChatUnderBothAddresses(t *testing.T) {
	f := open(t, scenario()...)
	_, chats := f.subscribe(func(s *v2.Subscribe) { s.SetChats(&v2.ChatsView{}) })
	asha := chats[1].GetId()
	sub, rows := f.subscribe(func(s *v2.Subscribe) {
		s.SetMessages(v2.MessagesView_builder{ChatId: asha}.Build())
	})
	var texts []string
	for _, r := range rows {
		texts = append(texts, r.GetMessage().GetText())
	}
	if !slices.Contains(texts, "hi there") || !slices.Contains(texts, "hello back") || len(texts) != 2 {
		t.Fatalf("messages %q", texts)
	}
	for _, r := range rows {
		if _, _, ok := SplitToken(r.GetId()); !ok {
			t.Fatalf("token %q", r.GetId())
		}
	}

	// the open window follows the live edge
	f.feed(msg("M3", ashaPN, ashaL, ashaPN, false, 30, "later"))
	up := f.upserts(sub)
	if len(up) != 1 || up[0].GetMessage().GetText() != "later" || up[0].GetMessage().GetSender().GetId() == "" {
		t.Fatalf("after M3 %v", up)
	}
}

func TestSearchMessagesNamesTheChat(t *testing.T) {
	f := open(t, scenario()...)
	f.send(func(r *v2.Request) { r.SetSearchMessages(v2.SearchMessages_builder{Query: "news"}.Build()) })
	res := f.read().GetResponse().GetSearchMessages()
	if len(res.GetMessages()) != 1 || res.GetMessages()[0].GetChatName() == "" || res.GetMore() {
		t.Fatalf("search %v", res)
	}
}

func TestPreferencesDefaultWhenNoneStored(t *testing.T) {
	f := open(t, scenario()...)
	_, rows := f.subscribe(func(s *v2.Subscribe) { s.SetPreferences(&v2.PreferencesView{}) })
	if len(rows) != 1 || !proto.Equal(rows[0].GetPreferences().GetPreferences(), whatsapp.DefaultPreferences()) {
		t.Fatalf("prefs %v", rows)
	}
}

func TestReceiptsLaterStepImpliesEarlier(t *testing.T) {
	if first(0, 5) != 5 || first(5, 0) != 5 || first(3, 5) != 3 || first(5, 3) != 3 {
		t.Fatal("first")
	}
}
