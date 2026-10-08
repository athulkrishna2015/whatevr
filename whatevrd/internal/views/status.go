package views

import (
	"context"
	"strconv"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

// statusChat is where status updates fold: the broadcast pseudo-chat, never
// a row in the chat list.
const statusChat = "status@broadcast"

// statusView is the contact-status feed, newest first, over the broadcast
// rows. Rows are message rows, so photos and videos render from the same
// fields a chat photo would; the viewed flag is overlaid from the local
// "viewed" ops.
func (rs *Reads) statusView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ms, err := rs.r.Messages(ctx, []string{statusChat}, model.Cursor{}, all(max), false)
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, m := range ms {
			ids = append(ids, m.ID)
		}
		seen, err := rs.r.StatusViewed(ctx, statusChat, ids)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for _, m := range ms {
			for _, it := range c.messageItems(chatCtx{}, []model.Message{m}) {
				if seen[m.ID] {
					it.GetMessage().SetViewed(true)
				}
				out = append(out, it)
			}
		}
		return limited(out, max), c.finish()
	}
	w.wake = func(cc core.Change) bool { return touches(cc, "message", "person", "local", "status") }
	return w, nil, nil
}

// statusMutedView lists the sender ids with status mute enabled, one row
// per sender. The Status tab reads it to decide which contacts collect
// under the collapsed Muted section instead of the main list.
func (rs *Reads) statusMutedView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		senders, err := rs.r.StatusMuted(ctx)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for i, sender := range senders {
			row := v2.StatusMutedRow_builder{SenderId: sender}.Build()
			it := &v2.Upsert{}
			it.SetId(sender)
			it.SetSort([]byte(strconv.Itoa(i) + "\x00" + sender))
			it.SetStatusMuted(row)
			out = append(out, it)
		}
		return limited(out, max), nil
	}
	w.wake = func(cc core.Change) bool { return touches(cc, "status") }
	return w, nil, nil
}
