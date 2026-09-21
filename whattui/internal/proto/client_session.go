package proto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// session runs one connection from dial to disconnect. Everything it calls
// runs on this goroutine, which is what lets sinks be lock-free against the
// client.
func (c *Client) session(ctx context.Context) {
	c.setState(Connecting, nil, nil)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		c.setState(Disconnected, nil, err)
		return
	}

	c.mu.Lock()
	c.conn = conn
	c.writeErr = nil
	c.mu.Unlock()

	defer func() {
		_ = conn.Close()
		c.teardown()
	}()

	// The daemon gets a bounded window to answer hello. Cleared once it does,
	// because after that silence is just an idle chat list.
	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	c.setState(Handshaking, nil, nil)

	if err := c.sendHello(); err != nil {
		c.setState(Disconnected, nil, err)
		return
	}

	frames := newLineReader(conn, maxLineBytes)
	for {
		line, err := frames.next()
		if err != nil {
			c.endBatch()
			if errors.Is(err, io.EOF) {
				err = errors.New("daemon closed the connection")
			}
			c.setState(Disconnected, nil, err)
			return
		}
		if len(line) == 0 {
			continue
		}
		c.dispatch(line)
		if !frames.moreBuffered() {
			c.endBatch()
		}
	}
}

func (c *Client) sendHello() error {
	params := Params{"client": c.name, "protocol": ProtocolVersion}
	return c.write("hello", params, func(result json.RawMessage, err *Error) {
		if err != nil {
			c.setState(Disconnected, nil, fmt.Errorf("hello rejected: %w", err))
			c.closeConn()
			return
		}
		var info ServerInfo
		if e := json.Unmarshal(result, &info); e != nil {
			c.setState(Disconnected, nil, fmt.Errorf("hello result: %w", e))
			c.closeConn()
			return
		}

		c.mu.Lock()
		c.info = &info
		c.state = Ready
		conn := c.conn
		subs := append([]*Subscription(nil), c.subs...)
		queued := c.queued
		c.queued = nil
		c.mu.Unlock()

		if conn != nil {
			_ = conn.SetReadDeadline(time.Time{})
		}

		// Every live subscription is re-issued before anything queued goes
		// out, so a command never lands on a connection whose views are not
		// back yet.
		for _, sub := range subs {
			sub.orphan()
			c.issue(sub)
		}
		for _, q := range queued {
			c.resend(q)
		}

		c.notifyState(Ready, &info, nil)
	})
}

func (c *Client) closeConn() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// teardown fails every in-flight request and drops the sub id routing. The
// subscriptions themselves survive: they are re-issued on the next hello.
func (c *Client) teardown() {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[uint64]ResponseFunc)
	c.bySubID = make(map[uint64]*Subscription)
	// The queue goes with the connection. Its callbacks are in pending and are
	// about to fire; leaving the bodies behind would re-send them and fire a
	// second time, and every callback fires exactly once.
	c.queued = nil
	c.conn = nil
	c.info = nil
	c.mu.Unlock()

	gone := &Error{Code: ErrNotConnected, Message: "connection lost"}
	for _, cb := range pending {
		if cb != nil {
			cb(nil, gone)
		}
	}
}

func (c *Client) setState(s State, info *ServerInfo, err error) {
	c.mu.Lock()
	changed := c.state != s
	c.state = s
	c.mu.Unlock()
	if changed || err != nil {
		c.notifyState(s, info, err)
	}
}

func (c *Client) notifyState(s State, info *ServerInfo, err error) {
	if c.OnState != nil {
		c.OnState(s, info, err)
	}
}
