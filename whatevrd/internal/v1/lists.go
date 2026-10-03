package v1

import (
	"context"
	"strings"
	"time"

	"whatevrd/internal/model"
	"whatevrd/internal/store"
)

// view is one chat as a listing needs it.
type view struct {
	w    *model.World
	chat model.Chat
}

func (a *Adapter) view(ctx context.Context, chatID string) (view, error) {
	w, err := a.World(ctx)
	if err != nil {
		return view{}, err
	}
	c, ok, err := a.r.ChatIn(ctx, w, key(w, chatID))
	if err != nil {
		return view{}, err
	}
	if !ok {
		// a chat with nothing in it yet is still somewhere to page
		k := key(w, chatID)
		c = model.Chat{Key: k, Addrs: w.Addrs(k), Group: model.IsGroup(k)}
	}
	return view{w: w, chat: c}, nil
}

func (a *Adapter) rows(ctx context.Context, v view, ms []model.Message) []store.Message {
	out := make([]store.Message, len(ms))
	over := make([]*store.Message, 0, len(ms))
	for i, m := range ms {
		var ok bool
		if out[i], ok = a.build(ctx, v.w, v.chat, m); ok {
			over = append(over, &out[i])
		}
	}
	a.overlay(ctx, over)
	return out
}

func reverse(ms []store.Message) {
	for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
		ms[i], ms[j] = ms[j], ms[i]
	}
}

// cursor is where a v1 message id sits in its chat.
func (a *Adapter) cursor(ctx context.Context, v view, v1id string) (model.Cursor, model.Message, error) {
	_, id, ok := SplitMessageID(v1id)
	if !ok {
		return model.Cursor{}, model.Message{}, errNotFound
	}
	m, found, err := a.r.Message(ctx, v.chat.Addrs, id)
	if err != nil {
		return model.Cursor{}, model.Message{}, err
	}
	if !found {
		return model.Cursor{}, model.Message{}, errNotFound
	}
	return model.Cursor{T: m.T, ID: m.ID}, m, nil
}

// ListMessages is the page before beforeMessageID (the newest page without
// one), oldest first.
func (a *Adapter) ListMessages(ctx context.Context, chatID string, limit int, beforeMessageID string) ([]store.Message, error) {
	v, err := a.view(ctx, chatID)
	if err != nil {
		return nil, err
	}
	from := model.Cursor{}
	if beforeMessageID != "" {
		if from, _, err = a.cursor(ctx, v, beforeMessageID); err != nil {
			return nil, err
		}
	}
	ms, err := a.r.Messages(ctx, v.chat.Addrs, from, limit, false)
	if err != nil {
		return nil, err
	}
	out := a.rows(ctx, v, ms)
	reverse(out)
	return out, nil
}

// ListMessagesAfter is the page after afterMessageID, oldest first.
func (a *Adapter) ListMessagesAfter(ctx context.Context, chatID string, limit int, afterMessageID string) ([]store.Message, error) {
	v, err := a.view(ctx, chatID)
	if err != nil {
		return nil, err
	}
	from, _, err := a.cursor(ctx, v, afterMessageID)
	if err != nil {
		return nil, err
	}
	ms, err := a.r.Messages(ctx, v.chat.Addrs, from, limit, true)
	if err != nil {
		return nil, err
	}
	return a.rows(ctx, v, ms), nil
}

// ListMessagesAround is a window with targetMessageID in the middle, oldest
// first. a picture inside an album anchors on its album.
func (a *Adapter) ListMessagesAround(ctx context.Context, chatID string, limit int, targetMessageID string) ([]store.Message, error) {
	v, err := a.view(ctx, chatID)
	if err != nil {
		return nil, err
	}
	at, target, err := a.cursor(ctx, v, targetMessageID)
	if err != nil {
		return nil, err
	}
	if target.Album != "" {
		if parent, ok, err := a.r.Message(ctx, v.chat.Addrs, target.Album); err == nil && ok {
			target, at = parent, model.Cursor{T: parent.T, ID: parent.ID}
		}
	}
	return a.around(ctx, v, at, target, limit)
}

func (a *Adapter) around(ctx context.Context, v view, at model.Cursor, target model.Message, limit int) ([]store.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit == 1 {
		return a.rows(ctx, v, []model.Message{target}), nil
	}
	room := limit - 1
	older, err := a.r.Messages(ctx, v.chat.Addrs, at, room, false)
	if err != nil {
		return nil, err
	}
	newer, err := a.r.Messages(ctx, v.chat.Addrs, at, room, true)
	if err != nil {
		return nil, err
	}
	olderTake := min(len(older), room/2)
	newerTake := min(len(newer), room-olderTake)
	if olderTake+newerTake < room {
		olderTake = min(len(older), room-newerTake)
	}
	picked := make([]model.Message, 0, olderTake+1+newerTake)
	for i := olderTake - 1; i >= 0; i-- {
		picked = append(picked, older[i])
	}
	picked = append(picked, target)
	picked = append(picked, newer[:newerTake]...)
	return a.rows(ctx, v, picked), nil
}

// ListMessagesAroundUnread anchors on the oldest of the unreadCount newest
// incoming messages.
func (a *Adapter) ListMessagesAroundUnread(ctx context.Context, chatID string, limit int, unreadCount int) ([]store.Message, string, error) {
	if unreadCount <= 0 {
		return nil, "", errNotFound
	}
	v, err := a.view(ctx, chatID)
	if err != nil {
		return nil, "", err
	}
	var anchor *model.Message
	from := model.Cursor{}
	seen := 0
	for seen < unreadCount {
		page, err := a.r.Messages(ctx, v.chat.Addrs, from, 200, false)
		if err != nil {
			return nil, "", err
		}
		if len(page) == 0 {
			break
		}
		for i := range page {
			m := page[i]
			from = model.Cursor{T: m.T, ID: m.ID}
			if m.FromMe || m.Facts.Revoked || strings.HasPrefix(m.Kind, "stub:") {
				continue
			}
			anchor = &page[i]
			seen++
			if seen == unreadCount {
				break
			}
		}
	}
	if anchor == nil {
		return nil, "", errNotFound
	}
	out, err := a.around(ctx, v, model.Cursor{T: anchor.T, ID: anchor.ID}, *anchor, limit)
	if err != nil {
		return nil, "", err
	}
	return out, MessageID(ChatID(v.w, v.chat.Key), anchor.ID), nil
}

// GetMessage is one message by its v1 id.
func (a *Adapter) GetMessage(ctx context.Context, v1id string) (store.Message, error) {
	chatID, id, ok := SplitMessageID(v1id)
	if !ok {
		return store.Message{}, errNotFound
	}
	v, err := a.view(ctx, chatID)
	if err != nil {
		return store.Message{}, err
	}
	m, found, err := a.r.Message(ctx, v.chat.Addrs, id)
	if err != nil {
		return store.Message{}, err
	}
	if !found {
		return store.Message{}, errNotFound
	}
	return a.message(ctx, v.w, v.chat, m), nil
}

// ListStarredMessages is starred messages, newest first, in one chat or all.
func (a *Adapter) ListStarredMessages(ctx context.Context, chatID string, limit int, beforeMessageID string) ([]store.StarredMessage, error) {
	w, err := a.World(ctx)
	if err != nil {
		return nil, err
	}
	var addrs []string
	if chatID != "" {
		addrs = w.Addrs(key(w, chatID))
	}
	from := model.Cursor{}
	if beforeMessageID != "" {
		bchat, id, ok := SplitMessageID(beforeMessageID)
		if !ok {
			return nil, errNotFound
		}
		m, found, err := a.r.Message(ctx, w.Addrs(key(w, bchat)), id)
		if err != nil {
			return nil, err
		}
		if found {
			from = model.Cursor{T: m.T, ID: m.ID}
		}
	}
	if limit <= 0 {
		limit = 50
	}
	ms, err := a.r.Starred(ctx, addrs, from, limit)
	if err != nil {
		return nil, err
	}
	out := make([]store.StarredMessage, 0, len(ms))
	for _, m := range ms {
		v, err := a.view(ctx, m.Chat)
		if err != nil {
			return nil, err
		}
		out = append(out, store.StarredMessage{Message: a.message(ctx, v.w, v.chat, m), ChatName: v.chat.Name})
	}
	return out, nil
}

// ListPinnedMessages is a chat's pinned messages, oldest pin first.
func (a *Adapter) ListPinnedMessages(ctx context.Context, chatID string) ([]store.Message, error) {
	v, err := a.view(ctx, chatID)
	if err != nil {
		return nil, err
	}
	ms, err := a.r.Pinned(ctx, v.chat.Addrs)
	if err != nil {
		return nil, err
	}
	// a pin runs out on its own, as the old store has it by the wall clock
	now := time.Now().UnixMilli()
	live := ms[:0]
	for _, m := range ms {
		if m.Facts.PinEnd > now {
			live = append(live, m)
		}
	}
	return a.rows(ctx, v, live), nil
}

// mediaFields are the content kinds the gallery shows, stickers left out as
// the old gallery does.
var mediaFields = []string{"imageMessage", "videoMessage", "documentMessage", "audioMessage", "ptvMessage"}

// ListChatMediaMessages is a chat's media, newest first.
func (a *Adapter) ListChatMediaMessages(ctx context.Context, chatID string, limit int, beforeMessageID string) ([]store.Message, error) {
	v, err := a.view(ctx, chatID)
	if err != nil {
		return nil, err
	}
	from := model.Cursor{}
	if beforeMessageID != "" {
		if from, _, err = a.cursor(ctx, v, beforeMessageID); err != nil {
			return nil, err
		}
	}
	if limit <= 0 {
		limit = 50
	}
	ms, err := a.r.Media(ctx, v.chat.Addrs, mediaFields, from, limit)
	if err != nil {
		return nil, err
	}
	return a.rows(ctx, v, ms), nil
}

// SearchMessages matches message words, newest first, in one chat or all.
func (a *Adapter) SearchMessages(ctx context.Context, query, chatID string, limit int, beforeMessageID string) ([]store.MessageSearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	w, err := a.World(ctx)
	if err != nil {
		return nil, err
	}
	var addrs []string
	if chatID != "" {
		addrs = w.Addrs(key(w, chatID))
	}
	from := model.Cursor{}
	if beforeMessageID != "" {
		if bchat, id, ok := SplitMessageID(beforeMessageID); ok {
			if m, found, err := a.r.Message(ctx, w.Addrs(key(w, bchat)), id); err == nil && found {
				from = model.Cursor{T: m.T, ID: m.ID}
			}
		}
	}
	if limit <= 0 {
		limit = 50
	}
	ms, err := a.r.Search(ctx, strings.TrimSpace(query), addrs, from, limit)
	if err != nil {
		return nil, err
	}
	out := make([]store.MessageSearchResult, 0, len(ms))
	for _, m := range ms {
		v, err := a.view(ctx, m.Chat)
		if err != nil {
			return nil, err
		}
		out = append(out, store.MessageSearchResult{Message: a.message(ctx, v.w, v.chat, m), ChatName: v.chat.Name})
	}
	return out, nil
}

// SenderDisplay is a sender's name and avatar for the typing view.
func (a *Adapter) SenderDisplay(ctx context.Context, id string) (string, string, error) {
	w, err := a.World(ctx)
	if err != nil {
		return "", "", err
	}
	name, _ := w.Name(w.Now(model.Norm(id)))
	avatar := ""
	if a.old != nil {
		_, avatar, _ = a.old.SenderDisplay(ctx, id)
	}
	return name, avatar, nil
}

// the rest of the store interfaces stay with the old store until their
// features move

func (a *Adapter) ListLiveLocationShares(ctx context.Context, chatID string, now int64) ([]store.LiveLocationShare, error) {
	return a.old.ListLiveLocationShares(ctx, chatID, now)
}

// CountPendingOutgoingMessages is the outbox: sends this daemon queued that
// are still owed.
func (a *Adapter) CountPendingOutgoingMessages(ctx context.Context) (int, error) {
	out, err := a.r.Unsent(ctx)
	return len(out), err
}

// the sticker library is still the old store's

func (a *Adapter) ListRecentStickers(ctx context.Context, limit int) ([]store.Sticker, error) {
	return a.old.ListRecentStickers(ctx, limit)
}

func (a *Adapter) ListFavoriteStickers(ctx context.Context, limit int) ([]store.Sticker, error) {
	return a.old.ListFavoriteStickers(ctx, limit)
}

func (a *Adapter) ListAllStickers(ctx context.Context, limit int) ([]store.Sticker, error) {
	return a.old.ListAllStickers(ctx, limit)
}

func (a *Adapter) ListStickerPacks(ctx context.Context) ([]store.StickerPack, error) {
	return a.old.ListStickerPacks(ctx)
}

func (a *Adapter) GetStickerPack(ctx context.Context, id string) (store.StickerPack, bool, error) {
	return a.old.GetStickerPack(ctx, id)
}

func (a *Adapter) ListPackStickers(ctx context.Context, packID string) ([]store.Sticker, error) {
	return a.old.ListPackStickers(ctx, packID)
}
