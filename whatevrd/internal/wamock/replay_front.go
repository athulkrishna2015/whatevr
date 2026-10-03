//go:build whatevr_mock

package wamock

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/encoding/protojson"

	"whatevrd/internal/capture"
)

const (
	frontDialWait = 30 * time.Second
	frontRespWait = 30 * time.Second
)

// frontendPlayer sends a capture's frontend requests to the daemon again, one
// recorded connection to one real one. ids the daemon handed out (messages
// it sent, subscriptions) differ in a replay, so every id a recorded response
// carried is mapped to the replayed one before a later request uses it.
type frontendPlayer struct {
	r      *replay
	socket string

	conns map[int64]*frontConn
	// recorded responses by connection and request id
	resps map[frontKey]json.RawMessage
	ids   map[string]string
	subs  map[string]string
}

type frontKey struct {
	conn int64
	id   string
}

type frontConn struct {
	nc      net.Conn
	mu      sync.Mutex
	waiting map[string]chan json.RawMessage
}

func newFrontendPlayer(r *replay, socket string, recs []capture.Record) *frontendPlayer {
	p := &frontendPlayer{
		r: r, socket: socket,
		conns: map[int64]*frontConn{},
		resps: map[frontKey]json.RawMessage{},
		ids:   map[string]string{},
		subs:  map[string]string{},
	}
	for _, rec := range recs {
		f := rec.Frontend
		if f == nil || f.Dir != capture.FrontendResp {
			continue
		}
		var fr v2.Frame
		if protojson.Unmarshal(f.Line, &fr) != nil || !fr.HasResponse() {
			continue
		}
		k := frontKey{f.Conn, strconv.FormatUint(fr.GetResponse().GetId(), 10)}
		if _, seen := p.resps[k]; !seen {
			p.resps[k] = f.Line
		}
	}
	return p
}

func (p *frontendPlayer) play(ctx context.Context, f *capture.Frontend) {
	switch f.Dir {
	case capture.FrontendOpen:
		p.open(ctx, f.Conn)
	case capture.FrontendClose:
		if c := p.conns[f.Conn]; c != nil {
			c.nc.Close()
			delete(p.conns, f.Conn)
		}
	case capture.FrontendReq:
		p.request(ctx, f)
	}
}

func (p *frontendPlayer) open(ctx context.Context, id int64) {
	if p.socket == "" {
		p.r.log().Warn().Msg("replay has no daemon socket, frontend requests are skipped")
		return
	}
	deadline := time.Now().Add(frontDialWait)
	for {
		nc, err := net.Dial("unix", p.socket)
		if err == nil {
			c := &frontConn{nc: nc, waiting: map[string]chan json.RawMessage{}}
			p.conns[id] = c
			go c.read()
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			p.r.log().Warn().Err(err).Int64("conn", id).Msg("replay could not reach the daemon socket")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (c *frontConn) read() {
	br := bufio.NewReader(c.nc)
	opts := protodelim.UnmarshalOptions{MaxSize: 64 << 20}
	for {
		var fr v2.Frame
		if opts.UnmarshalFrom(br, &fr) != nil {
			break
		}
		if !fr.HasResponse() {
			continue
		}
		id := strconv.FormatUint(fr.GetResponse().GetId(), 10)
		c.mu.Lock()
		ch := c.waiting[id]
		delete(c.waiting, id)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		if j, err := capture.FrameJSON.Marshal(&fr); err == nil {
			ch <- j
		} else {
			close(ch)
		}
	}
	c.mu.Lock()
	for id, ch := range c.waiting {
		close(ch)
		delete(c.waiting, id)
	}
	c.mu.Unlock()
}

func (p *frontendPlayer) request(ctx context.Context, f *capture.Frontend) {
	c := p.conns[f.Conn]
	if c == nil {
		return
	}
	// a line that is no protocol 2 request (an older capture's) is skipped
	var fr v2.Frame
	if protojson.Unmarshal(f.Line, &fr) != nil || !fr.HasRequest() {
		return
	}
	var req map[string]any
	d := json.NewDecoder(bytes.NewReader(f.Line))
	d.UseNumber()
	if d.Decode(&req) != nil {
		return
	}
	line, _ := json.Marshal(p.remap(req, "", 0))
	fr.Reset()
	if err := protojson.Unmarshal(line, &fr); err != nil {
		p.r.log().Warn().Err(err).Int64("conn", f.Conn).Msg("replay frontend remap")
		return
	}
	key := strconv.FormatUint(fr.GetRequest().GetId(), 10)
	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.waiting[key] = ch
	c.mu.Unlock()
	if _, err := protodelim.MarshalTo(c.nc, &fr); err != nil {
		p.r.log().Warn().Err(err).Int64("conn", f.Conn).Msg("replay frontend write")
		return
	}
	select {
	case got, ok := <-ch:
		if ok {
			p.learn(p.resps[frontKey{f.Conn, key}], got)
		}
	case <-time.After(frontRespWait):
		p.r.log().Warn().Int64("conn", f.Conn).Str("req", key).Msg("replay frontend request never answered")
	case <-ctx.Done():
	}
}

// learn walks the recorded and the replayed response side by side and keeps
// every id that came out different.
func (p *frontendPlayer) learn(recorded, got json.RawMessage) {
	if len(recorded) == 0 {
		return
	}
	var a, b any
	da := json.NewDecoder(bytes.NewReader(recorded))
	da.UseNumber()
	db := json.NewDecoder(bytes.NewReader(got))
	db.UseNumber()
	if da.Decode(&a) != nil || db.Decode(&b) != nil {
		return
	}
	p.walk(a, b, "", 0)
}

func idField(k string) bool {
	return k == "id" || k == "ids" || strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_ids")
}

// walk's depth 1 is inside {"response": ...}, where id is the request's own.
func (p *frontendPlayer) walk(a, b any, key string, depth int) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return
		}
		for k, v := range av {
			if depth == 1 && k == "id" {
				continue
			}
			p.walk(v, bv[k], k, depth+1)
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return
		}
		for i := 0; i < len(av) && i < len(bv); i++ {
			p.walk(av[i], bv[i], key, depth)
		}
	case string:
		bs, ok := b.(string)
		switch {
		case !ok || bs == av:
		case key == "sub":
			p.subs[av] = bs
		case idField(key):
			p.ids[av] = bs
		}
	}
}

// remap swaps every recorded id in a request for the replayed one. the
// request's own id stays: the replayer picks the same ones.
func (p *frontendPlayer) remap(v any, key string, depth int) any {
	switch tv := v.(type) {
	case map[string]any:
		for k, inner := range tv {
			if depth != 1 || k != "id" {
				tv[k] = p.remap(inner, k, depth+1)
			}
		}
		return tv
	case []any:
		for i := range tv {
			tv[i] = p.remap(tv[i], key, depth)
		}
		return tv
	case string:
		m, ok := p.ids[tv]
		if key == "sub" {
			m, ok = p.subs[tv]
		}
		if ok {
			return m
		}
	}
	return v
}

func (k frontKey) String() string { return fmt.Sprintf("%d/%s", k.conn, k.id) }
