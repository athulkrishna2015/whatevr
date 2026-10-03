package views

import (
	"context"
	"strings"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/server"
	"whatevrd/internal/store"
)

func stickerRow(s store.Sticker) *v2.StickerRow {
	return v2.StickerRow_builder{
		Id: s.CacheKey, Path: s.LocalPath, Mime: s.MimeType, Animated: s.IsAnimated,
		Lottie: strings.EqualFold(strings.TrimSpace(s.MimeType), "application/was"), Width: uint32(max(s.Width, 0)), Height: uint32(max(s.Height, 0)),
		Emojis: strings.Fields(s.Emojis), AccessibilityText: s.AccessibilityText, PackId: s.PackID,
		Favorite: s.IsFavorite, LastUsedMs: toMS(s.LastUsed),
	}.Build()
}

// stickerItems is ss in the order given.
func stickerItems(ss []store.Sticker) []*v2.Upsert {
	out := make([]*v2.Upsert, 0, len(ss))
	seen := map[string]bool{}
	for i, s := range ss {
		if s.CacheKey == "" || seen[s.CacheKey] {
			continue
		}
		seen[s.CacheKey] = true
		it := &v2.Upsert{}
		it.SetId(s.CacheKey)
		it.SetSort(asc(nil, int64(i)))
		it.SetSticker(stickerRow(s))
		out = append(out, it)
	}
	return out
}

func (rs *Reads) stickersView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	src := req.GetStickers().GetSource()
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		var ss []store.Sticker
		var err error
		switch src {
		case v2.StickerSource_STICKER_SOURCE_FAVORITE:
			ss, err = rs.r.ListFavoriteStickers(ctx, max)
		case v2.StickerSource_STICKER_SOURCE_ALL:
			ss, err = rs.r.ListAllStickers(ctx, max)
		default:
			ss, err = rs.r.ListRecentStickers(ctx, max)
		}
		if err != nil {
			return nil, err
		}
		return stickerItems(ss), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "sticker") }
	return w, nil, nil
}

func (rs *Reads) stickerPacksView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, lim int) ([]*v2.Upsert, error) {
		ps, err := rs.r.ListStickerPacks(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]*v2.Upsert, 0, len(ps))
		for i, p := range limited(ps, lim) {
			it := &v2.Upsert{}
			it.SetId(p.ID)
			it.SetSort(asc(nil, int64(i)))
			it.SetStickerPack(v2.StickerPackRow_builder{
				Id: p.ID, Name: p.Name, Publisher: p.Publisher, Description: p.Description, Animated: p.Animated,
				Lottie: p.Lottie, TrayPath: p.TrayLocalPath, Count: uint32(max(p.StickerCount, 0)),
				Installed: p.Installed, Fetched: p.ContentsFetchedAt > 0,
			}.Build())
			out = append(out, it)
		}
		return out, nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "sticker") }
	return w, nil, nil
}

func (rs *Reads) stickerPackView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetStickerPack().GetPackId()
	if id == "" {
		return nil, nil, invalid("sticker_pack needs a pack_id")
	}
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		ss, err := rs.r.ListPackStickers(ctx, id)
		if err != nil {
			return nil, err
		}
		return stickerItems(limited(ss, max)), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "sticker") }
	return w, nil, nil
}
