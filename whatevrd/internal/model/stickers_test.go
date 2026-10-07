package model

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/store"
)

func hash(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func hx(b byte) string { return hex.EncodeToString(hash(b)) }

func recentsIn(note string, t int, ms ...*waHistorySync.StickerMetadata) core.Input {
	return in(core.KindHistoryExtra, core.HistoryExtraHead{Notification: note, SyncType: "INITIAL_BOOTSTRAP"},
		pb(&waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_INITIAL_BOOTSTRAP.Enum(), RecentStickers: ms}), at(t))
}

func recentMeta(plain, enc byte, used int, weight float32) *waHistorySync.StickerMetadata {
	return &waHistorySync.StickerMetadata{FileSHA256: hash(plain), FileEncSHA256: hash(enc), Mimetype: proto.String("image/webp"),
		LastStickerSentTS: proto.Int64(at(used).UnixMilli()), Weight: proto.Float32(weight), Width: proto.Uint32(512), Height: proto.Uint32(512)}
}

func stickerIn(h core.StickerHead, body []byte, t int) core.Input {
	return in(core.KindSticker, h, body, at(t))
}

func jsonOf(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func packItem(plain, enc byte, emoji string) *types.StickerPackItem {
	return &types.StickerPackItem{FileHash: hash(plain), EncFileHash: hash(enc), MimeType: "image/webp", Width: 512, Height: 512, Emojis: []string{emoji}}
}

func favIn(idx string, enc byte, ts, t int) core.Input {
	return appState("regular_low", uint64(t), "set", []string{"favoriteSticker", idx}, &waSyncAction.SyncActionValue{Timestamp: proto.Int64(at(ts).UnixMilli()),
		StickerAction: &waSyncAction.StickerAction{FileEncSHA256: hash(enc), MediaKey: hash(9), IsFavorite: proto.Bool(true), Mimetype: proto.String("image/webp")}}, t)
}

func keys(ss []store.Sticker) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.CacheKey)
	}
	return out
}

// the library merges every source by the plaintext hash, and any order of
// inputs builds the same one
func TestTheStickerLibrary(t *testing.T) {
	sent := &waE2E.Message{StickerMessage: &waE2E.StickerMessage{FileSHA256: hash(3), FileEncSHA256: hash(13), Mimetype: proto.String("image/webp"),
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("Q")}}}
	ins := []core.Input{
		recentsIn("N1", 1, recentMeta(1, 11, -100, 2), recentMeta(2, 12, -200, 5)),
		recentsIn("N2", 2, recentMeta(1, 11, -90, 1)),
		msgIn("S1", ashaPN, mePN, "", true, 50, sent),
		// an older send of it under another upload
		msgIn("S0", ashaPN, mePN, "", true, 40, &waE2E.Message{StickerMessage: &waE2E.StickerMessage{FileSHA256: hash(3), FileEncSHA256: hash(33)}}),
		// a received sticker is no use of ours
		msgIn("S2", ashaPN, ashaPN, "", false, 51, &waE2E.Message{StickerMessage: &waE2E.StickerMessage{FileSHA256: hash(5), FileEncSHA256: hash(15)}}),
		// removed at the use it had, by the hash of its upload, so gone from
		// the recents
		appState("regular_low", 3, "set", []string{"removeRecentSticker", hx(12)}, &waSyncAction.SyncActionValue{
			RemoveRecentStickerAction: &waSyncAction.RemoveRecentStickerAction{LastStickerSentTS: proto.Int64(at(-200).UnixMilli())}}, 4),
		// a favorite by its encrypted hash alone, until its file ties it to 4
		favIn(hx(14), 14, 20, 5),
		stickerIn(core.StickerHead{Op: core.StickerFile, Key: hx(4), Enc: hx(14), Path: "/s/4.webp", Animated: true}, nil, 6),
		// a favorite of 1 under another upload, keyed by its plaintext hash
		favIn(hx(1), 16, 30, 7),
		stickerIn(core.StickerHead{Op: core.StickerIndex}, jsonOf([]types.StickerPack{
			{StickerPackID: "P1", Name: "Cats", ImageDataHash: "h1", TrayImageID: "tr1"},
			{StickerPackID: "P2", Name: "Dogs", ImageDataHash: "h2"},
			{StickerPackID: "P3", Name: "Owls", ImageDataHash: "h3"},
		}), 8),
		stickerIn(core.StickerHead{Op: core.StickerPack, Key: "P1"}, jsonOf(types.StickerPack{StickerPackID: "P1", ImageDataHash: "h1",
			Stickers: []*types.StickerPackItem{packItem(6, 26, "😺"), packItem(1, 21, "😸")}}), 9),
		// fetched before the pack changed
		stickerIn(core.StickerHead{Op: core.StickerPack, Key: "P2"}, jsonOf(types.StickerPack{StickerPackID: "P2", ImageDataHash: "old",
			Stickers: []*types.StickerPackItem{packItem(7, 27, "🐶"), packItem(6, 26, "🐕")}}), 10),
		stickerIn(core.StickerHead{Op: core.StickerInstalled, Key: "P1", On: true}, nil, 11),
		stickerIn(core.StickerHead{Op: core.StickerInstalled, Key: "P1"}, nil, 12),
		stickerIn(core.StickerHead{Op: core.StickerInstalled, Key: "P3", On: true}, nil, 13),
		stickerIn(core.StickerHead{Op: core.StickerInstalled, Key: "P2", On: true}, nil, 14),
		stickerIn(core.StickerHead{Op: core.StickerTray, Key: "tr1", Path: "/s/tr1.png"}, nil, 15),
		stickerIn(core.StickerHead{Op: core.StickerUpload, Key: hx(1)}, []byte("mine"), 16),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		ctx := context.Background()
		rec, err := r.ListRecentStickers(ctx, -1)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := keys(rec), []string{hx(3), hx(1)}; !reflect.DeepEqual(got, want) {
			t.Fatalf("recents %v, want %v", got, want)
		}
		if rec[0].LastUsed != at(50).Unix() || rec[0].EncCacheKey != hx(13) || rec[1].LastUsed != at(-90).Unix() || rec[1].RecentWeight != 2 {
			t.Fatalf("recents %+v", rec)
		}
		var m waE2E.StickerMessage
		if proto.Unmarshal(rec[0].StickerPayload, &m) != nil || m.ContextInfo != nil {
			t.Fatalf("a sent sticker keeps its quote %v", &m)
		}
		a := rec[1]
		if !a.IsFavorite || a.FavoriteTS != at(30).Unix() || a.PackID != "P1" || a.PackOrder != 1 || a.Emojis != "😸" ||
			string(a.UploadPayload) != "mine" || a.UploadTS != at(16).Unix() || a.EncCacheKey != hx(21) {
			t.Fatalf("1 is %+v", a)
		}
		fav, _ := r.ListFavoriteStickers(ctx, -1)
		if got, want := keys(fav), []string{hx(1), hx(4)}; !reflect.DeepEqual(got, want) {
			t.Fatalf("favorites %v, want %v", got, want)
		}
		if f := fav[1]; f.LocalPath != "/s/4.webp" || !f.IsAnimated || f.FavoriteTS != at(20).Unix() {
			t.Fatalf("4 is %+v", f)
		}
		if s, ok, _ := r.Sticker(ctx, "enc:"+hx(14)); !ok || s.CacheKey != hx(4) {
			t.Fatalf("enc key finds %+v %v", s, ok)
		}
		all, _ := r.ListAllStickers(ctx, -1)
		if got, want := keys(all), []string{hx(3), hx(1), hx(4), hx(2), hx(6), hx(7)}; !reflect.DeepEqual(got, want) {
			t.Fatalf("all %v, want %v", got, want)
		}
		// a sticker in two packs shows the newest fetched
		if s := all[4]; s.PackID != "P2" || s.Emojis != "🐕" {
			t.Fatalf("6 is %+v", s)
		}
		if got, _ := r.SearchStickers(ctx, "cat", -1); !reflect.DeepEqual(keys(got), []string{hx(1)}) {
			t.Fatalf("search by pack name %v", keys(got))
		}
		if got, _ := r.SearchStickers(ctx, "🐶", -1); !reflect.DeepEqual(keys(got), []string{hx(7)}) {
			t.Fatalf("search by emoji %v", keys(got))
		}
		packs, _ := r.ListStickerPacks(ctx)
		var ids []string
		for _, p := range packs {
			ids = append(ids, p.ID)
		}
		if want := []string{"P2", "P3", "P1"}; !reflect.DeepEqual(ids, want) {
			t.Fatalf("packs %v, want %v", ids, want)
		}
		if p := packs[2]; p.Installed || p.ContentsFetchedAt != at(9).Unix() || p.StickerCount != 2 || p.TrayLocalPath != "/s/tr1.png" {
			t.Fatalf("P1 %+v", p)
		}
		if p := packs[0]; !p.Installed || p.InstalledTS != at(14).Unix() || p.ContentsFetchedAt != 0 {
			t.Fatalf("P2 %+v", p)
		}
		items, _ := r.ListPackStickers(ctx, "P1")
		if got, want := keys(items), []string{hx(6), hx(1)}; !reflect.DeepEqual(got, want) || !items[1].IsFavorite {
			t.Fatalf("P1 stickers %v, want %v", got, want)
		}
	})
}

// a sticker used again after it was removed is back
func TestARemovedRecentComesBack(t *testing.T) {
	ins := []core.Input{
		recentsIn("N1", 1, recentMeta(2, 12, -200, 5)),
		appState("regular_low", 3, "set", []string{"removeRecentSticker", hx(2)}, &waSyncAction.SyncActionValue{
			RemoveRecentStickerAction: &waSyncAction.RemoveRecentStickerAction{LastStickerSentTS: proto.Int64(at(-200).UnixMilli())}}, 4),
		recentsIn("N2", 5, recentMeta(2, 12, -150, 1)),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		rec, _ := r.ListRecentStickers(context.Background(), -1)
		if len(rec) != 1 || rec[0].LastUsed != at(-150).Unix() {
			t.Fatalf("recents %+v", rec)
		}
	})
}
