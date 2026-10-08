package views

import (
	"context"
	"fmt"
	"strconv"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/server"
	"whatevrd/internal/whatsapp"
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

// channelMessagesPageSize is one live fetch of a channel's posts.
const channelMessagesPageSize = 30

// channelMessagesView is one channel's recent posts, newest first, fetched
// live on every fill and never stored. Extend re-fills with a bigger max;
// the window pages older by server id.
func (rs *Reads) channelMessagesView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	p := req.GetChannelMessages()
	if p.GetChannelId() == "" {
		return nil, nil, invalid("channel_messages needs a channel_id")
	}
	if rs.channelPosts == nil {
		return nil, nil, invalid("channel messages unavailable")
	}
	st := &channelWindow{channel: p.GetChannelId()}
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		for !st.ended && len(st.msgs) < max {
			before := int64(0)
			if len(st.msgs) > 0 {
				before = st.msgs[len(st.msgs)-1].ServerID
			}
			msgs, err := rs.channelPosts(ctx, st.channel, channelMessagesPageSize, before)
			if err != nil {
				return nil, err
			}
			if len(msgs) == 0 {
				st.ended = true
				break
			}
			st.msgs = append(st.msgs, msgs...)
			if len(msgs) < channelMessagesPageSize {
				st.ended = true
			}
		}
		msgs := st.msgs
		if len(msgs) > max {
			msgs = msgs[:max]
		}
		out := make([]*v2.Upsert, 0, len(msgs))
		for i, m := range msgs {
			row := v2.ChannelMessageRow_builder{
				ServerId: m.ServerID, ChannelId: st.channel, TMs: m.T * 1000,
				Text: m.Text, Fallback: m.Fallback, Views: m.Views,
			}.Build()
			it := &v2.Upsert{}
			it.SetId(fmt.Sprintf("%d", m.ServerID))
			it.SetSort([]byte(fmt.Sprintf("%020d\x00", len(msgs)-i) + strconv.FormatInt(m.ServerID, 10)))
			it.SetChannelMessage(row)
			out = append(out, it)
		}
		return out, nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "channels") }
	return w, nil, nil
}

// channelWindow pages a channel's posts by server id across fills.
type channelWindow struct {
	channel string
	msgs    []whatsapp.ChannelMessage
	ended   bool
}
