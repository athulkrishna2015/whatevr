package views

import (
	"context"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

// channelsView lists followed channels by name, from the directory table
// the refresh command fills.
func (rs *Reads) channelsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		channels, err := rs.r.Channels(ctx)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for _, ch := range channels {
			row := v2.ChannelRow_builder{
				Id: ch.JID, Name: ch.Name, Description: ch.Description,
				Followers: ch.Followers, Verified: ch.Verified, Muted: ch.Muted,
			}.Build()
			it := &v2.Upsert{}
			it.SetId(ch.JID)
			it.SetSort([]byte(ch.Name + "\x00" + ch.JID))
			it.SetChannel(row)
			out = append(out, it)
		}
		return limited(out, max), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "channels") }
	return w, nil, nil
}

// channelMessagesView is one channel's posts, newest first, over the
// channel's own message rows. Channels are never chats, so the address
// resolves straight to rows instead of through a chat id.
func (rs *Reads) channelMessagesView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	p := req.GetChannelMessages()
	if p.GetChannelId() == "" {
		return nil, nil, invalid("channel_messages needs a channel_id")
	}
	channel := p.GetChannelId()
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ms, err := rs.r.Messages(ctx, []string{channel}, model.Cursor{}, all(max), false)
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, m := range ms {
			ids = append(ids, m.ID)
		}
		serverIDs, err := rs.r.ServerIDs(ctx, channel, ids)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for _, m := range ms {
			for _, it := range c.messageItems(chatCtx{}, []model.Message{m}) {
				if serverID := serverIDs[m.ID]; serverID > 0 {
					it.GetMessage().SetServerId(serverID)
				}
				out = append(out, it)
			}
		}
		return limited(out, max), c.finish()
	}
	w.wake = func(cc core.Change) bool { return touches(cc, "message", "person", "chat", "local") }
	return w, nil, nil
}
