package proto

import "encoding/json"

// dispatch routes one frame. A frame with `event` is an event, anything else
// is a response; those are the only two shapes that arrive.
func (c *Client) dispatch(line []byte) {
	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		// A frame we cannot parse is the daemon's bug, not a reason to drop a
		// working connection. Skip it.
		return
	}
	if f.Event != "" {
		c.handleEvent(&f, line)
		return
	}
	if f.ID != nil {
		c.handleResponse(&f)
	}
}

func (c *Client) handleResponse(f *frame) {
	c.mu.Lock()
	cb, ok := c.pending[*f.ID]
	delete(c.pending, *f.ID)
	c.mu.Unlock()

	if !ok || cb == nil {
		return
	}
	cb(f.Result, f.Err)
}

func (c *Client) handleEvent(f *frame, line []byte) {
	// Events with no sub are directed at this connection rather than at a
	// view. There are exactly two.
	if f.Sub == nil {
		switch f.Event {
		case "open_chat":
			if c.OnOpenChat != nil {
				var ev OpenChat
				if json.Unmarshal(line, &ev) == nil {
					c.OnOpenChat(ev)
				}
			}
		case "media_stream_update":
			if c.OnMediaStreamUpdate != nil {
				var ev MediaStreamUpdate
				if json.Unmarshal(line, &ev) == nil {
					c.OnMediaStreamUpdate(ev)
				}
			}
		}
		return
	}

	c.mu.Lock()
	sub := c.bySubID[*f.Sub]
	c.mu.Unlock()
	if sub == nil || sub.sink == nil {
		return
	}
	sink := sub.sink

	switch f.Event {
	case "upsert":
		c.openBatch(sink)
		sink.Upsert(f.Sort, f.Item)
	case "remove":
		var rf removeFrame
		if json.Unmarshal(line, &rf) != nil {
			return
		}
		c.openBatch(sink)
		sink.Remove(rf.ItemID)
	case "ready":
		// Ready and reset apply eagerly: a caller waiting on a window being
		// full has to see it now, not at the end of the read.
		c.endBatch()
		sink.Ready(f.Exhausted != nil && *f.Exhausted, f.Exhausted != nil)
	case "reset":
		c.endBatch()
		sink.Reset()
	}
}

// openBatch starts a sink's transaction if it has none open.
func (c *Client) openBatch(sink ViewSink) {
	for _, s := range c.batched {
		if s == sink {
			return
		}
	}
	c.batched = append(c.batched, sink)
	sink.BatchBegin()
}

// endBatch closes every open transaction and tells the app to redraw. Called
// at the end of each read and before anything that has to be seen immediately.
func (c *Client) endBatch() {
	if len(c.batched) == 0 {
		return
	}
	open := c.batched
	c.batched = nil
	for _, sink := range open {
		sink.BatchEnd()
	}
	if c.OnDrain != nil {
		c.OnDrain()
	}
}
