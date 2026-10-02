//go:build whatevr_mock

package wamock

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

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
	// recorded responses by connection and raw request id
	resps map[frontKey]json.RawMessage
	ids   map[string]string
	subs  map[string]json.Number
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
		subs:  map[string]json.Number{},
	}
	for _, rec := range recs {
		f := rec.Frontend
		if f == nil || f.Dir != capture.FrontendResp {
			continue
		}
		var head struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(f.Line, &head) == nil && len(head.ID) > 0 {
			k := frontKey{f.Conn, string(head.ID)}
			if _, seen := p.resps[k]; !seen {
				p.resps[k] = f.Line
			}
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
	sc := bufio.NewScanner(c.nc)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var head struct {
			ID    json.RawMessage `json:"id"`
			Sub   json.RawMessage `json:"sub"`
			Event json.RawMessage `json:"event"`
		}
		if json.Unmarshal(line, &head) != nil || len(head.ID) == 0 || len(head.Event) > 0 {
			continue
		}
		c.mu.Lock()
		ch := c.waiting[string(head.ID)]
		delete(c.waiting, string(head.ID))
		c.mu.Unlock()
		if ch != nil {
			ch <- append(json.RawMessage(nil), line...)
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
	var req map[string]any
	d := json.NewDecoder(bytes.NewReader(f.Line))
	d.UseNumber()
	if d.Decode(&req) != nil {
		// a line the daemon rejected as garbage, send it as it was
		c.nc.Write(append(append([]byte(nil), f.Line...), '\n'))
		return
	}
	for k, v := range req {
		if k != "id" {
			req[k] = p.remap(v, k)
		}
	}
	line, _ := json.Marshal(req)
	rawID, hasID := req["id"]
	var ch chan json.RawMessage
	var key string
	if hasID {
		idJSON, _ := json.Marshal(rawID)
		key = string(idJSON)
		ch = make(chan json.RawMessage, 1)
		c.mu.Lock()
		c.waiting[key] = ch
		c.mu.Unlock()
	}
	if _, err := c.nc.Write(append(line, '\n')); err != nil {
		p.r.log().Warn().Err(err).Int64("conn", f.Conn).Msg("replay frontend write")
		return
	}
	if ch == nil {
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
	p.walk(a, b, "")
}

func idField(k string) bool {
	return k == "id" || k == "ids" || strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_ids")
}

func (p *frontendPlayer) walk(a, b any, key string) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return
		}
		for k, v := range av {
			if key == "" && k == "id" {
				continue
			}
			p.walk(v, bv[k], k)
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return
		}
		for i := 0; i < len(av) && i < len(bv); i++ {
			p.walk(av[i], bv[i], key)
		}
	case string:
		if bs, ok := b.(string); ok && bs != av && idField(key) {
			p.ids[av] = bs
		}
	case json.Number:
		if bn, ok := b.(json.Number); ok && bn != av && key == "sub" {
			p.subs[av.String()] = bn
		}
	}
}

func (p *frontendPlayer) remap(v any, key string) any {
	switch tv := v.(type) {
	case map[string]any:
		for k, inner := range tv {
			tv[k] = p.remap(inner, k)
		}
		return tv
	case []any:
		for i := range tv {
			tv[i] = p.remap(tv[i], key)
		}
		return tv
	case string:
		if m, ok := p.ids[tv]; ok {
			return m
		}
	case json.Number:
		if key == "sub" {
			if m, ok := p.subs[tv.String()]; ok {
				return m
			}
		}
	}
	return v
}

func (p *frontendPlayer) close() {
	for id, c := range p.conns {
		c.nc.Close()
		delete(p.conns, id)
	}
}

func (k frontKey) String() string { return fmt.Sprintf("%d/%s", k.conn, k.id) }
