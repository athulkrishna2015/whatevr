package views

import (
	"context"
	"math"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

// all is a read limit for a window that takes everything
func all(max int) int {
	if max <= 0 {
		return math.MaxInt32
	}
	return max
}

// mediaFields are the content kinds the gallery shows, stickers left out
var mediaFields = []string{"imageMessage", "videoMessage", "documentMessage", "audioMessage", "ptvMessage"}

// chats reads each message's chat once per read.
type chats struct {
	c    *rc
	byKy map[string]model.Chat
}

func (cs *chats) of(addr string) (model.Chat, bool, error) {
	k := cs.c.w.Now(model.Norm(addr))
	if ch, ok := cs.byKy[k]; ok {
		return ch, true, nil
	}
	ch, ok, err := cs.c.r.ChatIn(cs.c.ctx, cs.c.w, k)
	if err != nil || !ok {
		return ch, ok, err
	}
	if cs.byKy == nil {
		cs.byKy = map[string]model.Chat{}
	}
	cs.byKy[k] = ch
	return ch, true, nil
}

// messageList is a window of message rows read by read, in one chat or all
// when id is "". chat_name goes on each row when named is set.
func (rs *Reads) messageList(req *v2.Subscribe, id string, named bool,
	read func(ctx context.Context, addrs []string, max int) ([]model.Message, error)) (server.Window, error) {
	var addrs []string
	if id != "" {
		ch, err := rs.chatByID(context.Background(), id)
		if err != nil {
			return nil, err
		}
		addrs = ch.Addrs
	}
	wt := &watch{}
	if id != "" {
		wt.saw(model.Chat{Addrs: addrs}, &rc{})
	}
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		var in []string
		if id != "" {
			ch, ok, err := c.chat(id)
			if err != nil || !ok {
				return nil, err
			}
			in = ch.Addrs
			defer wt.saw(ch, c)
		}
		ms, err := read(ctx, in, max)
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
			if named {
				it.GetMessage().SetChatName(ch.Name)
			}
			out = append(out, it)
		}
		return out, c.finish()
	}
	if id != "" {
		w.wake = wt.wake
	} else {
		w.wake = func(c core.Change) bool { return touches(c, "message", "person", "chat") }
	}
	return w, nil
}

func (rs *Reads) starredView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w, err := rs.messageList(req, req.GetStarred().GetChatId(), true,
		func(ctx context.Context, addrs []string, max int) ([]model.Message, error) {
			return rs.r.Starred(ctx, addrs, model.Cursor{}, all(max))
		})
	return w, nil, err
}

func (rs *Reads) pinnedView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetPinned().GetChatId()
	if id == "" {
		return nil, nil, invalid("pinned needs a chat_id")
	}
	w, err := rs.messageList(req, id, false, func(ctx context.Context, addrs []string, max int) ([]model.Message, error) {
		ms, err := rs.r.Pinned(ctx, addrs)
		if err != nil {
			return nil, err
		}
		// a pin runs out on its own: the minute clock takes it away
		now := time.Now().UnixMilli()
		out := ms[:0]
		for _, m := range ms {
			if m.Facts.PinEnd > now {
				out = append(out, m)
			}
		}
		return limited(out, max), nil
	})
	if err != nil {
		return nil, nil, err
	}
	inner := w.(*win)
	wake := inner.wake
	inner.wake = func(c core.Change) bool { return wake(c) || touches(c, "pins", TouchClock) }
	return w, nil, nil
}

func (rs *Reads) chatLinksView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetChatLinks().GetChatId()
	if id == "" {
		return nil, nil, invalid("chat_links needs a chat_id")
	}
	w, err := rs.messageList(req, id, false, func(ctx context.Context, addrs []string, max int) ([]model.Message, error) {
		return rs.r.Links(ctx, addrs, model.Cursor{}, all(max))
	})
	if err != nil {
		return nil, nil, err
	}
	w.(*win).limit = messagesLimit
	return w, nil, nil
}

func (rs *Reads) chatMediaView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetChatMedia().GetChatId()
	if id == "" {
		return nil, nil, invalid("chat_media needs a chat_id")
	}
	w, err := rs.messageList(req, id, false, func(ctx context.Context, addrs []string, max int) ([]model.Message, error) {
		return rs.r.Media(ctx, addrs, mediaFields, model.Cursor{}, all(max))
	})
	if err != nil {
		return nil, nil, err
	}
	w.(*win).limit = messagesLimit
	return w, nil, nil
}

func (rs *Reads) liveLocationsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetLiveLocations().GetChatId()
	ch, err := rs.chatByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	wt := &watch{}
	wt.saw(ch, &rc{})
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ch, ok, err := c.chat(id)
		if err != nil || !ok {
			return nil, err
		}
		shares, err := rs.r.LiveShares(ctx, ch.Addrs, time.Now().UnixMilli())
		if err != nil {
			return nil, err
		}
		ids := make([]string, len(shares))
		for i, s := range shares {
			ids[i] = s.ID
		}
		homes, err := rs.r.Homes(ctx, ch.Addrs, ids)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for _, s := range shares {
			home := homes[s.ID]
			if home == "" {
				home = s.Chat
			}
			l := s.Live
			row := v2.LiveLocationRow_builder{
				MessageId: MessageToken(home, s.ID),
				StartedMs: l.Start, ExpiresMs: l.Ends(), UpdatedMs: l.Updated,
			}.Build()
			if l.Known {
				row.SetLocation(v2.Location_builder{Lat: l.Lat, Lng: l.Lng, AccuracyM: l.Acc}.Build())
			}
			if c.w.IsSelf(s.Sender) || s.Sender == model.Me {
				row.SetSender(c.self())
			} else {
				row.SetSender(c.at(s.Sender, l.Start))
			}
			it := &v2.Upsert{}
			it.SetId(row.GetMessageId())
			it.SetSort(append(asc(nil, l.Start), s.ID...))
			it.SetLiveLocation(row)
			out = append(out, it)
		}
		if err := c.finish(); err != nil {
			return nil, err
		}
		wt.saw(ch, c)
		return limited(sorted(out), max), nil
	}
	w.wake = wt.wake
	return w, nil, nil
}

func (rs *Reads) receiptsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	tok := req.GetReceipts().GetMessageId()
	addr, id, ok := SplitToken(tok)
	if !ok {
		return nil, nil, invalid("malformed message id %q", tok)
	}
	w0, err := rs.World(ctx)
	if err != nil {
		return nil, nil, err
	}
	ch, ok, err := rs.r.ChatIn(ctx, w0, addr)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, notFound("no message %q", tok)
	}
	if _, ok, err := rs.r.Message(ctx, ch.Addrs, id); err != nil || !ok {
		if err == nil {
			err = notFound("no message %q", tok)
		}
		return nil, nil, err
	}
	wt := &watch{}
	wt.saw(ch, &rc{})
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ch, ok, err := c.r.ChatIn(ctx, c.w, addr)
		if err != nil || !ok {
			return nil, err
		}
		m, ok, err := rs.r.Message(ctx, ch.Addrs, id)
		if err != nil || !ok {
			return nil, err
		}
		type times struct{ delivered, read, played int64 }
		byWho := map[string]*times{}
		var order []string
		for _, r := range m.Facts.Receipts {
			if r.Who == model.Me || c.w.IsSelf(r.Who) {
				continue
			}
			k := c.w.Now(model.Norm(r.Who))
			t, ok := byWho[k]
			if !ok {
				t = &times{}
				byWho[k] = t
				order = append(order, k)
			}
			// a later receipt says the earlier steps happened by then
			switch r.Type {
			case "played":
				t.played = first(t.played, r.T)
				fallthrough
			case "read":
				t.read = first(t.read, r.T)
				fallthrough
			case "delivered", "":
				t.delivered = first(t.delivered, r.T)
			}
		}
		var out []*v2.Upsert
		for _, k := range order {
			t := byWho[k]
			out = append(out, c.personItem(k, func(it *v2.Upsert, p *v2.Person) {
				it.SetReceipt(v2.ReceiptRow_builder{Person: p, DeliveredMs: t.delivered, ReadMs: t.read,
					PlayedMs: t.played}.Build())
			}))
		}
		if err := c.finish(); err != nil {
			return nil, err
		}
		wt.saw(ch, c)
		return limited(sorted(out), max), nil
	}
	w.wake = wt.wake
	w.replaced = rs.replaced
	return merging{w}, nil, nil
}

// first is the earlier of two times, 0 counting as none.
func first(a, b int64) int64 {
	if a == 0 || b != 0 && b < a {
		return b
	}
	return a
}
