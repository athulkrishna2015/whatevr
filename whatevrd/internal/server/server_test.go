package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/reflect/protoreflect"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
)

// fakeList is a view over a slice of chat rows a test changes by hand.
type fakeList struct {
	mu    sync.Mutex
	items []string
	gen   int
	fail  bool
}

func (l *fakeList) set(items ...string) {
	l.mu.Lock()
	l.items = items
	l.gen++
	l.mu.Unlock()
}

type listWin struct{ l *fakeList }

func (l *fakeList) Open(context.Context, *Session, *v2.Subscribe) (Window, *v2.SubscribeResult, error) {
	return listWin{l}, nil, nil
}

func (w listWin) Items(_ context.Context, max int) ([]*v2.Upsert, error) {
	w.l.mu.Lock()
	defer w.l.mu.Unlock()
	if w.l.fail {
		return nil, fmt.Errorf("no")
	}
	var out []*v2.Upsert
	for i, id := range w.l.items {
		if max > 0 && i >= max {
			break
		}
		u := v2.Upsert_builder{Id: id, Sort: []byte(fmt.Sprintf("%04d", i)),
			Chat: v2.ChatRow_builder{Id: id, Name: id + fmt.Sprint(w.l.gen)}.Build()}.Build()
		out = append(out, u)
	}
	return out, nil
}

func (listWin) Wake(c core.Change) bool { return len(c.Keys["chat"]) > 0 }
func (listWin) Close()                  {}
func (listWin) ReplacedBy(id string) string {
	if id == "old" {
		return "new"
	}
	return ""
}

type client struct {
	t  *testing.T
	nc net.Conn
	r  *bufio.Reader
	id uint64
}

func start(t *testing.T, l *fakeList) (*Server, *client) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s", "d.sock")
	s, err := New(Options{SocketPath: path, Version: "test", Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	s.View(protoreflect.FieldNumber(v2.Subscribe_Chats_case), l)
	s.HandleInline(protoreflect.FieldNumber(v2.Request_DaemonReconnect_case), func(context.Context, *Session, *v2.Request) (*v2.Response, error) {
		return nil, nil
	})
	s.Handle(protoreflect.FieldNumber(v2.Request_ChatPin_case), func(context.Context, *Session, *v2.Request) (*v2.Response, error) {
		return nil, Errorf(v2.ErrorCode_ERROR_CODE_REJECTED, "nope")
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); <-s.Err() })
	s.Serve(ctx)
	nc, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { nc.Close() })
	return s, &client{t: t, nc: nc, r: bufio.NewReader(nc)}
}

func (c *client) send(set func(*v2.Request)) uint64 {
	c.t.Helper()
	c.id++
	r := &v2.Request{}
	r.SetId(c.id)
	set(r)
	f := &v2.Frame{}
	f.SetRequest(r)
	if _, err := protodelim.MarshalTo(c.nc, f); err != nil {
		c.t.Fatal(err)
	}
	return c.id
}

func (c *client) read() *v2.Frame {
	c.t.Helper()
	_ = c.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	var f v2.Frame
	if err := protodelim.UnmarshalFrom(c.r, &f); err != nil {
		c.t.Fatal(err)
	}
	return &f
}

func (c *client) hello() {
	c.t.Helper()
	c.send(func(r *v2.Request) { r.SetHello(v2.Hello_builder{Client: "t", Protocol: 2}.Build()) })
	h := c.read().GetResponse().GetHello()
	if h.GetProtocol() != 2 || !slices.Contains(h.GetFeatures(), "chats") || !slices.Contains(h.GetFeatures(), "daemon_reconnect") {
		c.t.Fatalf("hello %v", h)
	}
}

func (c *client) update() *v2.ViewUpdate {
	c.t.Helper()
	u := c.read().GetEvent().GetUpdate()
	if u == nil {
		c.t.Fatal("not an update")
	}
	return u
}

func ids(u *v2.ViewUpdate) (up, rm []string) {
	for _, ch := range u.GetChanges() {
		if ch.HasUpsert() {
			up = append(up, ch.GetUpsert().GetId())
		} else {
			rm = append(rm, ch.GetRemove().GetId()+">"+ch.GetRemove().GetReplacedBy())
		}
	}
	return
}

func TestHelloFirst(t *testing.T) {
	_, c := start(t, &fakeList{})
	c.send(func(r *v2.Request) { r.SetDaemonReconnect(&v2.DaemonReconnect{}) })
	if e := c.read().GetResponse().GetError(); e.GetCode() != v2.ErrorCode_ERROR_CODE_INVALID_REQUEST {
		t.Fatalf("before hello: %v", e)
	}
	c.hello()
	c.send(func(r *v2.Request) { r.SetDaemonReconnect(&v2.DaemonReconnect{}) })
	if !c.read().GetResponse().HasDone() {
		t.Fatal("no done")
	}
	c.send(func(r *v2.Request) { r.SetChatPin(&v2.ChatPin{}) })
	if e := c.read().GetResponse().GetError(); e.GetCode() != v2.ErrorCode_ERROR_CODE_REJECTED || e.GetMessage() != "nope" {
		t.Fatalf("error %v", e)
	}
	c.send(func(r *v2.Request) { r.SetChatMute(&v2.ChatMute{}) })
	if e := c.read().GetResponse().GetError(); e.GetCode() != v2.ErrorCode_ERROR_CODE_UNKNOWN_METHOD {
		t.Fatalf("unserved %v", e)
	}
}

func TestWrongProtocolCloses(t *testing.T) {
	_, c := start(t, &fakeList{})
	c.send(func(r *v2.Request) { r.SetHello(v2.Hello_builder{Protocol: 1}.Build()) })
	if c.read().GetResponse().GetError().GetCode() != v2.ErrorCode_ERROR_CODE_INVALID_REQUEST {
		t.Fatal("protocol 1 taken")
	}
	_ = c.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.r.ReadByte(); err == nil {
		t.Fatal("still open")
	}
}

func subscribe(c *client, limit uint32) uint64 {
	c.t.Helper()
	c.send(func(r *v2.Request) {
		s := &v2.Subscribe{}
		s.SetLimit(limit)
		s.SetChats(&v2.ChatsView{})
		r.SetSubscribe(s)
	})
	return c.read().GetResponse().GetSubscribe().GetSub()
}

func TestWindowFillsThenFollows(t *testing.T) {
	l := &fakeList{}
	l.set("a", "b", "c")
	s, c := start(t, l)
	c.hello()
	sub := subscribe(c, 2)
	u := c.update()
	up, _ := ids(u)
	if u.GetSub() != sub || !slices.Equal(up, []string{"a", "b"}) || !u.HasReady() || u.GetReady().GetExhausted() {
		t.Fatalf("fill %v", u)
	}
	// a fold changed b and a merge folded old into new
	l.set("a", "old")
	s.Changed(core.Change{Keys: map[string][]string{"chat": {"x"}}})
	u = c.update()
	up, rm := ids(u)
	if !slices.Equal(up, []string{"a", "old"}) || !slices.Equal(rm, []string{"b>"}) || u.HasReady() {
		t.Fatalf("change %v %v %v", up, rm, u)
	}
	l.set("new")
	s.Changed(core.Change{Keys: map[string][]string{"chat": {"x"}}})
	u = c.update()
	up, rm = ids(u)
	slices.Sort(rm)
	if !slices.Equal(up, []string{"new"}) || !slices.Equal(rm, []string{"a>", "old>new"}) {
		t.Fatalf("merge %v %v", up, rm)
	}
	// a change the view doesn't care about wakes nothing, the next one is the extend
	s.Changed(core.Change{Keys: map[string][]string{"person": {"x"}}})
	l.set("new", "b", "c")
	c.send(func(r *v2.Request) {
		r.SetExtend(v2.Extend_builder{Sub: sub, Count: 5, Direction: v2.Direction_DIRECTION_OLDER}.Build())
	})
	if !c.read().GetResponse().HasDone() {
		t.Fatal("extend not done first")
	}
	u = c.update()
	up, _ = ids(u)
	if !slices.Equal(up, []string{"new", "b", "c"}) || !u.GetReady().GetExhausted() {
		t.Fatalf("extend %v", u)
	}
	c.send(func(r *v2.Request) {
		r.SetExtend(v2.Extend_builder{Sub: sub, Count: 5, Direction: v2.Direction_DIRECTION_NEWER}.Build())
	})
	if c.read().GetResponse().GetError().GetCode() != v2.ErrorCode_ERROR_CODE_INVALID_PARAMS {
		t.Fatal("newer on a live edge")
	}
	c.send(func(r *v2.Request) { r.SetUnsubscribe(v2.Unsubscribe_builder{Sub: sub}.Build()) })
	if !c.read().GetResponse().HasDone() {
		t.Fatal("unsubscribe")
	}
	l.set("z")
	s.Changed(core.Change{Keys: map[string][]string{"chat": {"x"}}})
	c.send(func(r *v2.Request) { r.SetDaemonReconnect(&v2.DaemonReconnect{}) })
	if !c.read().GetResponse().HasDone() {
		t.Fatal("an update after unsubscribe")
	}
}

func TestAFailedReadKeepsWhatWasSent(t *testing.T) {
	l := &fakeList{}
	l.set("a")
	s, c := start(t, l)
	c.hello()
	subscribe(c, 0)
	c.update()
	l.mu.Lock()
	l.fail = true
	l.mu.Unlock()
	s.Changed(core.Change{Keys: map[string][]string{"chat": {"x"}}})
	l.mu.Lock()
	l.fail = false
	l.mu.Unlock()
	l.set("a")
	s.Changed(core.Change{Keys: map[string][]string{"chat": {"x"}}})
	u := c.update()
	if up, rm := ids(u); !slices.Equal(up, []string{"a"}) || len(rm) > 0 {
		t.Fatalf("after a failed read %v %v", up, rm)
	}
}

func TestQueueMergesUntilAResponse(t *testing.T) {
	q := newQueue()
	q.addSub(1)
	up := func(ids ...string) *update {
		u := &update{sub: 1, changes: map[string][]byte{}}
		for _, id := range ids {
			u.order = append(u.order, id)
			u.changes[id] = []byte(id + "1")
		}
		return u
	}
	q.pushUpdate(up("a", "b"))
	second := up("b", "c")
	second.changes["b"] = []byte("b2")
	q.pushUpdate(second)
	q.push([]byte("resp"), false, nil)
	q.pushUpdate(up("d"))
	e, _ := q.pop()
	if !slices.Equal(e.upd.order, []string{"a", "b", "c"}) || string(e.upd.changes["b"]) != "b2" {
		t.Fatalf("merged %v", e.upd)
	}
	if e, _ = q.pop(); string(e.frame) != "resp" {
		t.Fatal("response not next")
	}
	if e, _ = q.pop(); !slices.Equal(e.upd.order, []string{"d"}) {
		t.Fatal("merged past a response")
	}
}

func TestQueueOverflowAsksForAReset(t *testing.T) {
	old := maxQueuedChanges
	maxQueuedChanges = 3
	defer func() { maxQueuedChanges = old }()
	q := newQueue()
	q.addSub(1)
	u := &update{sub: 1, changes: map[string][]byte{}}
	for _, id := range []string{"a", "b", "c", "d"} {
		u.order = append(u.order, id)
		u.changes[id] = []byte(id)
	}
	if q.pushUpdate(u) {
		t.Fatal("no overflow")
	}
	if _, ok := q.pop(); ok {
		t.Fatal("kept a dropped update")
	}
	u.reset = true
	if !q.pushUpdate(u) {
		t.Fatal("a reset refused")
	}
}

func TestBigUpdatesSplit(t *testing.T) {
	big := make([]byte, maxFrameBytes/3)
	u := &update{sub: 1, reset: true, ready: true, changes: map[string][]byte{}}
	for _, id := range []string{"a", "b", "c", "d"} {
		u.order = append(u.order, id)
		u.changes[id] = upsertChange(big)
	}
	fs := u.frames()
	if len(fs) < 2 {
		t.Fatalf("%d frames", len(fs))
	}
	for i, f := range fs {
		if len(f) > maxFrameBytes {
			t.Fatalf("frame %d is %d bytes", i, len(f))
		}
	}
}
