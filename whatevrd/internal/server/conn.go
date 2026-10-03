package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

const (
	// one wedged frontend can't hold a write forever; merging and reset deal
	// with merely slow ones first
	writeTimeout = 30 * time.Second
	writeBuffer  = 64 << 10
	// a peer that never says hello doesn't keep its fd
	helloTimeout = 10 * time.Second
)

type conn struct {
	srv  *Server
	nc   net.Conn
	id   uint64
	log  zerolog.Logger
	q    *queue
	done chan struct{}
	once sync.Once
	ctx  context.Context
	stop context.CancelFunc

	hello bool
	sess  *Session

	subMu   sync.Mutex
	subs    map[uint64]*subscription
	nextSub uint64
	closed  bool
}

func (c *conn) run() {
	defer c.srv.connDone(c)
	defer c.close()
	c.tap("open", nil)
	go c.writeLoop()

	r := bufio.NewReaderSize(c.nc, 64<<10)
	opts := protodelim.UnmarshalOptions{MaxSize: maxFrameBytes}
	_ = c.nc.SetReadDeadline(time.Now().Add(helloTimeout))
	for {
		var f v2.Frame
		if err := opts.UnmarshalFrom(r, &f); err != nil {
			c.readError(err)
			return
		}
		c.tap("in", nil, &f)
		req := f.GetRequest()
		if req == nil {
			c.respond(errorResponse(0, v2.ErrorCode_ERROR_CODE_INVALID_REQUEST, "a frontend sends requests only"), false)
			continue
		}
		c.dispatch(req)
	}
}

func (c *conn) readError(err error) {
	var ne net.Error
	var big *protodelim.SizeTooLargeError
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
	case errors.As(err, &big):
		c.log.Warn().Uint64("size", big.Size).Msg("a frame over the limit closed the connection")
		c.respond(errorResponse(0, v2.ErrorCode_ERROR_CODE_INVALID_REQUEST, "frame over 16 MiB"), true)
	case errors.As(err, &ne) && ne.Timeout() && !c.hello:
		c.log.Warn().Msg("no hello in time")
	default:
		c.log.Warn().Err(err).Msg("connection read")
	}
}

func (c *conn) dispatch(req *v2.Request) {
	n := protoreflect.FieldNumber(req.WhichMethod())
	if n == 0 {
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_UNKNOWN_METHOD, "no method this daemon knows"), false)
		return
	}
	if req.HasHello() {
		c.handleHello(req)
		return
	}
	if !c.hello {
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_INVALID_REQUEST, "the first request must be hello"), false)
		return
	}
	name := methodName(n)
	log := c.log.With().Uint64("req", req.GetId()).Str("method", name).Logger()
	ctx := log.WithContext(context.Background())
	switch {
	case req.HasSubscribe():
		c.subscribe(ctx, req)
		return
	case req.HasExtend():
		c.extend(ctx, req)
		return
	case req.HasUnsubscribe():
		c.unsubscribe(ctx, req)
		return
	}
	m, ok := c.srv.methods[n]
	if !ok {
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_UNKNOWN_METHOD, "this daemon doesn't serve "+name), false)
		return
	}
	log.Info().Msg("command")
	call := func() {
		resp, err := m.fn(ctx, c.sess, req)
		if err != nil {
			e := asError(err)
			ev := log.Info()
			if e.code == v2.ErrorCode_ERROR_CODE_INTERNAL {
				ev = log.Warn()
			}
			ev.Stringer("code", e.code).Str("error", e.msg).Msg("command failed")
			c.respond(errorResponse(req.GetId(), e.code, e.msg), false)
			return
		}
		if resp == nil {
			resp = doneResponse(req.GetId())
		}
		resp.SetId(req.GetId())
		c.respond(resp, false)
	}
	if m.inline {
		call()
		return
	}
	go call()
}

func (c *conn) handleHello(req *v2.Request) {
	h := req.GetHello()
	if c.hello {
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_INVALID_REQUEST, "hello again"), false)
		return
	}
	if h.GetProtocol() != Protocol {
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_INVALID_REQUEST, "this daemon speaks protocol 2"), true)
		return
	}
	c.hello = true
	_ = c.nc.SetReadDeadline(time.Time{})
	c.sess.client = h.GetClient()
	c.log.Info().Str("client", h.GetClient()).Msg("hello")
	resp := &v2.Response{}
	resp.SetId(req.GetId())
	resp.SetHello(v2.HelloResult_builder{
		Daemon:   "whatevrd",
		Version:  c.srv.opts.Version,
		Protocol: Protocol,
		Features: c.srv.features(),
		DataDir:  c.srv.opts.DataDir,
		CacheDir: c.srv.opts.CacheDir,
	}.Build())
	c.respond(resp, false)
	c.srv.sessionChanged()
}

func (c *conn) subscribe(ctx context.Context, req *v2.Request) {
	sr := req.GetSubscribe()
	n := protoreflect.FieldNumber(sr.WhichView())
	v, ok := c.srv.views[n]
	if !ok {
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_UNKNOWN_METHOD, "this daemon doesn't serve that view"), false)
		return
	}
	c.subMu.Lock()
	c.nextSub++
	id := c.nextSub
	c.subMu.Unlock()
	log := zerolog.Ctx(ctx).With().Uint64("sub", id).Str("view", viewName(n)).Logger()
	// opening can read a lot, off the dispatch loop. nothing can name this
	// sub before its response, so nothing overtakes it
	go func() {
		start := time.Now()
		sub := &subscription{id: id, conn: c, log: log, limit: int(sr.GetLimit()), params: sr}
		win, res, err := v.Open(log.WithContext(context.Background()), c.sess, sr)
		if err != nil {
			e := asError(err)
			log.Info().Stringer("code", e.code).Str("error", e.msg).Msg("subscribe failed")
			c.respond(errorResponse(req.GetId(), e.code, e.msg), false)
			return
		}
		sub.win = win
		if d, ok := win.(Sized); ok && sub.limit == 0 {
			sub.limit = d.DefaultLimit()
		}
		if b, ok := win.(Bounded); ok {
			sub.bnd = b
		}
		c.subMu.Lock()
		if c.closed {
			c.subMu.Unlock()
			win.Close()
			return
		}
		c.subs[id] = sub
		c.subMu.Unlock()
		c.q.addSub(id)
		if res == nil {
			res = &v2.SubscribeResult{}
		}
		res.SetSub(id)
		resp := &v2.Response{}
		resp.SetId(req.GetId())
		resp.SetSubscribe(res)
		c.respond(resp, false)
		log.Info().Dur("open", time.Since(start)).Msg("subscribed")
		sub.start()
		c.srv.sessionChanged()
	}()
}

func (c *conn) extend(ctx context.Context, req *v2.Request) {
	e := req.GetExtend()
	sub, ok := c.sub(e.GetSub())
	switch {
	case !ok:
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_NOT_FOUND, "no such subscription"), false)
		return
	case e.GetCount() == 0:
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_INVALID_PARAMS, "count must be positive"), false)
		return
	case e.GetDirection() == v2.Direction_DIRECTION_NEWER && sub.bnd == nil:
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_INVALID_PARAMS, "a live-edge window has no newer side to extend"), false)
		return
	case e.GetDirection() != v2.Direction_DIRECTION_OLDER && e.GetDirection() != v2.Direction_DIRECTION_NEWER:
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_INVALID_PARAMS, "direction must be older or newer"), false)
		return
	}
	c.respond(doneResponse(req.GetId()), false)
	zerolog.Ctx(ctx).Info().Uint64("sub", sub.id).Stringer("direction", e.GetDirection()).Uint32("count", e.GetCount()).Msg("extend")
	sub.extend(e.GetDirection(), int(e.GetCount()))
}

func (c *conn) unsubscribe(ctx context.Context, req *v2.Request) {
	id := req.GetUnsubscribe().GetSub()
	c.subMu.Lock()
	sub, ok := c.subs[id]
	delete(c.subs, id)
	c.subMu.Unlock()
	if !ok {
		c.respond(errorResponse(req.GetId(), v2.ErrorCode_ERROR_CODE_NOT_FOUND, "no such subscription"), false)
		return
	}
	c.q.closeSub(id)
	sub.close()
	c.respond(doneResponse(req.GetId()), false)
	zerolog.Ctx(ctx).Info().Uint64("sub", id).Msg("unsubscribed")
	c.srv.sessionChanged()
}

func (c *conn) sub(id uint64) (*subscription, bool) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	s, ok := c.subs[id]
	return s, ok
}

// windows is every open window on this connection, for wakes and for the
// daemon asking what is on screen.
func (c *conn) windows() []*subscription {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	out := make([]*subscription, 0, len(c.subs))
	for _, s := range c.subs {
		out = append(out, s)
	}
	return out
}

func (c *conn) respond(r *v2.Response, closeAfter bool) {
	f := responseFrame(r)
	c.tap("out", nil, f)
	b, err := marshalFrame(f)
	if err != nil {
		c.log.Error().Err(err).Msg("marshal a response")
		b, _ = marshalFrame(responseFrame(errorResponse(r.GetId(), v2.ErrorCode_ERROR_CODE_INTERNAL, "the daemon could not encode its answer")))
	}
	c.q.push(b, closeAfter, overloaded)
}

// event sends a connection event, open_chat or media_stream_update.
func (c *conn) event(e *v2.Event) {
	f := eventFrame(e)
	c.tap("out", nil, f)
	b, err := marshalFrame(f)
	if err != nil {
		c.log.Error().Err(err).Msg("marshal an event")
		return
	}
	c.q.push(b, false, overloaded)
}

func overloaded() []byte {
	b, _ := marshalFrame(responseFrame(errorResponse(0, v2.ErrorCode_ERROR_CODE_INTERNAL, "too far behind on responses, closing")))
	return b
}

// writeLoop writes queued frames through a buffer, flushing when the queue
// runs dry, so a fill is one or two writes rather than one per frame.
func (c *conn) writeLoop() {
	w := bufio.NewWriterSize(c.nc, writeBuffer)
	write := func(b []byte) bool {
		_ = c.nc.SetWriteDeadline(time.Now().Add(writeTimeout))
		var n [10]byte
		k := putUvarint(n[:], uint64(len(b)))
		if _, err := w.Write(n[:k]); err != nil {
			return false
		}
		_, err := w.Write(b)
		return err == nil
	}
	for {
		e, ok := c.q.pop()
		if !ok {
			if w.Buffered() > 0 {
				_ = c.nc.SetWriteDeadline(time.Now().Add(writeTimeout))
				if w.Flush() != nil {
					c.close()
					return
				}
			}
			select {
			case <-c.q.signal:
				continue
			case <-c.done:
				return
			}
		}
		if e.upd != nil {
			for _, f := range e.upd.frames() {
				if c.srv.opts.Tap != nil {
					c.tapRaw("out", f)
				}
				if !write(f) {
					c.close()
					return
				}
			}
			continue
		}
		if !write(e.frame) {
			c.close()
			return
		}
		if e.closeAfter {
			_ = w.Flush()
			c.close()
			return
		}
	}
}

func putUvarint(b []byte, v uint64) int {
	i := 0
	for v >= 0x80 {
		b[i] = byte(v) | 0x80
		v >>= 7
		i++
	}
	b[i] = byte(v)
	return i + 1
}

func (c *conn) close() {
	c.once.Do(func() {
		c.log.Info().Msg("connection closed")
		c.tap("close", nil)
		c.stop()
		close(c.done)
		_ = c.nc.Close()
		c.subMu.Lock()
		subs := c.subs
		c.subs = map[uint64]*subscription{}
		c.closed = true
		c.subMu.Unlock()
		for _, s := range subs {
			s.close()
		}
		c.srv.sessionChanged()
	})
}

func (c *conn) tap(dir string, raw []byte, f ...*v2.Frame) {
	t := c.srv.opts.Tap
	if t == nil {
		return
	}
	if len(f) > 0 {
		raw, _ = proto.Marshal(f[0])
	}
	t(c.id, dir, raw)
}

func (c *conn) tapRaw(dir string, b []byte) { c.srv.opts.Tap(c.id, dir, b) }
