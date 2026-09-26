package proto

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Do sends a command. cb fires exactly once, on the client goroutine, with
// either a result or an error, and may be nil for fire-and-forget. A request
// made before the handshake completes is queued, not dropped.
//
// Nothing here returns data to render: a command's result carries ids for
// correlation, and what you draw arrives through the views (rule 2).
func (c *Client) Do(method string, params Params, cb ResponseFunc) {
	if err := c.write(method, params, cb); err != nil && cb != nil {
		cb(nil, &Error{Code: ErrInternal, Message: err.Error()})
	}
}

// Subscribe opens a view. The subscription is live immediately: extends may be
// issued before the daemon answers, and it re-issues itself across reconnects.
func (c *Client) Subscribe(view string, params Params, sink ViewSink) *Subscription {
	if params == nil {
		params = Params{}
	}
	sub := &Subscription{client: c, view: view, params: params, sink: sink}

	c.mu.Lock()
	c.subs = append(c.subs, sub)
	ready := c.state == Ready
	c.mu.Unlock()

	if ready {
		c.issue(sub)
	}
	return sub
}

// issue sends the subscribe for sub and wires the response to its sub id.
func (c *Client) issue(sub *Subscription) {
	params := Params{"view": sub.view}
	for k, v := range sub.params {
		params[k] = v
	}

	c.Do("subscribe", params, func(result json.RawMessage, err *Error) {
		if err != nil {
			if sub.OnFailed != nil {
				sub.OnFailed(err)
			}
			return
		}

		var meta map[string]any
		if e := json.Unmarshal(result, &meta); e != nil {
			return
		}
		raw, ok := meta["sub"].(float64)
		if !ok {
			return
		}
		subID := uint64(raw)
		delete(meta, "sub")

		// A subscription closed while its subscribe was in flight leaves the
		// daemon feeding a sub id nobody routes. Compensate rather than leak.
		sub.mu.Lock()
		closed := sub.closed
		sub.mu.Unlock()
		if closed {
			c.Do("unsubscribe", Params{"sub": subID}, nil)
			return
		}

		c.mu.Lock()
		c.bySubID[subID] = sub
		c.mu.Unlock()

		queued := sub.adopt(subID, meta)
		if sub.OnMeta != nil {
			sub.OnMeta(meta)
		}
		for _, p := range queued {
			sub.sendExtend(subID, p.count, p.direction)
		}
	})
}

// forget drops a subscription from re-issue and from sub id routing.
func (c *Client) forget(sub *Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.subs {
		if s == sub {
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			break
		}
	}
	for id, s := range c.bySubID {
		if s == sub {
			delete(c.bySubID, id)
		}
	}
}

func (c *Client) write(method string, params Params, cb ResponseFunc) error {
	if params == nil {
		params = Params{}
	}

	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	body, err := json.Marshal(request{ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode %s: %w", method, err)
	}
	body = append(body, '\n')
	return c.send(id, body, cb, method == "hello")
}

// resend puts a queued request on the wire now that the handshake is done.
func (c *Client) resend(q queuedRequest) { _ = c.send(q.id, q.req, q.cb, true) }

// send writes one encoded request. bypass is for the frames that must not wait
// on the handshake: hello itself, and the queue hello releases.
func (c *Client) send(id uint64, body []byte, cb ResponseFunc, bypass bool) error {
	c.mu.Lock()

	if len(c.pending) >= maxInFlight {
		c.mu.Unlock()
		return errors.New("too many requests in flight")
	}

	if c.state != Ready && !bypass {
		if len(c.queued) >= maxQueued {
			c.mu.Unlock()
			return errors.New("too many requests queued")
		}
		c.queued = append(c.queued, queuedRequest{id: id, req: body, cb: cb})
		if cb != nil {
			c.pending[id] = cb
		}
		c.mu.Unlock()
		return nil
	}

	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return errors.New("not connected")
	}
	if cb != nil {
		c.pending[id] = cb
	}
	_, werr := conn.Write(body)
	if werr != nil {
		delete(c.pending, id)
		c.writeErr = werr
	}
	c.mu.Unlock()

	if werr != nil {
		_ = conn.Close()
		return werr
	}
	return nil
}
