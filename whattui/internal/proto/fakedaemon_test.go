package proto

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeDaemon is a unix socket that speaks just enough of the protocol to drive
// a client: it hands every request line to a scripted handler and lets a test
// push frames back whenever it likes.
type fakeDaemon struct {
	t    *testing.T
	path string
	ln   net.Listener

	mu       sync.Mutex
	conn     net.Conn
	requests []map[string]any
	gotReq   chan map[string]any
	conns    chan struct{}
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
		gotReq: make(chan map[string]any, 64),
		conns:  make(chan struct{}, 8),
	}
	go d.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return d
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
		select {
		case d.conns <- struct{}{}:
		default:
		}
		go d.read(conn)
	}
}

func (d *fakeDaemon) read(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		var msg map[string]any
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		d.mu.Lock()
		d.requests = append(d.requests, msg)
		d.mu.Unlock()
		select {
		case d.gotReq <- msg:
		default:
		}
	}
}

// nextRequest waits for the next request the client sent.
func (d *fakeDaemon) nextRequest() map[string]any {
	d.t.Helper()
	select {
	case m := <-d.gotReq:
		return m
	case <-time.After(3 * time.Second):
		d.t.Fatal("timed out waiting for a request")
		return nil
	}
}

// expect waits for a request with this method, skipping any others.
func (d *fakeDaemon) expect(method string) map[string]any {
	d.t.Helper()
	for i := 0; i < 32; i++ {
		m := d.nextRequest()
		if m["method"] == method {
			return m
		}
	}
	d.t.Fatalf("never saw a %s request", method)
	return nil
}

// sendLines writes several frames in one socket write, which is how a window
// fill actually arrives and what the batching depends on.
func (d *fakeDaemon) sendLines(objs ...any) {
	d.t.Helper()
	var buf []byte
	for _, o := range objs {
		b, err := json.Marshal(o)
		if err != nil {
			d.t.Fatalf("marshal: %v", err)
		}
		buf = append(append(buf, b...), '\n')
	}
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	if conn == nil {
		d.t.Fatal("no client connected")
	}
	if _, err := conn.Write(buf); err != nil {
		d.t.Fatalf("write: %v", err)
	}
}

func (d *fakeDaemon) sendRaw(b []byte) {
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	if conn != nil {
		_, _ = conn.Write(b)
	}
}

// dropConnection hangs up on the client, which is what a daemon restart looks
// like from here.
func (d *fakeDaemon) dropConnection() {
	d.mu.Lock()
	conn := d.conn
	d.conn = nil
	d.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (d *fakeDaemon) waitForConnection() {
	d.t.Helper()
	select {
	case <-d.conns:
	case <-time.After(5 * time.Second):
		d.t.Fatal("timed out waiting for a connection")
	}
}

// helloOK answers a hello request.
func (d *fakeDaemon) helloOK(req map[string]any) {
	d.sendLines(map[string]any{
		"id": req["id"],
		"result": map[string]any{
			"daemon": "fake", "version": "0.0.0", "protocol": 1, "state": "online",
		},
	})
}
