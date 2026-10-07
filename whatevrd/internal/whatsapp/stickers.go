package whatsapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	appstore "whatevrd/internal/store"
)

const (
	// the store whatsapp web reads: every official pack, and their tray art
	// unencrypted by image id. a pack's stickers are ordinary media
	stickerIndexURL = "https://static.whatsapp.net/sticker?cat=all&lg=en&lottie=1"
	stickerImageURL = "https://static.whatsapp.net/sticker?img="

	stickerIndexFor     = 24 * time.Hour
	stickerIndexMax     = 8 << 20
	stickerTrayMax      = 1 << 20
	stickerFetches      = 8
	stickerFetchTimeout = 30 * time.Second
)

// stickers fetches what the picker shows: the store's packs and the files of
// the library's stickers. what it gets goes into the log as sticker inputs.
type stickers struct {
	c *Client

	index sync.Mutex
	mu    sync.Mutex
	packs map[string]bool
	sem   chan struct{}
	kicks chan struct{}
}

func newStickers(c *Client) *stickers {
	return &stickers{c: c, packs: map[string]bool{}, sem: make(chan struct{}, stickerFetches), kicks: make(chan struct{}, 1)}
}

func (s *stickers) connected() {
	select {
	case s.kicks <- struct{}{}:
	default:
	}
}

// run brings the store index up to date on each connect.
func (s *stickers) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.kicks:
		}
		if err := s.refresh(ctx, false); err != nil && ctx.Err() == nil {
			s.c.log.Warn().Err(err).Msg("whatsapp: sticker store index")
		}
	}
}

func (c *Client) logSticker(ctx context.Context, h core.StickerHead, body []byte) error {
	return c.append(ctx, core.KindSticker, h, body)
}

// RefreshStickerPacks fetches the store's packs again.
func (c *Client) RefreshStickerPacks(ctx context.Context) error {
	if err := c.stickers.refresh(ctx, true); err != nil {
		return Errorf(ErrIO, "fetch the sticker store: %v", err)
	}
	return nil
}

// WantStickerPacks is the pack list on screen: the index is fetched when it
// is stale.
func (c *Client) WantStickerPacks() {
	c.spawn(func(ctx context.Context) {
		if err := c.stickers.refresh(ctx, false); err != nil && ctx.Err() == nil {
			c.log.Warn().Err(err).Msg("whatsapp: sticker store index")
		}
	})
}

// refresh fetches the store index once a day, or now when forced, then the
// tray art it lacks.
func (s *stickers) refresh(ctx context.Context, force bool) error {
	c := s.c
	s.index.Lock()
	defer s.index.Unlock()
	old, t, err := c.r.StickerIndexAt(ctx)
	if err != nil {
		return err
	}
	if force || len(old) == 0 || time.Since(t) >= stickerIndexFor {
		body, err := s.get(ctx, stickerIndexURL, stickerIndexMax)
		if err != nil {
			return err
		}
		var ps []types.StickerPack
		if err := json.Unmarshal(body, &ps); err != nil {
			return fmt.Errorf("decode the index: %w", err)
		}
		if len(ps) == 0 {
			return errors.New("the index is empty")
		}
		// compacted, so the same index logs as the same bytes
		var buf bytes.Buffer
		if json.Compact(&buf, body) == nil {
			body = buf.Bytes()
		}
		// logged even when unchanged: the row's time is when it was checked
		if err := c.logSticker(ctx, core.StickerHead{Op: core.StickerIndex}, body); err != nil {
			return err
		}
		c.waitLogged(ctx)
	}
	s.trays(ctx)
	return nil
}

// trays fetches the tray art no row has a file for.
func (s *stickers) trays(ctx context.Context) {
	c := s.c
	packs, err := c.r.ListStickerPacks(ctx)
	if err != nil {
		return
	}
	dir := filepath.Join(c.o.Paths.MediaCacheDir, "stickers", "trays")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, p := range packs {
		if p.TrayImageID == "" {
			continue
		}
		if p.TrayLocalPath != "" {
			if _, err := os.Stat(p.TrayLocalPath); err == nil {
				continue
			}
		}
		wg.Go(func() {
			s.sem <- struct{}{}
			defer func() { <-s.sem }()
			if ctx.Err() != nil {
				return
			}
			path := filepath.Join(dir, safeMediaFileName(p.TrayImageID, ".png"))
			if _, err := os.Stat(path); err != nil {
				data, err := s.get(ctx, stickerImageURL+url.QueryEscape(p.TrayImageID), stickerTrayMax)
				if err == nil {
					err = writeFileAtomic(path, data, 0o600)
				}
				if err != nil {
					c.log.Debug().Err(err).Str("pack", p.ID).Msg("whatsapp: a sticker tray")
					return
				}
			}
			if err := c.logSticker(ctx, core.StickerHead{Op: core.StickerTray, Key: p.TrayImageID, Path: path}, nil); err != nil {
				c.log.Warn().Err(err).Msg("whatsapp: log a sticker tray")
			}
		})
	}
	wg.Wait()
}

func (s *stickers) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: stickerFetchTimeout, Transport: s.c.o.Transport}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the sticker store answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the sticker store sent too much")
	}
	return data, nil
}

// WantStickerPack is a pack on screen: its stickers are fetched when they
// are not, or are from before the pack changed.
func (c *Client) WantStickerPack(id string) {
	s := c.stickers
	s.mu.Lock()
	if s.packs[id] {
		s.mu.Unlock()
		return
	}
	s.packs[id] = true
	s.mu.Unlock()
	c.spawn(func(ctx context.Context) {
		defer func() {
			s.mu.Lock()
			delete(s.packs, id)
			s.mu.Unlock()
		}()
		p, ok, err := c.r.GetStickerPack(ctx, id)
		if err != nil || !ok || p.ContentsFetchedAt > 0 {
			return
		}
		if err := s.fetchPack(ctx, id); err != nil && ctx.Err() == nil {
			c.log.Warn().Err(err).Str("pack", id).Msg("whatsapp: fetch a sticker pack")
		}
	})
}

// fetchPack logs a pack's stickers as whatsapp lists them: keys and emoji,
// no files.
func (s *stickers) fetchPack(ctx context.Context, id string) error {
	c := s.c
	cli, err := c.connected()
	if err != nil {
		return err
	}
	p, err := cli.FetchStickerPack(ctx, id)
	if err != nil {
		return Errorf(ErrIO, "fetch the pack: %v", err)
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := c.logSticker(ctx, core.StickerHead{Op: core.StickerPack, Key: id}, body); err != nil {
		return err
	}
	c.waitLogged(ctx)
	return nil
}

// SetStickerPackInstalled puts a pack on the picker's tabs, or takes it off.
// an installed pack's files are fetched ahead.
func (c *Client) SetStickerPackInstalled(ctx context.Context, id string, on bool) error {
	if _, ok, err := c.r.GetStickerPack(ctx, id); err != nil {
		return err
	} else if !ok {
		return Errorf(ErrNotFound, "no sticker pack %q", id)
	}
	if err := c.logSticker(ctx, core.StickerHead{Op: core.StickerInstalled, Key: id, On: on}, nil); err != nil {
		return err
	}
	c.waitLogged(ctx)
	if on {
		c.spawn(func(ctx context.Context) { c.stickers.warm(ctx, id) })
	}
	return nil
}

func (s *stickers) warm(ctx context.Context, id string) {
	c := s.c
	p, ok, err := c.r.GetStickerPack(ctx, id)
	if err != nil || !ok {
		return
	}
	if p.ContentsFetchedAt == 0 {
		if err := s.fetchPack(ctx, id); err != nil {
			c.log.Warn().Err(err).Str("pack", id).Msg("whatsapp: fetch an installed sticker pack")
			return
		}
	}
	ss, err := c.r.ListPackStickers(ctx, id)
	if err != nil {
		return
	}
	for _, x := range ss {
		if ctx.Err() != nil {
			return
		}
		if x.LocalPath != "" {
			continue
		}
		if _, err := s.file(ctx, x.CacheKey, false); err != nil {
			c.log.Debug().Err(err).Str("sticker", x.CacheKey).Msg("whatsapp: warm a sticker")
		}
	}
}

// DownloadSticker fetches a library sticker's file; it lands on its rows.
func (c *Client) DownloadSticker(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return Errorf(ErrInvalid, "a sticker id is needed")
	}
	_, err := c.stickers.file(ctx, key, false)
	return err
}

// file makes sure a sticker's file is on disk, and with archive a lottie's
// archive too: that is what goes out on a send. an "enc:" sticker is keyed by
// the hash of its bytes once they are here.
func (s *stickers) file(ctx context.Context, key string, archive bool) (appstore.Sticker, error) {
	c := s.c
	x, ok, err := c.r.Sticker(ctx, key)
	if err != nil {
		return x, err
	}
	if !ok {
		return x, Errorf(ErrNotFound, "no sticker %q", key)
	}
	lottie := isAnimatedSticker(x.MimeType)
	if have(x.LocalPath) && (!archive || !lottie || have(x.ArchivePath)) {
		return x, nil
	}
	release, err := c.media.lockKey(ctx, "sticker:"+x.CacheKey)
	if err != nil {
		return x, err
	}
	defer release()
	// someone may have fetched it while this waited
	if y, ok, err := c.r.Sticker(ctx, x.CacheKey); err == nil && ok && have(y.LocalPath) && (!archive || !lottie || have(y.ArchivePath)) {
		return y, nil
	}

	dir := filepath.Join(c.o.Paths.MediaCacheDir, "stickers")
	if plain, isPlain := strings.CutPrefix(x.CacheKey, "enc:"); !isPlain && plain != "" {
		// a chat already fetched it
		raw := filepath.Join(dir, x.CacheKey+mediaExtension(x.MimeType))
		if have(raw) {
			return s.landed(ctx, x, x.CacheKey, raw, nil)
		}
	}
	if len(x.StickerPayload) == 0 {
		return x, Errorf(ErrRejected, "the sticker has no download keys")
	}
	var sm waE2E.StickerMessage
	if err := proto.Unmarshal(x.StickerPayload, &sm); err != nil {
		return x, Errorf(ErrRejected, "the sticker's keys do not decode")
	}
	cli, err := c.connected()
	if err != nil {
		return x, err
	}
	s.sem <- struct{}{}
	defer func() { <-s.sem }()
	dctx, cancel := context.WithTimeout(ctx, stickerFetchTimeout)
	defer cancel()
	var d whatsmeow.DownloadableMessage = &sm
	if sm.GetDirectPath() != "" && placeholderURL(sm.GetURL()) {
		d = byDirectPath{&sm}
	}
	data, err := cli.Download(dctx, d)
	// a favorite has no plaintext hash to check, but whatsmeow checked the
	// mac and hands back the bytes
	if errors.Is(err, whatsmeow.ErrInvalidMediaSHA256) && len(sm.GetFileSHA256()) == 0 && len(data) > 0 {
		err = nil
	}
	switch {
	case err != nil && staleMedia(err):
		return x, Errorf(ErrNotFound, "the sticker is gone from WhatsApp's servers")
	case err != nil:
		return x, Errorf(ErrIO, "download the sticker: %v", err)
	case len(data) == 0 || len(data) > maxInboundBytes:
		return x, Errorf(ErrRejected, "the sticker is empty or too big")
	}
	plain := x.CacheKey
	if strings.HasPrefix(plain, "enc:") {
		sum := sha256.Sum256(data)
		plain = hex.EncodeToString(sum[:])
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return x, Errorf(ErrIO, "%v", err)
	}
	path := filepath.Join(dir, plain+mediaExtension(x.MimeType))
	repairWebPAlphaFlagBytes(data)
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return x, Errorf(ErrIO, "%v", err)
	}
	return s.landed(ctx, x, plain, path, data)
}

// landed logs a sticker's raw file, a lottie's archive extracted next to it.
// data is the file's bytes when they are at hand.
func (s *stickers) landed(ctx context.Context, x appstore.Sticker, plain, path string, data []byte) (appstore.Sticker, error) {
	c := s.c
	h := core.StickerHead{Op: core.StickerFile, Key: plain, Enc: x.EncCacheKey, Path: path, Animated: x.IsAnimated}
	if isAnimatedSticker(x.MimeType) {
		json, err := extractLottie(path)
		if err != nil {
			return x, err
		}
		h.Path, h.Archive, h.Animated = json, path, true
	} else if strings.EqualFold(strings.TrimSpace(x.MimeType), "image/webp") {
		// the library can't tell an animated webp from a still one; the bytes can
		if data == nil {
			data, _ = readHead(path, 64<<10)
		}
		h.Animated = isAnimatedWebP(data)
	}
	if err := c.logSticker(ctx, h, nil); err != nil {
		return x, err
	}
	c.waitLogged(ctx)
	x.CacheKey, x.LocalPath, x.ArchivePath, x.IsAnimated = plain, h.Path, h.Archive, h.Animated
	return x, nil
}

func have(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func readHead(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

// uploaded keeps the keys our upload of a library sticker made, so the next
// send of it skips the upload. key is the library's.
func (s *stickers) uploaded(ctx context.Context, key string, sm *waE2E.StickerMessage) {
	if key == "" || sm.GetDirectPath() == "" {
		return
	}
	sm = proto.Clone(sm).(*waE2E.StickerMessage)
	sm.ContextInfo = nil
	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(sm)
	if err != nil {
		return
	}
	if err := s.c.logSticker(ctx, core.StickerHead{Op: core.StickerUpload, Key: key}, body); err != nil {
		s.c.log.Warn().Err(err).Msg("whatsapp: log a sticker upload")
	}
}

// SetStickerFavorite favorites a sticker on every device, or takes it off.
// it is named by its library key, or by a message carrying it.
func (c *Client) SetStickerFavorite(ctx context.Context, key, messageID string, on bool) error {
	cli, err := c.loggedIn()
	if err != nil {
		return err
	}
	var sm *waE2E.StickerMessage
	mime := ""
	if key != "" {
		x, ok, err := c.r.Sticker(ctx, key)
		if err != nil {
			return err
		}
		if ok {
			key, mime = x.CacheKey, x.MimeType
			sm = &waE2E.StickerMessage{}
			if proto.Unmarshal(x.StickerPayload, sm) != nil {
				sm = nil
			}
		}
	}
	if sm == nil && messageID != "" {
		chat, ok, err := c.r.MessageChat(ctx, messageID)
		if err != nil {
			return err
		}
		if !ok {
			return Errorf(ErrNotFound, "no message %q", messageID)
		}
		m, _, err := c.message(ctx, Ref{Chat: chat, ID: messageID})
		if err != nil {
			return err
		}
		raw, _ := m.Content()
		sm = model.Unwrap(raw).Msg.GetStickerMessage()
		if sm == nil {
			return Errorf(ErrInvalid, "the message is not a sticker")
		}
		mime = sm.GetMimetype()
		if key == "" {
			key = hex.EncodeToString(sm.GetFileSHA256())
			if key == "" {
				key = "enc:" + hex.EncodeToString(sm.GetFileEncSHA256())
			}
		}
	}
	if sm == nil || len(sm.GetFileEncSHA256()) == 0 {
		return Errorf(ErrNotFound, "no sticker %q", key)
	}
	if strings.TrimSpace(mime) == "" {
		mime = "image/webp"
	}
	a := &waSyncAction.StickerAction{
		URL: proto.String(sm.GetURL()), FileEncSHA256: sm.GetFileEncSHA256(), MediaKey: sm.GetMediaKey(),
		Mimetype: proto.String(mime), Width: proto.Uint32(sm.GetWidth()), Height: proto.Uint32(sm.GetHeight()),
		DirectPath: proto.String(sm.GetDirectPath()), FileLength: proto.Uint64(sm.GetFileLength()),
		IsFavorite: proto.Bool(on), IsLottie: proto.Bool(sm.GetIsLottie() || isAnimatedSticker(mime)),
	}
	// keyed by the plaintext hash, as removeRecentSticker comes; an enc-only
	// one by what it has
	index := strings.ToLower(strings.TrimPrefix(key, "enc:"))
	return c.sendAppState(ctx, cli, appstate.PatchInfo{
		Type: appstate.WAPatchRegularLow,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexFavoriteSticker, index},
			Version: 2,
			Value:   &waSyncAction.SyncActionValue{StickerAction: a},
		}},
	})
}
