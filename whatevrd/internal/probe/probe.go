// package probe is a plain protocol 2 client for the daemon's own tools: it
// says hello, asks, and keeps every subscribed view's items the way a
// frontend would.
package probe

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"google.golang.org/protobuf/encoding/protodelim"
)

// Item is one row of a view as it stands.
type Item struct {
	Sort []byte
	Row  *v2.Upsert
}

// View is one subscription's items.
type View struct {
	Sub   uint64
	Items map[string]Item
	Ready bool
	// the last ready said nothing further is there to extend into
	Exhausted bool
	// how many windows were filled, one per subscribe and extend
	Readies int
}

// ErrQuiet is a read that saw nothing in time.
var ErrQuiet = errors.New("nothing to read")

type Client struct {
	nc     net.Conn
	frames chan *v2.Frame
	err    error
	next   uint64
	// Views by subscription
	Views map[uint64]*View
	// Events is every event that was not a view update, in order
	Events []*v2.Event
	// OnUpsert, when set, sees every row as it lands
	OnUpsert func(sub uint64, u *v2.Upsert)
}

// Dial connects and says hello, waiting up to wait for the socket to show up.
func Dial(socket, name string, wait time.Duration) (*Client, error) {
	deadline := time.Now().Add(wait)
	for {
		nc, err := net.Dial("unix", socket)
		if err == nil {
			c := &Client{nc: nc, frames: make(chan *v2.Frame, 64), Views: map[uint64]*View{}}
			go c.read()
			_, err := c.Ask(v2.Request_builder{Hello: v2.Hello_builder{Client: name, Protocol: 2}.Build()}.Build(), 30*time.Second)
			if err != nil {
				nc.Close()
				return nil, err
			}
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no daemon at %s: %w", socket, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (c *Client) Close() error { return c.nc.Close() }

// Send writes a request with the next id and returns that id.
func (c *Client) Send(req *v2.Request) (uint64, error) {
	c.next++
	req.SetId(c.next)
	_, err := protodelim.MarshalTo(c.nc, v2.Frame_builder{Request: req}.Build())
	return c.next, err
}

// Ask sends a request and reads until its response, applying whatever else
// came first. an error response comes back as an error.
func (c *Client) Ask(req *v2.Request, wait time.Duration) (*v2.Response, error) {
	id, err := c.Send(req)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		resp, err := c.Read(time.Until(deadline))
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.GetId() != id {
			continue
		}
		if e := resp.GetError(); e != nil {
			return resp, fmt.Errorf("%s: %s", e.GetCode(), e.GetMessage())
		}
		return resp, nil
	}
}

func (c *Client) read() {
	r := bufio.NewReader(c.nc)
	for {
		f := &v2.Frame{}
		if err := (protodelim.UnmarshalOptions{MaxSize: -1}).UnmarshalFrom(r, f); err != nil {
			c.err = err
			close(c.frames)
			return
		}
		c.frames <- f
	}
}

// Read takes one frame, applies it if it is an event, and returns it if it
// is a response. ErrQuiet is nothing within wait.
func (c *Client) Read(wait time.Duration) (*v2.Response, error) {
	var f *v2.Frame
	select {
	case got, ok := <-c.frames:
		if !ok {
			return nil, fmt.Errorf("daemon closed the socket: %w", c.err)
		}
		f = got
	case <-time.After(wait):
		return nil, ErrQuiet
	}
	if f.HasResponse() {
		r := f.GetResponse()
		if s := r.GetSubscribe(); s != nil {
			c.Views[s.GetSub()] = &View{Sub: s.GetSub(), Items: map[string]Item{}}
		}
		return r, nil
	}
	e := f.GetEvent()
	u := e.GetUpdate()
	if u == nil {
		c.Events = append(c.Events, e)
		return nil, nil
	}
	v := c.Views[u.GetSub()]
	if v == nil {
		return nil, nil
	}
	if u.GetReset() {
		v.Items = map[string]Item{}
		v.Ready = false
	}
	for _, ch := range u.GetChanges() {
		if up := ch.GetUpsert(); up != nil {
			v.Items[up.GetId()] = Item{Sort: up.GetSort(), Row: up}
			if c.OnUpsert != nil {
				c.OnUpsert(v.Sub, up)
			}
		} else if rm := ch.GetRemove(); rm != nil {
			delete(v.Items, rm.GetId())
		}
	}
	if u.HasReady() {
		v.Ready = true
		v.Exhausted = u.GetReady().GetExhausted()
		v.Readies++
	}
	return nil, nil
}
