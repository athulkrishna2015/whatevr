package views

import (
	"context"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

func (rs *Reads) transfersView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ts := rs.live.Transfers()
		var out []*v2.Upsert
		for _, t := range ts {
			dir := v2.TransferDirection_TRANSFER_DIRECTION_DOWNLOAD
			if t.Upload {
				dir = v2.TransferDirection_TRANSFER_DIRECTION_UPLOAD
			}
			row := v2.TransferRow_builder{Direction: dir, DoneBytes: t.Done, TotalBytes: t.Total, Error: t.Error}.Build()
			home := t.Chat
			if ch, ok, err := c.r.ChatIn(ctx, c.w, t.Chat); err == nil && ok {
				if hs, err := c.r.Homes(ctx, ch.Addrs, []string{t.ID}); err == nil && hs[t.ID] != "" {
					home = hs[t.ID]
				}
			}
			row.SetMessageId(MessageToken(home, t.ID))
			c.wait(c.w.Now(model.Norm(t.Chat)), func(id, _ string) { row.SetChatId(id) })
			it := &v2.Upsert{}
			it.SetId(row.GetMessageId())
			it.SetSort([]byte(row.GetMessageId()))
			it.SetTransfer(row)
			out = append(out, it)
		}
		return limited(sorted(out), max), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, live.TouchTransfer) }
	return w, nil, nil
}

func (rs *Reads) notificationsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, lim int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for _, n := range rs.live.Notifications() {
			row := v2.NotificationRow_builder{
				Id: n.ID, MessageId: n.Message, Title: n.Title, Body: n.Body, AvatarPath: n.Avatar,
				Count: uint32(max(n.Count, 0)), TMs: ms(n.T), Sound: n.Sound,
			}.Build()
			if n.Sender != "" {
				row.SetSender(c.now(n.Sender))
			}
			c.wait(c.w.Now(model.Norm(n.Chat)), func(id, _ string) { row.SetChatId(id) })
			it := &v2.Upsert{}
			it.SetId(n.ID)
			it.SetSort(append(desc(nil, ms(n.T)), n.ID...))
			it.SetNotification(row)
			out = append(out, it)
		}
		return limited(sorted(out), lim), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, live.TouchNotification) }
	return w, nil, nil
}
