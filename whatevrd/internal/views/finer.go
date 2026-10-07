package views

import (
	"context"
	"fmt"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/server"
)

// what a row only counts, the finer views list: everyone who reacted,
// voted or answered, read off the message's row built whole.

func (rs *Reads) reactionsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	return rs.rowView(ctx, req, req.GetReactions().GetMessageId(), func(row *v2.MessageRow) []*v2.Upsert {
		var out []*v2.Upsert
		for _, r := range row.GetReactions() {
			it := &v2.Upsert{}
			it.SetId(r.GetSender().GetId())
			it.SetSort(append(desc(nil, r.GetTMs()), r.GetSender().GetId()...))
			it.SetReaction(r)
			out = append(out, it)
		}
		return out
	})
}

func (rs *Reads) pollVotesView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	return rs.rowView(ctx, req, req.GetPollVotes().GetMessageId(), func(row *v2.MessageRow) []*v2.Upsert {
		var out []*v2.Upsert
		for _, o := range row.GetPoll().GetOptions() {
			for _, v := range o.GetVoters() {
				id := v.GetPerson().GetId()
				it := &v2.Upsert{}
				it.SetId(fmt.Sprint(o.GetIndex(), ":", id))
				it.SetSort(append(desc(asc(nil, int64(o.GetIndex())), v.GetTMs()), id...))
				it.SetPollVote(v2.PollVoteRow_builder{Option: o.GetIndex(), Person: v.GetPerson(), TMs: v.GetTMs()}.Build())
				out = append(out, it)
			}
		}
		return out
	})
}

func (rs *Reads) eventResponsesView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	return rs.rowView(ctx, req, req.GetEventResponses().GetMessageId(), func(row *v2.MessageRow) []*v2.Upsert {
		var out []*v2.Upsert
		for _, r := range row.GetEvent().GetResponders() {
			id := r.GetPerson().GetId()
			it := &v2.Upsert{}
			it.SetId(id)
			it.SetSort(append(desc(nil, r.GetTMs()), id...))
			it.SetResponder(r)
			out = append(out, it)
		}
		return out
	})
}

// rowView is a window of what items makes of message tok's whole row.
func (rs *Reads) rowView(ctx context.Context, req *v2.Subscribe, tok string, items func(*v2.MessageRow) []*v2.Upsert) (server.Window, *v2.SubscribeResult, error) {
	if _, _, err := rs.wholeRow(ctx, tok); err != nil {
		return nil, nil, err
	}
	wt := &watch{}
	w := &win{params: req, wake: wt.wake}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		row, saw, err := rs.wholeRow(ctx, tok)
		if err != nil {
			// deleted since: the window empties rather than failing
			if server.Code(err) == v2.ErrorCode_ERROR_CODE_NOT_FOUND {
				return nil, nil
			}
			return nil, err
		}
		saw(wt)
		return limited(sorted(items(row)), max), nil
	}
	return w, nil, nil
}

// wholeRow is message tok as a row with every list in it, people named, and
// what tells a watch about it.
func (rs *Reads) wholeRow(ctx context.Context, tok string) (*v2.MessageRow, func(*watch), error) {
	addr, id, ok := SplitToken(tok)
	if !ok {
		return nil, nil, invalid("malformed message id %q", tok)
	}
	c, err := rs.begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	c.full = true
	ch, ok, err := c.r.ChatIn(ctx, c.w, addr)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, notFound("no message %q", tok)
	}
	m, ok, err := rs.r.Message(ctx, ch.Addrs, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, notFound("no message %q", tok)
	}
	row := c.message(chatOf(ch), m)
	if err := c.finish(); err != nil {
		return nil, nil, err
	}
	return row, func(w *watch) { w.saw(ch, c) }, nil
}

// messageText is the whole text of a row that came cut.
func (rs *Reads) messageText(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	row, _, err := rs.wholeRow(ctx, req.GetMessageText().GetMessageId())
	if err != nil {
		return nil, err
	}
	resp := &v2.Response{}
	resp.SetMessageText(v2.MessageTextResult_builder{Text: row.GetText(), Mentions: row.GetMentions()}.Build())
	return resp, nil
}
