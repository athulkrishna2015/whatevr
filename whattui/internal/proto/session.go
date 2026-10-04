package proto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// session runs one connection from dial to disconnect.
func (c *Client) session(ctx context.Context) {
	c.setState(Connecting, nil, nil)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		c.setState(Disconnected, nil, err)
		return
	}
	sctx, stop := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.state = Handshaking
	c.mu.Unlock()
	go c.writer(sctx, conn)

	defer func() {
		stop()
		_ = conn.Close()
		c.teardown()
	}()

	// The daemon gets a bounded window to answer hello. Cleared once it does,
	// because after that silence is just an idle chat list.
	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	c.notifyState(Handshaking, nil, nil)
	if err := c.sendHello(); err != nil {
		c.setState(Disconnected, nil, err)
		return
	}

	frames := newFrameReader(conn)
	for {
		f, err := frames.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("daemon closed the connection")
			}
			c.setState(Disconnected, nil, err)
			return
		}
		if f != nil {
			c.serial.Lock()
			c.dispatch(f)
			c.serial.Unlock()
		}
		if !frames.moreBuffered() && c.OnDrain != nil {
			c.OnDrain()
		}
	}
}

func (c *Client) dispatch(f *v2.Frame) {
	switch f.WhichFrame() {
	case v2.Frame_Response_case:
		c.answer(f.GetResponse())
	case v2.Frame_Event_case:
		ev := f.GetEvent()
		switch ev.WhichEvent() {
		case v2.Event_Update_case:
			c.update(ev.GetUpdate())
		case v2.Event_OpenChat_case:
			if c.OnOpenChat != nil {
				c.OnOpenChat(ev.GetOpenChat())
			}
		case v2.Event_MediaStreamUpdate_case:
			if c.OnMediaStreamUpdate != nil {
				c.OnMediaStreamUpdate(ev.GetMediaStreamUpdate())
			}
		}
	}
}

func (c *Client) update(u *v2.ViewUpdate) {
	c.mu.Lock()
	sub := c.bySub[u.GetSub()]
	c.mu.Unlock()
	if sub == nil {
		return
	}
	sub.sink.Apply(u, sub.takeFresh())
	if u.HasReady() && sub.hooks.OnReady != nil {
		sub.hooks.OnReady(u.GetReady().GetExhausted())
	}
}

func (c *Client) sendHello() error {
	req := &v2.Request{}
	req.SetHello(v2.Hello_builder{Client: c.name, Protocol: ProtocolVersion}.Build())
	err := c.enqueue(req, func(resp *v2.Response, err *Error) {
		if err == nil && resp.GetHello().GetProtocol() != ProtocolVersion {
			err = &Error{Message: fmt.Sprintf("whatevrd speaks protocol %d", resp.GetHello().GetProtocol())}
		}
		if err != nil {
			c.setState(Disconnected, nil, fmt.Errorf("hello: %w", err))
			c.closeConn()
			return
		}
		info := resp.GetHello()

		c.mu.Lock()
		c.info = info
		c.state = Ready
		conn := c.conn
		subs := append([]*Subscription(nil), c.subs...)
		c.mu.Unlock()
		if conn != nil {
			_ = conn.SetReadDeadline(time.Time{})
		}
		// every live subscription goes again before the UI hears it is
		// ready, so a command never lands ahead of the views
		for _, sub := range subs {
			c.issue(sub)
		}
		c.notifyState(Ready, info, nil)
	}, true, nil)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) closeConn() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// teardown fails every request still owed and drops the sub routing. The
// subscriptions themselves survive: they go again on the next hello.
func (c *Client) teardown() {
	c.mu.Lock()
	calls := c.calls
	c.calls = map[uint64]*call{}
	c.queue, c.queuedBytes, c.written = nil, 0, 0
	c.bySub = map[uint64]*Subscription{}
	c.conn = nil
	c.info = nil
	var owed []ResponseFunc
	for _, x := range calls {
		x.timer.Stop()
		if x.cb != nil {
			owed = append(owed, x.cb)
			x.cb = nil
		}
	}
	subs := append([]*Subscription(nil), c.subs...)
	c.mu.Unlock()

	for _, sub := range subs {
		sub.orphan()
	}
	c.serial.Lock()
	defer c.serial.Unlock()
	for _, cb := range owed {
		cb(nil, errLost)
	}
}

func (c *Client) setState(s State, info *v2.HelloResult, err error) {
	c.mu.Lock()
	changed := c.state != s
	c.state = s
	c.mu.Unlock()
	if changed || err != nil {
		c.notifyState(s, info, err)
	}
}

func (c *Client) notifyState(s State, info *v2.HelloResult, err error) {
	if c.OnState != nil {
		c.OnState(s, info, err)
	}
}
