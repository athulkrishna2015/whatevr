package proto

import (
	"bufio"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"google.golang.org/protobuf/encoding/protodelim"
)

// fakeDaemon is a unix socket that reads protocol 2 frames with the protobuf
// library's own delimited reader, not the client's, and lets a test answer
// them whenever it likes. hello is answered on its own unless a test takes it.
type fakeDaemon struct {
	t    *testing.T
	path string
	ln   net.Listener

	// hello answers hello; nil leaves it to the test
	hello func(*v2.Request) *v2.Response
	// stall stops reading once hello is answered, which is a wedged daemon
	stall bool

	mu    sync.Mutex
	conn  net.Conn
	reqs  chan *v2.Request
	conns chan net.Conn
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	path := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := &fakeDaemon{
		t: t, path: path, ln: ln,
		hello: helloOK,
		reqs:  make(chan *v2.Request, 256),
		conns: make(chan net.Conn, 8),
	}
	go d.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return d
}

func helloOK(req *v2.Request) *v2.Response {
	return v2.Response_builder{Id: req.GetId(), Hello: v2.HelloResult_builder{
		Daemon: "fake", Protocol: 2, Features: []string{"messages", "send_text"},
	}.Build()}.Build()
}

func (d *fakeDaemon) accept() {
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			return
		}
		d.mu.Lock()
		d.conn = conn
		d.mu.Unlock()
		d.conns <- conn
		go d.read(conn)
	}
}

func (d *fakeDaemon) read(conn net.Conn) {
	r := bufio.NewReader(conn)
	for {
		f := &v2.Frame{}
		if err := (protodelim.UnmarshalOptions{MaxSize: maxFrameBytes}).UnmarshalFrom(r, f); err != nil {
			return
		}
		req := f.GetRequest()
		if req.HasHello() && d.hello != nil {
			d.send(v2.Frame_builder{Response: d.hello(req)}.Build())
			if d.stall {
				return
			}
			continue
		}
		d.reqs <- req
	}
}

// next is the next request the client sent that was not a hello.
func (d *fakeDaemon) next() *v2.Request {
	d.t.Helper()
	select {
	case r := <-d.reqs:
		return r
	case <-time.After(5 * time.Second):
		d.t.Fatal("no request arrived")
		return nil
	}
}

// quiet fails the test if a request arrives within a moment.
func (d *fakeDaemon) quiet() {
	d.t.Helper()
	select {
	case r := <-d.reqs:
		d.t.Fatalf("unexpected request %v", r)
	case <-time.After(50 * time.Millisecond):
	}
}

func (d *fakeDaemon) send(f *v2.Frame) {
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	if _, err := protodelim.MarshalTo(conn, f); err != nil {
		d.t.Logf("send: %v", err)
	}
}

func (d *fakeDaemon) write(b []byte) {
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	_, _ = conn.Write(b)
}

func (d *fakeDaemon) answer(req *v2.Request, set func(*v2.Response)) {
	r := v2.Response_builder{Id: req.GetId()}.Build()
	if set == nil {
		r.SetDone(&v2.Done{})
	} else {
		set(r)
	}
	d.send(v2.Frame_builder{Response: r}.Build())
}

func (d *fakeDaemon) subscribed(req *v2.Request, sub uint64) {
	d.answer(req, func(r *v2.Response) { r.SetSubscribe(v2.SubscribeResult_builder{Sub: sub}.Build()) })
}

func (d *fakeDaemon) update(u v2.ViewUpdate_builder) {
	d.send(v2.Frame_builder{Event: v2.Event_builder{Update: u.Build()}.Build()}.Build())
}

// drop closes the client's connection from the daemon's end.
func (d *fakeDaemon) drop() {
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	_ = conn.Close()
}

// connected waits for the client to dial.
func (d *fakeDaemon) connected() net.Conn {
	d.t.Helper()
	select {
	case c := <-d.conns:
		return c
	case <-time.After(5 * time.Second):
		d.t.Fatal("the client never connected")
		return nil
	}
}
