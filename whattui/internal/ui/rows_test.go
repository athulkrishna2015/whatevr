package ui

import (
	"sort"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// The tests that say exactly what is on screen hand the views updates the way
// the client does, one ViewUpdate at a time.

func update(sink proto.ViewSink, u v2.ViewUpdate_builder) { sink.Apply(u.Build(), false) }

func reset(sink proto.ViewSink) { update(sink, v2.ViewUpdate_builder{Reset: true}) }

func ready(sink proto.ViewSink, exhausted bool) {
	update(sink, v2.ViewUpdate_builder{Ready: v2.Ready_builder{Exhausted: exhausted}.Build()})
}

func upsert(sink proto.ViewSink, u v2.Upsert_builder) {
	update(sink, v2.ViewUpdate_builder{Changes: []*v2.Change{v2.Change_builder{Upsert: u.Build()}.Build()}})
}

func remove(sink proto.ViewSink, id string) {
	update(sink, v2.ViewUpdate_builder{Changes: []*v2.Change{
		v2.Change_builder{Remove: v2.Remove_builder{Id: id}.Build()}.Build(),
	}})
}

func putChat(c *view.Collection[*v2.ChatRow], sort string, r *v2.ChatRow) {
	upsert(c, v2.Upsert_builder{Id: r.GetId(), Sort: []byte(sort), Chat: r})
}

func putMsg(c *view.Collection[*v2.MessageRow], sort string, r *v2.MessageRow) {
	upsert(c, v2.Upsert_builder{Id: r.GetId(), Sort: []byte(sort), Message: r})
}

func setConn(o *view.Object[*v2.ConnectionRow], r *v2.ConnectionRow) {
	upsert(o, v2.Upsert_builder{Connection: r})
	ready(o, false)
}

func setLogin(o *view.Object[*v2.LoginRow], r *v2.LoginRow) {
	upsert(o, v2.Upsert_builder{Login: r})
	ready(o, false)
}

func online() *v2.ConnectionRow {
	return v2.ConnectionRow_builder{State: v2.ConnectionState_CONNECTION_STATE_ONLINE}.Build()
}

func needLogin() *v2.ConnectionRow {
	return v2.ConnectionRow_builder{State: v2.ConnectionState_CONNECTION_STATE_NEED_LOGIN}.Build()
}

func person(id, name string) *v2.Person { return v2.Person_builder{Id: id, Name: name}.Build() }

func preview(text string) *v2.ChatPreview { return v2.ChatPreview_builder{Text: text}.Build() }

// rx is one person's reaction, for building a row the way the daemon would.
type rx struct {
	emoji, name string
	mine        bool
}

// counts is the daemon's reaction_counts for rs: most first, first seen
// breaking ties.
func counts(rs []rx) []*v2.ReactionCount {
	var out []*v2.ReactionCount
	at := map[string]int{}
	for _, r := range rs {
		i, ok := at[r.emoji]
		if !ok {
			i = len(out)
			at[r.emoji] = i
			out = append(out, v2.ReactionCount_builder{Emoji: r.emoji}.Build())
		}
		out[i].SetCount(out[i].GetCount() + 1)
		out[i].SetMine(out[i].GetMine() || r.mine)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GetCount() > out[j].GetCount() })
	return out
}

// reactors is the row's named sample for rs.
func reactors(rs []rx) []*v2.Reaction {
	out := make([]*v2.Reaction, 0, len(rs))
	for _, r := range rs {
		out = append(out, v2.Reaction_builder{
			Emoji:  r.emoji,
			Sender: v2.Person_builder{Name: r.name, Self: r.mine}.Build(),
		}.Build())
	}
	return out
}

// chatsAnswer is a search_chats result holding chats.
func chatsAnswer(chats ...*v2.ChatRow) *v2.Response {
	r := &v2.Response{}
	r.SetSearchChats(v2.SearchChatsResult_builder{Chats: chats}.Build())
	return r
}
