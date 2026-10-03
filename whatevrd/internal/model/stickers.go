package model

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/store"
)

// the sticker picker's library: recents from history and from what we sent,
// favorites from app state, packs and files from what the daemon fetched.
// a sticker is its plaintext hash; one known only by an encrypted hash, a
// favorite say, is "enc:"+that until some source ties the two.
var stickersDomain = core.Domain{
	Name:    "stickers",
	Version: 1,
	Tables:  []string{"stk", "stk_recent"},
	Schema: []string{
		// value is the head; per op and key the newest wins
		`CREATE TABLE stk (
			op    TEXT NOT NULL,
			key   TEXT NOT NULL,
			t     INTEGER NOT NULL,
			value BLOB NOT NULL,
			body  BLOB NOT NULL,
			PRIMARY KEY (op, key)
		) WITHOUT ROWID`,
		// the history's recents. used and weight only grow; meta is the
		// StickerMetadata of the newest use
		`CREATE TABLE stk_recent (
			plain  TEXT PRIMARY KEY,
			used   INTEGER NOT NULL,
			weight REAL NOT NULL,
			meta   BLOB NOT NULL
		) WITHOUT ROWID`,
	},
	Folds: map[string]core.FoldFunc{
		core.KindSticker:      foldSticker,
		core.KindHistoryExtra: foldRecentStickers,
	},
}

func foldSticker(tx *core.Tx, in core.Input) error {
	h, err := head[core.StickerHead](in)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO stk (op, key, t, value, body) VALUES (?, ?, ?, ?, COALESCE(?, x''))
		ON CONFLICT (op, key) DO UPDATE SET t = excluded.t, value = excluded.value, body = excluded.body
		WHERE (excluded.t, excluded.value, excluded.body) > (stk.t, stk.value, stk.body)`,
		h.Op, h.Key, in.At.UnixMilli(), []byte(in.Head), in.Body); err != nil {
		return err
	}
	tx.Touch("sticker", h.Op)
	return nil
}

func foldRecentStickers(tx *core.Tx, in core.Input) error {
	if len(in.Body) == 0 {
		return nil
	}
	var hs waHistorySync.HistorySync
	if err := proto.Unmarshal(in.Body, &hs); err != nil {
		return err
	}
	for _, m := range hs.GetRecentStickers() {
		if len(m.GetFileSHA256()) == 0 {
			continue
		}
		meta, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO stk_recent (plain, used, weight, meta) VALUES (?, ?, ?, ?)
			ON CONFLICT (plain) DO UPDATE SET used = MAX(stk_recent.used, excluded.used),
				weight = MAX(stk_recent.weight, excluded.weight),
				meta = CASE WHEN (excluded.used, excluded.meta) > (stk_recent.used, stk_recent.meta) THEN excluded.meta ELSE stk_recent.meta END`,
			hex.EncodeToString(m.GetFileSHA256()), unixSec(m.GetLastStickerSentTS()), m.GetWeight(), meta); err != nil {
			return err
		}
		tx.Touch("sticker", "recent")
	}
	return nil
}

// unixSec takes whatsapp's seconds or milliseconds to seconds
func unixSec(v int64) int64 {
	if v > 1_000_000_000_000 {
		return v / 1000
	}
	return max(v, 0)
}

func lottieMime(mime string) bool {
	return strings.EqualFold(strings.TrimSpace(mime), "application/was")
}

// stickerLib is every sticker the sources know, merged by key.
type stickerLib struct {
	by map[string]*store.Sticker
	// rank of the source a sticker's metadata came from, lower is better
	rank map[string]int
	// every encrypted hash a sticker was seen under
	encs map[string][]string
	// enc to plain, from every source that has both
	plain map[string]string
	// pack id to its fetched body and when
	packs map[string]stkRow
	index stkRow
	inst  map[string]stkRow
	trays map[string]string
}

type stkRow struct {
	t    int64
	head core.StickerHead
	body []byte
}

// sources, best metadata first
const (
	rankPack = iota
	rankRecent
	rankSent
	rankFavorite
)

// sentStickers is how many of our newest sent stickers count as recent uses
const sentStickers = 1000

func (l *stickerLib) key(plain, enc string) string {
	if plain == "" {
		plain = l.plain[enc]
	}
	if plain != "" {
		return plain
	}
	return "enc:" + enc
}

// put merges what one source says of a sticker, and returns it.
func (l *stickerLib) put(plain string, rank int, m *waE2E.StickerMessage) *store.Sticker {
	enc := hex.EncodeToString(m.GetFileEncSHA256())
	k := l.key(plain, enc)
	s := l.by[k]
	if s == nil {
		s = &store.Sticker{CacheKey: k}
		l.by[k] = s
		l.rank[k] = rankFavorite + 1
	}
	if enc != "" && !contains(l.encs[k], enc) {
		l.encs[k] = append(l.encs[k], enc)
	}
	if rank < l.rank[k] {
		l.rank[k] = rank
		mime := strings.TrimSpace(m.GetMimetype())
		if mime == "" {
			mime = "image/webp"
		}
		s.EncCacheKey, s.MimeType = enc, mime
		s.Width, s.Height = int32(m.GetWidth()), int32(m.GetHeight())
		s.IsAnimated = lottieMime(mime) || m.GetIsLottie() || m.GetIsAnimated()
		s.StickerPayload, _ = proto.MarshalOptions{Deterministic: true}.Marshal(m)
	}
	return s
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func packItems(body []byte) (types.StickerPack, error) {
	var p types.StickerPack
	err := json.Unmarshal(body, &p)
	return p, err
}

func packItemMessage(it *types.StickerPackItem) *waE2E.StickerMessage {
	return &waE2E.StickerMessage{
		URL: proto.String(it.URL), DirectPath: proto.String(it.DirectPath), MediaKey: it.MediaKey,
		FileEncSHA256: it.EncFileHash, FileSHA256: it.FileHash, FileLength: proto.Uint64(uint64(it.FileSize)),
		Mimetype: proto.String(it.MimeType), Width: proto.Uint32(uint32(it.Width)), Height: proto.Uint32(uint32(it.Height)),
		IsAnimated: proto.Bool(lottieMime(it.MimeType)),
	}
}

func recentMessage(m *waHistorySync.StickerMetadata) *waE2E.StickerMessage {
	return &waE2E.StickerMessage{
		URL: proto.String(m.GetURL()), DirectPath: proto.String(m.GetDirectPath()), MediaKey: m.GetMediaKey(),
		FileEncSHA256: m.GetFileEncSHA256(), FileSHA256: m.GetFileSHA256(), FileLength: proto.Uint64(m.GetFileLength()),
		Mimetype: proto.String(m.GetMimetype()), Width: proto.Uint32(m.GetWidth()), Height: proto.Uint32(m.GetHeight()),
		IsAnimated: proto.Bool(m.GetIsLottie()), IsLottie: proto.Bool(m.GetIsLottie()),
	}
}

func favoriteMessage(a *waSyncAction.StickerAction) *waE2E.StickerMessage {
	return &waE2E.StickerMessage{
		URL: proto.String(a.GetURL()), DirectPath: proto.String(a.GetDirectPath()), MediaKey: a.GetMediaKey(),
		FileEncSHA256: a.GetFileEncSHA256(), FileLength: proto.Uint64(a.GetFileLength()),
		Mimetype: proto.String(a.GetMimetype()), Width: proto.Uint32(a.GetWidth()), Height: proto.Uint32(a.GetHeight()),
		IsAnimated: proto.Bool(a.GetIsLottie()), IsLottie: proto.Bool(a.GetIsLottie()),
	}
}

// stickers gathers the whole library. it is small: recents, favorites and
// the packs fetched.
func (r *Reader) stickers(ctx context.Context) (*stickerLib, error) {
	l := &stickerLib{by: map[string]*store.Sticker{}, rank: map[string]int{}, encs: map[string][]string{},
		plain: map[string]string{}, packs: map[string]stkRow{}, inst: map[string]stkRow{}, trays: map[string]string{}}
	files := map[string]stkRow{}
	uploads := map[string]stkRow{}

	rows, err := r.db.QueryContext(ctx, `SELECT t, value, body FROM stk`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x stkRow
		var value []byte
		if err := rows.Scan(&x.t, &value, &x.body); err != nil {
			rows.Close()
			return nil, err
		}
		if json.Unmarshal(value, &x.head) != nil {
			continue
		}
		switch x.head.Op {
		case core.StickerIndex:
			l.index = x
		case core.StickerPack:
			l.packs[x.head.Key] = x
		case core.StickerInstalled:
			l.inst[x.head.Key] = x
		case core.StickerTray:
			l.trays[x.head.Key] = x.head.Path
		case core.StickerFile:
			files[x.head.Key] = x
			if x.head.Enc != "" {
				l.plain[x.head.Enc] = x.head.Key
			}
		case core.StickerUpload:
			uploads[x.head.Key] = x
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type recent struct {
		used   int64
		weight float64
		m      *waHistorySync.StickerMetadata
	}
	var recents []recent
	rows, err = r.db.QueryContext(ctx, `SELECT used, weight, meta FROM stk_recent`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x recent
		var meta []byte
		if err := rows.Scan(&x.used, &x.weight, &meta); err != nil {
			rows.Close()
			return nil, err
		}
		x.m = &waHistorySync.StickerMetadata{}
		if proto.Unmarshal(meta, x.m) == nil {
			recents = append(recents, x)
			l.plain[hex.EncodeToString(x.m.GetFileEncSHA256())] = hex.EncodeToString(x.m.GetFileSHA256())
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type sent struct {
		t int64
		m *waE2E.StickerMessage
	}
	var sents []sent
	rows, err = r.db.QueryContext(ctx, `SELECT t, body, off IS NOT NULL FROM msg
		WHERE from_me = 1 AND kind = 'stickerMessage' ORDER BY t DESC LIMIT ?`, sentStickers)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.T, &m.Body, &m.History); err != nil {
			rows.Close()
			return nil, err
		}
		c, _ := m.Content()
		if s := Unwrap(c).Msg.GetStickerMessage(); len(s.GetFileSHA256()) > 0 {
			s = proto.Clone(s).(*waE2E.StickerMessage)
			s.ContextInfo = nil
			sents = append(sents, sent{m.T, s})
			l.plain[hex.EncodeToString(s.GetFileEncSHA256())] = hex.EncodeToString(s.GetFileSHA256())
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type pack struct {
		id string
		t  int64
		p  types.StickerPack
	}
	var packs []pack
	for id, x := range l.packs {
		p, err := packItems(x.body)
		if err != nil {
			continue
		}
		packs = append(packs, pack{id, x.t, p})
		for _, it := range p.Stickers {
			if it != nil && len(it.FileHash) > 0 && len(it.EncFileHash) > 0 {
				l.plain[hex.EncodeToString(it.EncFileHash)] = hex.EncodeToString(it.FileHash)
			}
		}
	}
	// a sticker in several packs shows the newest fetched
	sort.Slice(packs, func(i, j int) bool {
		if packs[i].t != packs[j].t {
			return packs[i].t > packs[j].t
		}
		return packs[i].id < packs[j].id
	})

	for _, p := range packs {
		for i, it := range p.p.Stickers {
			if it == nil || len(it.FileHash) == 0 && len(it.EncFileHash) == 0 {
				continue
			}
			s := l.put(hex.EncodeToString(it.FileHash), rankPack, packItemMessage(it))
			if s.PackID == "" {
				s.PackID, s.PackOrder = p.id, int32(i)
				s.Emojis, s.AccessibilityText = strings.Join(it.Emojis, " "), it.AccessibilityText
			}
		}
	}
	for _, x := range recents {
		s := l.put(hex.EncodeToString(x.m.GetFileSHA256()), rankRecent, recentMessage(x.m))
		s.LastUsed, s.RecentWeight = max(s.LastUsed, x.used), max(s.RecentWeight, x.weight)
	}
	for _, x := range sents {
		s := l.put(hex.EncodeToString(x.m.GetFileSHA256()), rankSent, x.m)
		s.LastUsed = max(s.LastUsed, x.t/1000)
	}

	rows, err = r.db.QueryContext(ctx, `SELECT a.a, a.t, i.body FROM appstate a JOIN inputs i ON i.seq = a.seq
		WHERE a.kind = ? AND a.op = 'set' AND a.on_ = 1`, asFavSticker)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var idx string
		var t int64
		var body []byte
		if err := rows.Scan(&idx, &t, &body); err != nil {
			rows.Close()
			return nil, err
		}
		var v waSyncAction.SyncActionValue
		if proto.Unmarshal(body, &v) != nil || len(v.GetStickerAction().GetFileEncSHA256()) == 0 {
			continue
		}
		a := v.GetStickerAction()
		// the index is the plaintext hash when whatsapp keyed it so
		plain := strings.ToLower(idx)
		if plain == hex.EncodeToString(a.GetFileEncSHA256()) || len(plain) != 64 || l.plain[hex.EncodeToString(a.GetFileEncSHA256())] != "" {
			plain = ""
		}
		s := l.put(plain, rankFavorite, favoriteMessage(a))
		s.IsFavorite, s.FavoriteTS = true, max(s.FavoriteTS, unixSec(t))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db.QueryContext(ctx, `SELECT a, n FROM appstate WHERE kind = ? AND op = 'set'`, asRemoveRecent)
	if err != nil {
		return nil, err
	}
	removed := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, err
		}
		removed[strings.ToLower(k)] = unixSec(n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for k, s := range l.by {
		// a removed recent is back once it is used after the removal
		for _, key := range append([]string{strings.TrimPrefix(k, "enc:")}, l.encs[k]...) {
			if n, ok := removed[key]; ok && (n == 0 || n >= s.LastUsed) {
				s.LastUsed, s.RecentWeight = 0, 0
			}
		}
		if f, ok := files[k]; ok {
			s.LocalPath, s.ArchivePath, s.IsAnimated = f.head.Path, f.head.Archive, f.head.Animated
		}
		if u, ok := uploads[k]; ok && len(u.body) > 0 {
			s.UploadPayload, s.UploadTS = u.body, u.t/1000
		}
	}
	return l, nil
}

func (l *stickerLib) list(keep func(*store.Sticker) bool, less func(a, b *store.Sticker) bool, limit int) []store.Sticker {
	var out []*store.Sticker
	for _, s := range l.by {
		if keep(s) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if less(out[i], out[j]) {
			return true
		}
		if less(out[j], out[i]) {
			return false
		}
		return out[i].CacheKey < out[j].CacheKey
	})
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	res := make([]store.Sticker, len(out))
	for i, s := range out {
		res[i] = *s
	}
	return res
}

func touched(s *store.Sticker) int64 { return max(s.LastUsed, s.FavoriteTS) }

// ListRecentStickers is the stickers used, the newest first. a negative limit
// is all of them.
func (r *Reader) ListRecentStickers(ctx context.Context, limit int) ([]store.Sticker, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return nil, err
	}
	return l.list(func(s *store.Sticker) bool { return s.LastUsed > 0 }, func(a, b *store.Sticker) bool {
		return a.LastUsed > b.LastUsed || a.LastUsed == b.LastUsed && a.RecentWeight > b.RecentWeight
	}, limit), nil
}

// ListFavoriteStickers is the favorites, the newest first.
func (r *Reader) ListFavoriteStickers(ctx context.Context, limit int) ([]store.Sticker, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return nil, err
	}
	return l.list(func(s *store.Sticker) bool { return s.IsFavorite }, func(a, b *store.Sticker) bool {
		return a.FavoriteTS > b.FavoriteTS
	}, limit), nil
}

// ListAllStickers is the whole library, the most recently used or
// favorited first.
func (r *Reader) ListAllStickers(ctx context.Context, limit int) ([]store.Sticker, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return nil, err
	}
	return l.list(func(*store.Sticker) bool { return true }, func(a, b *store.Sticker) bool {
		return touched(a) > touched(b)
	}, limit), nil
}

// SearchStickers matches emoji, accessibility text and pack names.
func (r *Reader) SearchStickers(ctx context.Context, query string, limit int) ([]store.Sticker, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	q := strings.ToLower(query)
	names := map[string]bool{}
	if p, ok := l.indexPacks(); ok {
		for _, x := range p {
			if q != "" && strings.Contains(strings.ToLower(x.Name), q) {
				names[x.StickerPackID] = true
			}
		}
	}
	return l.list(func(s *store.Sticker) bool {
		return strings.Contains(s.Emojis, query) || strings.Contains(strings.ToLower(s.AccessibilityText), q) || names[s.PackID]
	}, func(a, b *store.Sticker) bool {
		if touched(a) != touched(b) {
			return touched(a) > touched(b)
		}
		if a.PackID != b.PackID {
			return a.PackID < b.PackID
		}
		return a.PackOrder < b.PackOrder
	}, limit), nil
}

// Sticker is one sticker by its key. an "enc:" key whose plaintext hash is
// known since finds it under that.
func (r *Reader) Sticker(ctx context.Context, key string) (store.Sticker, bool, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return store.Sticker{}, false, err
	}
	if enc, ok := strings.CutPrefix(key, "enc:"); ok {
		key = l.key("", enc)
	}
	if s := l.by[key]; s != nil {
		return *s, true, nil
	}
	return store.Sticker{}, false, nil
}

func (l *stickerLib) indexPacks() ([]types.StickerPack, bool) {
	var ps []types.StickerPack
	if len(l.index.body) == 0 || json.Unmarshal(l.index.body, &ps) != nil {
		return nil, false
	}
	return ps, true
}

func (l *stickerLib) pack(i int, p types.StickerPack) store.StickerPack {
	out := store.StickerPack{ID: p.StickerPackID, Name: p.Name, Publisher: p.Publisher, Description: p.Description,
		Animated: p.Animated != 0, Lottie: p.Lottie != 0, TrayImageID: p.TrayImageID, TrayLocalPath: l.trays[p.TrayImageID],
		ImageDataHash: p.ImageDataHash, StoreOrder: int32(i)}
	if x, ok := l.inst[p.StickerPackID]; ok && x.head.On {
		out.Installed, out.InstalledTS = true, x.t/1000
	}
	// contents fetched before the pack changed are stale
	if x, ok := l.packs[p.StickerPackID]; ok {
		if c, err := packItems(x.body); err == nil && c.ImageDataHash == p.ImageDataHash {
			out.ContentsFetchedAt = x.t / 1000
			for _, it := range c.Stickers {
				if it != nil && (len(it.FileHash) > 0 || len(it.EncFileHash) > 0) {
					out.StickerCount++
				}
			}
		}
	}
	return out
}

// ListStickerPacks is the store's packs, the installed first, newest
// installed leading, then the rest in store order.
func (r *Reader) ListStickerPacks(ctx context.Context) ([]store.StickerPack, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return nil, err
	}
	ps, _ := l.indexPacks()
	var out []store.StickerPack
	for i, p := range ps {
		if p.StickerPackID != "" {
			out = append(out, l.pack(i, p))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Installed != b.Installed {
			return a.Installed
		}
		if a.Installed {
			return a.InstalledTS > b.InstalledTS
		}
		return a.StoreOrder < b.StoreOrder
	})
	return out, nil
}

// GetStickerPack is one pack of the store's.
func (r *Reader) GetStickerPack(ctx context.Context, id string) (store.StickerPack, bool, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return store.StickerPack{}, false, err
	}
	ps, _ := l.indexPacks()
	for i, p := range ps {
		if p.StickerPackID == id {
			return l.pack(i, p), true, nil
		}
	}
	return store.StickerPack{}, false, nil
}

// ListPackStickers is a fetched pack's stickers in its order.
func (r *Reader) ListPackStickers(ctx context.Context, id string) ([]store.Sticker, error) {
	l, err := r.stickers(ctx)
	if err != nil {
		return nil, err
	}
	x, ok := l.packs[id]
	if !ok {
		return nil, nil
	}
	p, err := packItems(x.body)
	if err != nil {
		return nil, nil
	}
	var out []store.Sticker
	for i, it := range p.Stickers {
		if it == nil || len(it.FileHash) == 0 && len(it.EncFileHash) == 0 {
			continue
		}
		s := *l.by[l.key(hex.EncodeToString(it.FileHash), hex.EncodeToString(it.EncFileHash))]
		s.PackID, s.PackOrder = id, int32(i)
		s.Emojis, s.AccessibilityText = strings.Join(it.Emojis, " "), it.AccessibilityText
		out = append(out, s)
	}
	return out, nil
}

// StickerIndexAt is the newest store index logged and when.
func (r *Reader) StickerIndexAt(ctx context.Context) ([]byte, time.Time, error) {
	var body []byte
	var t int64
	err := r.db.QueryRowContext(ctx, `SELECT body, t FROM stk WHERE op = ? AND key = ''`, core.StickerIndex).Scan(&body, &t)
	if isNoRows(err) {
		return nil, time.Time{}, nil
	}
	return body, time.UnixMilli(t), err
}
