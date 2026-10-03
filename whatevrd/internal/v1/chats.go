package v1

import (
	"context"
	"sort"
	"strings"

	"whatevrd/internal/model"
	"whatevrd/internal/store"
)

// ListChatsForView is the chats view's window.
func (a *Adapter) ListChatsForView(ctx context.Context, f store.ChatListFilter) ([]store.Chat, error) {
	gen := a.previews.begin()
	w, err := a.World(ctx)
	if err != nil {
		return nil, err
	}
	kind := ""
	switch f.Kind {
	case store.ChatFilterDirect:
		kind = "direct"
	case store.ChatFilterGroups:
		kind = "groups"
	}
	chats, err := a.r.ChatsIn(ctx, w, model.ChatFilter{Kind: kind, Archived: f.Archived, Limit: f.Limit, Ties: true})
	if err != nil {
		return nil, err
	}
	out := make([]store.Chat, 0, len(chats))
	for _, c := range chats {
		out = append(out, a.chat(ctx, gen, w, c))
	}
	// in the order of v1's sort key, which goes by second and chat id where
	// the model goes by millisecond and person key
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.IsPinned != y.IsPinned {
			return x.IsPinned
		}
		if x.IsPinned && x.PinnedOrder != y.PinnedOrder {
			return x.PinnedOrder > y.PinnedOrder
		}
		if x.LastMessageTime != y.LastMessageTime {
			return x.LastMessageTime > y.LastMessageTime
		}
		return x.ID < y.ID
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// GetChatForView is one chat row.
func (a *Adapter) GetChatForView(ctx context.Context, chatID string) (store.Chat, error) {
	return a.GetChat(ctx, chatID)
}

func (a *Adapter) GetChat(ctx context.Context, chatID string) (store.Chat, error) {
	gen := a.previews.begin()
	w, err := a.World(ctx)
	if err != nil {
		return store.Chat{}, err
	}
	c, ok, err := a.r.ChatIn(ctx, w, key(w, chatID))
	if err != nil {
		return store.Chat{}, err
	}
	if !ok {
		return store.Chat{}, errNotFound
	}
	return a.chat(ctx, gen, w, c), nil
}

// chat is a model chat in v1's shape, its preview decoded from the newest
// message and its avatar from the old store. gen is the preview generation
// taken before w and c were read: a preview built from them is kept only if
// nothing was dropped since.
func (a *Adapter) chat(ctx context.Context, gen uint64, w *model.World, c model.Chat) store.Chat {
	id := ChatID(w, c.Key)
	out := store.Chat{
		ID:               id,
		Name:             c.Name,
		NameSource:       nameSource(c.NameFrom),
		LastMessageTime:  c.LastT / 1000,
		UnreadCount:      int32(c.Unread),
		IsGroup:          c.Group,
		IsPinned:         c.Pinned,
		IsArchived:       c.Archived,
		IsMuted:          c.Muted,
		MuteEndTimestamp: c.MuteEnd,
		HistoryExhausted: c.Exhausted,
		UpdatedAt:        c.LastT / 1000,
	}
	if c.Pinned {
		out.PinnedOrder = uint32(c.PinT / 1000)
	}
	// the phone's "mark as unread" is a dot, a badge of one stands in
	if c.MarkedUnread && out.UnreadCount == 0 {
		out.UnreadCount = 1
	}
	p, ok := a.previews.get(c.Key)
	if !ok {
		var keep bool
		p, keep = a.preview(ctx, w, c)
		if keep {
			a.previews.put(gen, c.Key, c.Addrs, p)
		}
	}
	if p.ok {
		out.LastMessage, out.LastMessageDirection, out.LastMessageStatus = p.text, p.direction, p.msgStatus
	}
	if a.old != nil {
		// never kept: the old core tells the views of a new avatar itself
		path, pic, st, checked, err := a.old.ChatAvatar(ctx, id)
		if err == nil {
			out.AvatarLocalPath, out.AvatarPictureID, out.AvatarStatus, out.AvatarCheckedAt = path, pic, st, checked
		}
	}
	return out
}

// preview is the chat's newest row as the list shows it, and whether it can
// be kept: not when the old store had a say in it (a queued send, a live
// location the old core groups).
func (a *Adapter) preview(ctx context.Context, w *model.World, c model.Chat) (preview, bool) {
	last, ok, err := a.r.Preview(ctx, w, c.Addrs)
	if err != nil {
		return preview{}, false
	}
	if !ok {
		return preview{}, true
	}
	m := a.message(ctx, w, c, last)
	keep := !last.Queued || len(last.Body) > 0
	if raw, _ := last.Content(); raw != nil && model.Unwrap(raw).Msg.GetLiveLocationMessage() != nil {
		keep = false
	}
	return preview{ok: true, text: store.ChatPreview(m, c.Group), direction: m.Direction, msgStatus: m.Status}, keep
}

func nameSource(from string) string {
	switch from {
	case model.NameContact:
		return store.ChatNameSourceContact
	case "group":
		return store.ChatNameSourceGroup
	case "phone":
		return store.ChatNameSourcePhone
	case "raw", "":
		return store.ChatNameSourceRaw
	}
	return store.ChatNameSourceWhatsApp
}

// SearchChats matches chat names, in list order.
func (a *Adapter) SearchChats(ctx context.Context, query string, limit int) ([]store.Chat, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	gen := a.previews.begin()
	w, err := a.World(ctx)
	if err != nil {
		return nil, err
	}
	chats, err := a.r.ChatsIn(ctx, w, model.ChatFilter{Any: true, Name: query, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]store.Chat, 0, len(chats))
	for _, c := range chats {
		out = append(out, a.chat(ctx, gen, w, c))
	}
	return out, nil
}
