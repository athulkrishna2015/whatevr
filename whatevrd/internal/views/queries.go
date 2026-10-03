package views

import (
	"context"
	"strings"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

// searchLimit is a query's page when it asked for none
const searchLimit = 50

// pageOf is a query's page for limit, held to what a frame takes of items
// up to itemBytes, the same cap a window of them has.
func pageOf(limit uint32, itemBytes int) (int, error) {
	if c := server.WindowCap(itemBytes); int(limit) > c {
		return 0, invalid("limit %d is over the cap of %d", limit, c)
	}
	if limit == 0 {
		return searchLimit, nil
	}
	return int(limit), nil
}

func (rs *Reads) searchChats(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	q := req.GetSearchChats()
	res := &v2.SearchChatsResult{}
	if query := strings.TrimSpace(q.GetQuery()); query != "" {
		limit, err := pageOf(q.GetLimit(), chatBytes)
		if err != nil {
			return nil, err
		}
		gen := rs.previews.begin()
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		chats, err := rs.r.ChatsIn(ctx, c.w, model.ChatFilter{Any: true, Name: query, Limit: limit})
		if err != nil {
			return nil, err
		}
		rows := make([]*v2.ChatRow, len(chats))
		for i, ch := range chats {
			rows[i] = c.chatRow(gen, ch)
		}
		if err := c.finish(); err != nil {
			return nil, err
		}
		for _, r := range rows {
			Fit(r, chatBytes)
		}
		res.SetChats(rows)
	}
	resp := &v2.Response{}
	resp.SetSearchChats(res)
	return resp, nil
}

func (rs *Reads) searchMessages(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	q := req.GetSearchMessages()
	res := &v2.SearchMessagesResult{}
	resp := &v2.Response{}
	resp.SetSearchMessages(res)
	query := strings.TrimSpace(q.GetQuery())
	if query == "" {
		return resp, nil
	}
	limit, err := pageOf(q.GetLimit(), messageBytes)
	if err != nil {
		return nil, err
	}
	c, err := rs.begin(ctx)
	if err != nil {
		return nil, err
	}
	var addrs []string
	if id := q.GetChatId(); id != "" {
		ch, ok, err := c.chat(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, notFound("no chat %q", id)
		}
		addrs = ch.Addrs
	}
	from := model.Cursor{}
	if b := q.GetBefore(); b != "" {
		addr, id, ok := SplitToken(b)
		if !ok {
			return nil, invalid("malformed message id %q", b)
		}
		m, ok, err := rs.r.Message(ctx, c.w.Addrs(c.w.Now(model.Norm(addr))), id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, notFound("no message %q", b)
		}
		from = model.Cursor{T: m.T, Ord: m.Ord, ID: m.ID}
	}
	ms, err := rs.r.Search(ctx, query, addrs, from, limit+1)
	if err != nil {
		return nil, err
	}
	if len(ms) > limit {
		ms = ms[:limit]
		res.SetMore(true)
	}
	cs := &chats{c: c}
	rows := make([]*v2.MessageRow, 0, len(ms))
	for _, m := range ms {
		ch, ok, err := cs.of(m.Chat)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		row := c.message(chatOf(ch), m)
		row.SetChatName(ch.Name)
		rows = append(rows, row)
	}
	if err := c.finish(); err != nil {
		return nil, err
	}
	for _, r := range rows {
		Fit(r, messageBytes)
	}
	res.SetMessages(rows)
	return resp, nil
}

func (rs *Reads) searchStickers(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	q := req.GetSearchStickers()
	limit, err := pageOf(q.GetLimit(), rowBytes)
	if err != nil {
		return nil, err
	}
	res := &v2.SearchStickersResult{}
	if query := strings.TrimSpace(q.GetQuery()); query != "" {
		ss, err := rs.r.SearchStickers(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		rows := make([]*v2.StickerRow, len(ss))
		for i, s := range ss {
			rows[i] = stickerRow(s)
		}
		res.SetStickers(rows)
	}
	resp := &v2.Response{}
	resp.SetSearchStickers(res)
	return resp, nil
}
