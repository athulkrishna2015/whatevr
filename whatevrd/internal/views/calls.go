package views

import (
	"context"
	"fmt"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

// callsView is what rings right now: one row per offered call with no
// accept, reject or terminate after it. The desktop cannot answer —
// whatsmeow has no media stack — so the row renders "answer on your phone"
// with a Reject button wired to call.reject.
func (rs *Reads) callsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ringing, err := rs.r.Ringing(ctx)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for _, call := range ringing {
			row := v2.CallRow_builder{
				Video:     call.Video,
				StartedMs: call.T,
			}.Build()
			if caller := c.at(call.From, call.T); caller != nil {
				row.SetCaller(caller)
			}
			key := call.From
			if call.Group != "" {
				key = call.Group
			}
			c.wait(key, func(id, _ string) { row.SetChatId(id) })
			it := &v2.Upsert{}
			it.SetId(call.ID)
			it.SetSort([]byte(fmt.Sprintf("%020d\x00%s", call.T, call.ID)))
			it.SetCall(row)
			out = append(out, it)
		}
		return limited(out, max), c.finish()
	}
	w.wake = func(cc core.Change) bool { return touches(cc, "call", "person") }
	return w, nil, nil
}

// callHistoryView is every chat's call-log rows newest first, each carrying
// its chat's display name.
func (rs *Reads) callHistoryView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ms, err := rs.r.CallLog(ctx, model.Cursor{}, all(max))
		if err != nil {
			return nil, err
		}
		cs := &chats{c: c}
		out := make([]*v2.Upsert, 0, len(ms))
		for _, m := range ms {
			ch, ok, err := cs.of(m.Chat)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			it := c.messageItems(chatOf(ch), []model.Message{m})[0]
			it.GetMessage().SetChatName(ch.Name)
			out = append(out, it)
		}
		return out, c.finish()
	}
	w.wake = func(cc core.Change) bool { return touches(cc, "message", "person", "chat") }
	return w, nil, nil
}
