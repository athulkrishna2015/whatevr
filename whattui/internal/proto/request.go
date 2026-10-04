package proto

import (
	"context"
	"net"
	"slices"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// Do sends a request; the client sets its id. cb fires exactly once and may be
// nil. With no daemon up the request is refused on the spot: nothing waits for
// a socket that may never come back.
//
// A result carries ids for correlation. What to draw arrives through the
// views, queries apart.
func (c *Client) Do(req *v2.Request, cb ResponseFunc) {
	if err := c.enqueue(req, cb, false, nil); err != nil && cb != nil {
		cb(nil, err)
	}
}

// deadlineFor is how long a request gets before its caller is told no.
var deadlineFor = deadline

func deadline(req *v2.Request) time.Duration {
	switch req.WhichMethod() {
	case v2.Request_Hello_case:
		return handshakeTimeout
	case v2.Request_MediaFetchProfilePicture_case, v2.Request_ChatEnsureDirect_case,
		v2.Request_ContactCheckPhone_case, v2.Request_GroupJoinInvite_case:
		// these wait on whatsapp
		return 30 * time.Second
	case v2.Request_SendMedia_case:
		// the daemon copies the file before it answers
		return 2 * time.Minute
	}
	return 5 * time.Second
}

// enqueue queues req for the writer. hello goes before the handshake is done,
// nothing else does.
func (c *Client) enqueue(req *v2.Request, cb ResponseFunc, hello bool, late func(*v2.Response)) *Error {
	id := c.nextID.Add(1)
	req.SetId(id)
	f := &v2.Frame{}
	f.SetRequest(req)
	body, err := encode(f)
	if err != nil {
		return &Error{Message: err.Error()}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || (c.state != Ready && !hello) {
		return errOffline
	}
	if len(c.queue) >= maxQueued || c.queuedBytes+len(body) > maxQueuedBytes {
		return errBusy
	}
	x := &call{id: id, body: body, cb: cb, late: late}
	c.calls[id] = x
	c.queue = append(c.queue, x)
	c.queuedBytes += len(body)
	x.timer = time.AfterFunc(deadlineFor(req), func() { c.expire(x) })
	signal(c.wake)
	return nil
}

// expire tells x's caller no. one still queued never goes out; one written
// keeps its slot until the daemon answers it.
func (c *Client) expire(x *call) {
	c.mu.Lock()
	cb := x.cb
	if c.calls[x.id] != x || cb == nil {
		c.mu.Unlock()
		return
	}
	x.cb = nil
	if !x.written {
		delete(c.calls, x.id)
		if i := slices.Index(c.queue, x); i >= 0 {
			c.queue = slices.Delete(c.queue, i, i+1)
			c.queuedBytes -= len(x.body)
		}
	}
	c.mu.Unlock()

	c.serial.Lock()
	defer c.serial.Unlock()
	cb(nil, errTimeout)
}

// answer hands a response to whoever asked. one past its deadline goes to
// its late hook, or nowhere.
func (c *Client) answer(r *v2.Response) {
	c.mu.Lock()
	x := c.calls[r.GetId()]
	if x == nil {
		c.mu.Unlock()
		return
	}
	delete(c.calls, x.id)
	if x.written {
		c.written--
		signal(c.wake)
	}
	cb := x.cb
	x.cb = nil
	c.mu.Unlock()

	x.timer.Stop()
	switch {
	case cb != nil && r.HasError():
		cb(nil, daemonError(r.GetError()))
	case cb != nil:
		cb(r, nil)
	case x.late != nil:
		x.late(r)
	}
}

// writer puts queued requests on conn, never more than maxAwaiting
// unanswered, until ctx ends or a write fails.
func (c *Client) writer(ctx context.Context, conn net.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		}
		for {
			c.mu.Lock()
			if c.conn != conn || len(c.queue) == 0 || c.written >= maxAwaiting {
				c.mu.Unlock()
				break
			}
			x := c.queue[0]
			c.queue = c.queue[1:]
			c.queuedBytes -= len(x.body)
			x.written = true
			c.written++
			c.mu.Unlock()

			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := conn.Write(x.body); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}
