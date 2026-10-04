package proto

import (
	"sync"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// ViewSink is what a subscription's updates go to. The client never looks
// inside an item, so one sink serves every view.
type ViewSink interface {
	// Apply takes one update whole: reset, the changes in order, then ready.
	// fresh says it is the first update of a subscription just issued, which
	// replaces whatever the sink held before, as a reset does.
	Apply(u *v2.ViewUpdate, fresh bool)
}

// Subscription is one live view. It owns the daemon's sub id and re-issues
// itself across reconnects, so a caller subscribes once and never thinks
// about the socket again.
type Subscription struct {
	client *Client
	params *v2.Subscribe
	sink   ViewSink
	hooks  Hooks

	mu      sync.Mutex
	id      uint64
	have    bool
	closed  bool
	fresh   bool
	pending []extend
}

// Hooks is what a subscription tells its owner besides the updates. They are
// fixed when it is made: the client calls them from its own goroutines, and a
// refusal on the spot calls OnFailed before Subscribe returns.
type Hooks struct {
	// OnResult hears each subscribe's answer, anchor_id and all
	OnResult func(*v2.SubscribeResult)
	OnFailed func(*Error)
	// OnExtendFailed fires when an extend is refused. Without it a list
	// that cannot grow spins: the caller's in-flight guard never clears.
	OnExtendFailed func(v2.Direction, *Error)
	// OnReady hears every ready, after the update carrying it is applied
	OnReady func(exhausted bool)
	// OnLost, when set, ends the subscription with its connection instead
	// of issuing it again on the next one, and hears that it did. For a
	// caller that wants to say where the new one starts.
	OnLost func()
}

type extend struct {
	count     uint32
	direction v2.Direction
}

// Subscribe opens a view. params holds the view arm and limit and is not
// touched again. The subscription is live at once: extends may come before
// the daemon answers.
func (c *Client) Subscribe(params *v2.Subscribe, sink ViewSink, hooks Hooks) *Subscription {
	sub := &Subscription{client: c, params: params, sink: sink, hooks: hooks}
	c.mu.Lock()
	c.subs = append(c.subs, sub)
	ready := c.state == Ready
	c.mu.Unlock()
	if ready {
		c.issue(sub)
	}
	return sub
}

// Params is what the subscription asked for.
func (s *Subscription) Params() *v2.Subscribe { return s.params }

func (c *Client) issue(sub *Subscription) {
	req := &v2.Request{}
	req.SetSubscribe(sub.params)
	err := c.enqueue(req, func(resp *v2.Response, err *Error) {
		if err != nil {
			if sub.hooks.OnFailed != nil {
				sub.hooks.OnFailed(err)
			}
			return
		}
		res := resp.GetSubscribe()
		if !sub.adopt(res.GetSub()) {
			c.unsubscribe(res.GetSub())
			return
		}
		c.mu.Lock()
		c.bySub[res.GetSub()] = sub
		c.mu.Unlock()
		if sub.hooks.OnResult != nil {
			sub.hooks.OnResult(res)
		}
		for _, e := range sub.takePending() {
			sub.sendExtend(res.GetSub(), e)
		}
	}, false, func(late *v2.Response) {
		// past its deadline nobody routes it
		if late.HasSubscribe() {
			c.unsubscribe(late.GetSubscribe().GetSub())
		}
	})
	if err != nil && sub.hooks.OnFailed != nil {
		sub.hooks.OnFailed(err)
	}
}

func (c *Client) unsubscribe(id uint64) {
	req := &v2.Request{}
	req.SetUnsubscribe(v2.Unsubscribe_builder{Sub: id}.Build())
	c.Do(req, nil)
}

func (c *Client) forget(sub *Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.subs {
		if s == sub {
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			break
		}
	}
	for id, s := range c.bySub {
		if s == sub {
			delete(c.bySub, id)
		}
	}
}

// Active reports whether the daemon has answered the subscribe and the
// subscription is not closed.
func (s *Subscription) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.have && !s.closed
}

// Extend grows the window. One asked before the subscribe is answered goes
// once it is.
func (s *Subscription) Extend(count int, d v2.Direction) {
	e := extend{count: uint32(count), direction: d}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if !s.have {
		s.pending = append(s.pending, e)
		s.mu.Unlock()
		return
	}
	id := s.id
	s.mu.Unlock()
	s.sendExtend(id, e)
}

func (s *Subscription) sendExtend(id uint64, e extend) {
	req := &v2.Request{}
	req.SetExtend(v2.Extend_builder{Sub: id, Count: e.count, Direction: e.direction}.Build())
	s.client.Do(req, func(_ *v2.Response, err *Error) {
		if err != nil && s.hooks.OnExtendFailed != nil {
			s.hooks.OnExtendFailed(e.direction, err)
		}
	})
}

// Close unsubscribes. Safe to call more than once.
func (s *Subscription) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	id, have := s.id, s.have
	s.mu.Unlock()

	s.client.forget(s)
	if have {
		s.client.unsubscribe(id)
	}
}

// adopt records the daemon's sub id; false for a subscription closed while
// its subscribe was out.
func (s *Subscription) adopt(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.id, s.have, s.fresh = id, true, true
	return true
}

func (s *Subscription) takePending() []extend {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending
	s.pending = nil
	return p
}

func (s *Subscription) takeFresh() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fresh
	s.fresh = false
	return f
}

// lose ends a subscription that does not outlive its connection, and
// reports whether it was one.
func (s *Subscription) lose() bool {
	if s.hooks.OnLost == nil {
		return false
	}
	s.mu.Lock()
	s.closed, s.id, s.have = true, 0, false
	s.mu.Unlock()
	s.client.forget(s)
	return true
}

// orphan drops the sub id, which a lost connection does.
func (s *Subscription) orphan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id, s.have = 0, false
}
