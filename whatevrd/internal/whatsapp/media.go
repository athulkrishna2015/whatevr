package whatsapp

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	appstore "whatevrd/internal/store"
)

const (
	mediaRetryTimeout = 30 * time.Second
	// maxInboundBytes is far above what we send: refusing what the phone
	// already shows is worse than spending the disk
	maxInboundBytes = 2 << 30
	// lottieJSONMax caps the animation.json out of an animated sticker
	lottieJSONMax = 25 << 20
)

// media is every file the daemon fetches or makes for a message.
type media struct {
	c    *Client
	maps *mapFetcher
	http *http.Client

	mu        sync.Mutex
	downloads map[string]*download
	retries   map[string]*mediaRetry
	streams   map[string]*streamEntry
	keys      map[string]*keyLock

	// arrivals is live messages to look at once they fold
	arrivals chan arrival
	// made is files a poster or waveform is wanted for
	made chan made

	srv streamServer
}

type download struct {
	done      chan struct{}
	cancel    context.CancelFunc
	cancelled atomic.Bool
	err       error
}

type mediaRetry struct {
	done       chan struct{}
	key        []byte
	directPath string
	err        error
	completed  bool
}

type keyLock struct {
	token chan struct{}
	refs  int
}

type arrival struct {
	ref Ref
	// seq is the log's end when it arrived; the message folds by then
	seq int64
}

type made struct {
	chat, id, kind, path string
}

func newMedia(c *Client) *media {
	return &media{
		c:         c,
		maps:      newMapFetcher(filepath.Join(c.o.Paths.CacheDir, "map-tiles"), MapUserAgent, DefaultMapTileURLTemplate, c.o.Transport),
		http:      &http.Client{Timeout: 2 * time.Minute, Transport: c.o.Transport},
		downloads: map[string]*download{},
		retries:   map[string]*mediaRetry{},
		streams:   map[string]*streamEntry{},
		keys:      map[string]*keyLock{},
		arrivals:  make(chan arrival, 256),
		made:      make(chan made, 256),
	}
}

// target is a message as the media work needs it.
type target struct {
	m model.Message
	// key is the chat's key and the id, one per message whatever address it
	// came under
	key string
	sm  appstore.Message
}

func (md *media) target(ctx context.Context, ref Ref) (target, error) {
	m, w, err := md.c.message(ctx, ref)
	if err != nil {
		return target{}, err
	}
	sm, _, ok := NewDecoder(WorldNames(w), md.c.o.Paths.MediaCacheDir).Model(ctx, w, m)
	if !ok || !appstore.MessageCarriesMedia(sm) {
		return target{}, Errorf(ErrRejected, "the message has nothing to download")
	}
	sm.MediaLocalPath = m.Facts.Local.File
	return target{m: m, key: w.Now(model.Norm(m.Chat)) + "/" + m.ID, sm: sm}, nil
}

func (t target) location() bool {
	return t.sm.MediaKind == appstore.MediaKindLocation || t.sm.MediaKind == appstore.MediaKindLiveLocation ||
		t.sm.MediaKind == appstore.MediaKindEvent
}

// Download starts fetching a message's media. it lands on the row: the file,
// or the error.
func (c *Client) Download(ctx context.Context, ref Ref) error {
	t, err := c.media.target(ctx, ref)
	if err != nil {
		return err
	}
	if t.location() && !c.prefs(ctx).GetAutoFetchMaps() {
		return Errorf(ErrRejected, "map fetching is turned off")
	}
	c.spawn(func(ctx context.Context) { _, _ = c.media.fetch(ctx, t) })
	return nil
}

// CancelDownload stops a download or a stream of ref. what landed is kept.
func (c *Client) CancelDownload(ctx context.Context, ref Ref) error {
	m, w, err := c.message(ctx, ref)
	if err != nil {
		return err
	}
	key := w.Now(model.Norm(m.Chat)) + "/" + m.ID
	md := c.media
	md.mu.Lock()
	d := md.downloads[key]
	_, streaming := md.streams[key]
	md.mu.Unlock()
	if d != nil {
		d.cancelled.Store(true)
		d.cancel()
	}
	if streaming {
		md.dropStream(key, false)
		c.live.EndTransfer(m.Chat, m.ID)
	}
	if d == nil && !streaming {
		return Errorf(ErrRejected, "no download is running for this message")
	}
	return nil
}

// fetch downloads t's media, or joins the download already running, and
// says where it landed.
func (md *media) fetch(ctx context.Context, t target) (string, error) {
	md.mu.Lock()
	if d := md.downloads[t.key]; d != nil {
		md.mu.Unlock()
		select {
		case <-d.done:
			if d.err != nil {
				return "", d.err
			}
			return md.landed(ctx, t), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d := &download{done: make(chan struct{}), cancel: cancel}
	md.downloads[t.key] = d
	md.mu.Unlock()
	defer func() {
		md.mu.Lock()
		delete(md.downloads, t.key)
		close(d.done)
		md.mu.Unlock()
	}()
	path, started, err := md.fetchOne(ctx, t)
	d.err = err
	c := md.c
	if started {
		c.live.EndTransfer(t.m.Chat, t.m.ID)
	}
	cancelled := d.cancelled.Load() || errors.Is(err, context.Canceled)
	if started && !cancelled && c.o.Media != nil {
		c.o.Media(err)
	}
	switch {
	case cancelled:
	case err != nil:
		c.log.Info().Err(err).Str("chat", t.m.Chat).Str("id", t.m.ID).Msg("whatsapp: download")
		c.logLocal(context.WithoutCancel(ctx), core.LocalHead{Chat: t.m.Chat, ID: t.m.ID, Op: core.MediaError, Error: err.Error()})
	}
	return path, err
}

// landed is where t's file is now, read back after someone else fetched it.
func (md *media) landed(ctx context.Context, t target) string {
	if n, err := md.target(ctx, Ref{Chat: t.m.Chat, ID: t.m.ID}); err == nil {
		return n.sm.MediaLocalPath
	}
	return ""
}

func (md *media) fetchOne(ctx context.Context, t target) (string, bool, error) {
	c := md.c
	m, sm := t.m, t.sm
	if p := sm.MediaLocalPath; p != "" {
		if _, err := os.Stat(p); err == nil {
			if isAnimatedSticker(sm.MediaMimeType) && filepath.Ext(p) != ".json" {
				json, err := extractLottie(p)
				if err != nil {
					return "", false, err
				}
				c.logLocal(ctx, core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.MediaFile, Path: json})
				return json, false, nil
			}
			return p, false, nil
		}
	}
	if m.Facts.Local.DownloadError() != "" {
		// a retry clears the error before it starts
		c.logLocal(ctx, core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.MediaError})
	}
	if t.location() {
		return md.fetchMap(ctx, t)
	}
	if len(sm.MediaPayload) == 0 {
		return "", false, Errorf(ErrRejected, "the media is not there to download")
	}
	if p, ok := md.stickerCached(sm); ok {
		c.logLocal(ctx, core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.MediaFile, Path: p})
		return p, false, nil
	}
	dm, err := downloadable(sm, m.Facts.Local.DirectPath)
	if err != nil {
		return "", false, err
	}
	total := dm.GetFileLength()
	if total > maxInboundBytes {
		return "", false, Errorf(ErrRejected, "the media is over 2 GiB")
	}
	c.live.SetTransfer(live.Transfer{Chat: m.Chat, ID: m.ID, Total: total})
	cli := c.client()
	if cli == nil || !cli.IsLoggedIn() || !cli.IsConnected() {
		return "", true, errors.New("WhatsApp is not connected")
	}
	dir := filepath.Join(c.o.Paths.MediaCacheDir, "messages", m.Chat)
	name := safeMediaFileName(m.ID, fileExtension(sm))
	if sm.MediaKind == appstore.MediaKindSticker && sm.MediaCacheKey != "" {
		release, err := md.lockKey(ctx, sm.MediaCacheKey)
		if err != nil {
			return "", true, err
		}
		defer release()
		if p, ok := md.stickerCached(sm); ok {
			c.logLocal(ctx, core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.MediaFile, Path: p})
			return p, true, nil
		}
		dir = filepath.Join(c.o.Paths.MediaCacheDir, "stickers")
		name = sm.MediaCacheKey + mediaExtension(sm.MediaMimeType)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", true, err
	}
	path := filepath.Join(dir, name)
	// each fetch has its own part file: a stream may hold the sparse one
	part, err := os.CreateTemp(dir, "."+name+".download-*.part")
	if err != nil {
		return "", true, err
	}
	partPath := part.Name()
	ok := false
	defer func() {
		part.Close()
		if !ok {
			os.Remove(partPath)
		}
	}()
	progress := &progressFile{File: part, report: func(n uint64) {
		c.live.SetTransfer(live.Transfer{Chat: m.Chat, ID: m.ID, Done: n, Total: total})
	}}
	if err := cli.DownloadToFile(ctx, dm, progress); err != nil {
		if !staleMedia(err) {
			return "", true, fmt.Errorf("download: %w", err)
		}
		c.log.Info().Err(err).Str("id", m.ID).Msg("whatsapp: media did not verify, asking the sender for a fresh path")
		direct, rerr := md.retry(ctx, cli, t, dm)
		if rerr != nil {
			return "", true, rerr
		}
		c.logLocal(ctx, core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.MediaDirect, DirectPath: direct})
		if dm, err = downloadable(sm, direct); err != nil {
			return "", true, err
		}
		if _, err = part.Seek(0, io.SeekStart); err == nil {
			if err = part.Truncate(0); err == nil {
				progress.position = 0
				err = cli.DownloadToFile(ctx, dm, progress)
			}
		}
		if staleMedia(err) {
			return "", true, errors.New("this media is no longer available from the sender")
		}
		if err != nil {
			return "", true, fmt.Errorf("download: %w", err)
		}
	}
	info, err := part.Stat()
	if err != nil {
		return "", true, err
	}
	if info.Size() <= 0 || info.Size() > maxInboundBytes {
		return "", true, Errorf(ErrRejected, "the media is empty or over 2 GiB")
	}
	var w, h int32
	if sm.MediaKind == appstore.MediaKindImage {
		if _, err := part.Seek(0, io.SeekStart); err == nil {
			w, h = imageSize(part)
		}
	}
	if err := part.Close(); err != nil {
		return "", true, err
	}
	if err := os.Rename(partPath, path); err != nil {
		return "", true, err
	}
	ok = true
	if _, err := repairWebPAlphaFlagFile(path); err != nil {
		c.log.Debug().Err(err).Str("id", m.ID).Msg("whatsapp: repair a sticker's alpha flag")
	}
	if isAnimatedSticker(sm.MediaMimeType) {
		if path, err = extractLottie(path); err != nil {
			return "", true, err
		}
	}
	c.logLocal(ctx, core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.MediaFile, Path: path, W: w, H: h})
	md.after(m.Chat, m.ID, sm, path)
	return path, true, nil
}

// after queues what a fresh file wants made from it.
func (md *media) after(chat, id string, sm appstore.Message, path string) {
	switch {
	case sm.MediaKind == appstore.MediaKindVideo || sm.MediaKind == appstore.MediaKindGIF:
	case sm.MediaKind == appstore.MediaKindVoice && len(sm.MediaWaveform) == 0:
	default:
		return
	}
	select {
	case md.made <- made{chat: chat, id: id, kind: sm.MediaKind, path: path}:
	default:
	}
}

// stickerCached is a sticker file another message already fetched: every
// copy of a sticker shares one.
func (md *media) stickerCached(sm appstore.Message) (string, bool) {
	if sm.MediaKind != appstore.MediaKindSticker || sm.MediaCacheKey == "" {
		return "", false
	}
	ext := mediaExtension(sm.MediaMimeType)
	if isAnimatedSticker(sm.MediaMimeType) {
		ext = ".json"
	}
	p := filepath.Join(md.c.o.Paths.MediaCacheDir, "stickers", sm.MediaCacheKey+ext)
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}

// lockKey serializes the fetches of one sticker. a waiter that gives up
// leaves the others alone.
func (md *media) lockKey(ctx context.Context, key string) (func(), error) {
	md.mu.Lock()
	l := md.keys[key]
	if l == nil {
		l = &keyLock{token: make(chan struct{}, 1)}
		l.token <- struct{}{}
		md.keys[key] = l
	}
	l.refs++
	md.mu.Unlock()
	unref := func() {
		md.mu.Lock()
		l.refs--
		if l.refs == 0 && md.keys[key] == l {
			delete(md.keys, key)
		}
		md.mu.Unlock()
	}
	select {
	case <-l.token:
		return func() { l.token <- struct{}{}; unref() }, nil
	case <-ctx.Done():
		unref()
		return nil, ctx.Err()
	}
}

// fetchMap draws a location's map from tiles.
func (md *media) fetchMap(ctx context.Context, t target) (string, bool, error) {
	c := md.c
	if !c.prefs(ctx).GetAutoFetchMaps() {
		return "", false, Errorf(ErrRejected, "map fetching is turned off")
	}
	p := appstore.DecodePayload(t.sm.PayloadJSON)
	loc := p.Location
	if loc == nil && p.Event != nil {
		loc = p.Event.Location
	}
	if loc == nil || math.IsNaN(loc.Latitude) || math.IsNaN(loc.Longitude) {
		return "", false, Errorf(ErrRejected, "the location has no coordinates")
	}
	var trail []mapPoint
	if t.sm.MediaKind == appstore.MediaKindLiveLocation {
		pts, err := c.r.LiveTrail(ctx, t.m.Chat, t.m.ID)
		if err != nil {
			c.log.Warn().Err(err).Str("id", t.m.ID).Msg("whatsapp: read a live share's trail")
		}
		for _, pt := range pts {
			trail = append(trail, mapPoint{Lat: pt.Lat, Lng: pt.Lng})
		}
		if n := len(pts); n > 0 {
			loc = &appstore.LocationPayload{Latitude: pts[n-1].Lat, Longitude: pts[n-1].Lng}
		}
	}
	total := uint64(mapTilesX * mapTilesY)
	c.live.SetTransfer(live.Transfer{Chat: t.m.Chat, ID: t.m.ID, Total: total})
	dir := filepath.Join(c.o.Paths.MediaCacheDir, "messages", t.m.Chat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", true, err
	}
	// keyed by message, so a live share redraws over its own file
	path := filepath.Join(dir, safeMediaFileName(t.m.ID, ".map.png"))
	err := md.maps.StitchMap(ctx, loc.Latitude, loc.Longitude, trail, path, func(done, _ int) {
		c.live.SetTransfer(live.Transfer{Chat: t.m.Chat, ID: t.m.ID, Done: uint64(done), Total: total})
	})
	if err != nil {
		return "", true, err
	}
	c.logLocal(ctx, core.LocalHead{Chat: t.m.Chat, ID: t.m.ID, Op: core.MediaMap, Path: path, W: mapOutputWidth, H: mapOutputHeight})
	return path, true, nil
}

// retry asks the sender's phone to upload media the servers lost, and
// says the new direct path.
func (md *media) retry(ctx context.Context, cli *whatsmeow.Client, t target, dm downloadableMedia) (string, error) {
	info, err := retryInfo(t.m)
	if err != nil {
		return "", err
	}
	key := append([]byte(nil), dm.GetMediaKey()...)
	if len(key) == 0 {
		return "", Errorf(ErrRejected, "the media has no key to ask with")
	}
	r := &mediaRetry{done: make(chan struct{}), key: key}
	md.mu.Lock()
	if old := md.retries[t.key]; old != nil {
		r = old
	} else {
		md.retries[t.key] = r
	}
	md.mu.Unlock()
	defer func() {
		md.mu.Lock()
		if md.retries[t.key] == r {
			delete(md.retries, t.key)
		}
		md.mu.Unlock()
	}()
	if err := cli.SendMediaRetryReceipt(ctx, info, key); err != nil {
		return "", fmt.Errorf("ask for the media again: %w", err)
	}
	timer := time.NewTimer(mediaRetryTimeout)
	defer timer.Stop()
	select {
	case <-r.done:
		if r.err != nil {
			return "", r.err
		}
		if r.directPath == "" {
			return "", errors.New("the phone answered with no path")
		}
		return r.directPath, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("the phone did not answer the media retry")
	}
}

// retried is the phone's answer to a media retry.
func (md *media) retried(evt *events.MediaRetry) {
	if evt == nil || evt.MessageID == "" || evt.ChatID.IsEmpty() {
		return
	}
	key := md.c.chatKey(evt.ChatID) + "/" + evt.MessageID
	md.mu.Lock()
	r := md.retries[key]
	if r == nil || r.completed {
		md.mu.Unlock()
		return
	}
	mediaKey := r.key
	md.mu.Unlock()
	direct, err := "", error(nil)
	n, derr := whatsmeow.DecryptMediaRetryNotification(evt, mediaKey)
	switch {
	case errors.Is(derr, whatsmeow.ErrMediaNotAvailableOnPhone):
		err = errors.New("the media is no longer on the sender's phone")
	case derr != nil:
		err = fmt.Errorf("media retry answer: %w", derr)
	case n.GetResult() != waMmsRetry.MediaRetryNotification_SUCCESS:
		err = fmt.Errorf("media retry failed: %s", n.GetResult())
	default:
		direct = n.GetDirectPath()
	}
	md.mu.Lock()
	if !r.completed {
		r.directPath, r.err, r.completed = direct, err, true
		close(r.done)
	}
	md.mu.Unlock()
}

// retryInfo is the message a media retry names.
func retryInfo(m model.Message) (*types.MessageInfo, error) {
	chat, err := types.ParseJID(m.Chat)
	if err != nil {
		return nil, Errorf(ErrInvalid, "chat %q", m.Chat)
	}
	group := chat.Server == types.GroupServer
	var sender types.JID
	if group && !m.FromMe {
		if sender, err = types.ParseJID(m.Sender); err != nil || m.Sender == "" {
			return nil, Errorf(ErrRejected, "the media retry has no sender to name")
		}
	}
	return &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsFromMe: m.FromMe, IsGroup: group},
		ID:            m.ID,
	}, nil
}

// arrived is a live message, looked at once it folds.
func (md *media) arrived(evt *events.Message) {
	_, seq := md.c.core.Progress()
	select {
	case md.arrivals <- arrival{ref: Ref{Chat: evt.Info.Chat.ToNonAD().String(), ID: evt.Info.ID}, seq: seq}:
	default:
	}
}

// run is the account's media work: what arrivals want fetched, and the
// posters and waveforms of what landed.
func (md *media) run(ctx context.Context) {
	go md.makeLoop(ctx)
	for {
		select {
		case <-ctx.Done():
			md.closeStreams()
			return
		case a := <-md.arrivals:
			md.look(ctx, a)
		}
	}
}

// look fetches on its own what the preferences say to: media under the
// size cap of the kinds turned on, maps, the full picture of a link card.
func (md *media) look(ctx context.Context, a arrival) {
	c := md.c
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := c.core.WaitFolded(wctx, a.seq)
	cancel()
	if err != nil {
		return
	}
	t, err := md.target(ctx, a.ref)
	if err != nil {
		md.linkPicture(ctx, a.ref)
		c.lookInvite(ctx, a.ref)
		return
	}
	if t.sm.MediaLocalPath != "" {
		return
	}
	p := c.prefs(ctx)
	var on bool
	switch t.sm.MediaKind {
	case appstore.MediaKindImage:
		on = p.GetAutoDownloadPhotos()
	case appstore.MediaKindVideo, appstore.MediaKindGIF, appstore.MediaKindVideoNote:
		on = p.GetAutoDownloadVideos()
	case appstore.MediaKindVoice, appstore.MediaKindAudio:
		on = p.GetAutoDownloadAudio()
	case appstore.MediaKindDocument:
		on = p.GetAutoDownloadDocuments()
	case appstore.MediaKindSticker:
		on = p.GetAutoDownloadStickers()
	case appstore.MediaKindLocation, appstore.MediaKindLiveLocation, appstore.MediaKindEvent:
		on = p.GetAutoFetchMaps()
	}
	if max := p.GetAutoDownloadMaxBytes(); max > 0 && t.sm.MediaSizeBytes > 0 && uint64(t.sm.MediaSizeBytes) > max {
		on = false
	}
	if on {
		c.spawn(func(ctx context.Context) { _, _ = md.fetch(ctx, t) })
	}
}

// linkPicture fetches the full picture of a link card that draws it large;
// the inline one is plenty for the small layout.
func (md *media) linkPicture(ctx context.Context, ref Ref) {
	c := md.c
	m, w, err := c.message(ctx, ref)
	if err != nil {
		return
	}
	raw, _ := m.Content()
	if raw == nil {
		return
	}
	ext := model.Unwrap(raw).Msg.GetExtendedTextMessage()
	if ext.GetThumbnailDirectPath() == "" || len(ext.GetMediaKey()) == 0 {
		return
	}
	switch ext.GetPreviewType() {
	case waE2E.ExtendedTextMessage_VIDEO, waE2E.ExtendedTextMessage_IMAGE:
	default:
		return
	}
	cli := c.client()
	if cli == nil || !cli.IsLoggedIn() {
		return
	}
	c.spawn(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		data, err := cli.DownloadThumbnail(ctx, ext)
		if err != nil {
			c.log.Debug().Err(err).Str("id", m.ID).Msg("whatsapp: a link card's full picture")
			return
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return
		}
		d := NewDecoder(WorldNames(w), c.o.Paths.MediaCacheDir)
		path := d.saveMessageThumbnailWithExtension(m.Chat, m.ID, data, linkPreviewFullExtension)
		if path == "" {
			return
		}
		c.logLocal(ctx, core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.MediaPreview, Path: path,
			W: int32(cfg.Width), H: int32(cfg.Height)})
	})
}

// makeLoop makes posters and waveforms one at a time: ffmpeg is heavy.
func (md *media) makeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case x := <-md.made:
			md.make(ctx, x)
		}
	}
}

func (md *media) make(ctx context.Context, x made) {
	c := md.c
	switch x.kind {
	case appstore.MediaKindVoice:
		wf, err := waveform(ctx, x.path)
		if err != nil || len(wf) == 0 {
			if err != nil {
				c.log.Debug().Err(err).Str("id", x.id).Msg("whatsapp: waveform")
			}
			return
		}
		c.logLocal(ctx, core.LocalHead{Chat: x.chat, ID: x.id, Op: core.MediaWaveform, Waveform: wf})
	default:
		out := x.path + ".poster2.jpg"
		if info, err := os.Stat(out); err != nil || !info.Mode().IsRegular() {
			if err := extractPoster(ctx, x.path, out); err != nil {
				c.log.Debug().Err(err).Str("id", x.id).Msg("whatsapp: video poster")
				return
			}
		}
		c.logLocal(ctx, core.LocalHead{Chat: x.chat, ID: x.id, Op: core.MediaPoster, Path: out})
	}
}

// logLocal logs something this daemon did for a message.
func (c *Client) logLocal(ctx context.Context, h core.LocalHead) {
	if err := c.append(ctx, core.KindLocal, h, nil); err != nil {
		c.log.Warn().Err(err).Str("op", h.Op).Str("id", h.ID).Msg("whatsapp: log a local fact")
	}
}

// forget drops what runs for the account that is gone.
func (md *media) forget() {
	md.closeStreams()
	md.mu.Lock()
	for _, d := range md.downloads {
		d.cancel()
	}
	md.mu.Unlock()
}

func (md *media) close() {
	md.srv.stop()
	md.forget()
}

type downloadableMedia interface {
	GetDirectPath() string
	GetURL() string
	GetMediaKey() []byte
	GetFileEncSHA256() []byte
	GetFileSHA256() []byte
	GetFileLength() uint64
}

// payloadMedia is the stored media message back as its type. video, gif and
// video notes share one type, so the kind picks it; a wrong pick decodes to
// garbage, not an error.
func payloadMedia(sm appstore.Message) (downloadableMedia, error) {
	var d downloadableMedia
	switch sm.MediaKind {
	case appstore.MediaKindSticker:
		d = &waE2E.StickerMessage{}
	case appstore.MediaKindVideo, appstore.MediaKindGIF, appstore.MediaKindVideoNote:
		d = &waE2E.VideoMessage{}
	case appstore.MediaKindVoice, appstore.MediaKindAudio:
		d = &waE2E.AudioMessage{}
	case appstore.MediaKindDocument:
		d = &waE2E.DocumentMessage{}
	default:
		d = &waE2E.ImageMessage{}
	}
	if err := proto.Unmarshal(sm.MediaPayload, d.(proto.Message)); err != nil {
		return nil, fmt.Errorf("decode the media: %w", err)
	}
	return d, nil
}

// downloadable is the media to fetch, at direct when a retry moved it. a
// sticker on the placeholder host goes by direct path, which whatsmeow
// picks when the url is empty.
func downloadable(sm appstore.Message, direct string) (downloadableMedia, error) {
	d, err := payloadMedia(sm)
	if err != nil {
		return nil, err
	}
	if direct != "" {
		switch m := d.(type) {
		case *waE2E.StickerMessage:
			m.DirectPath, m.URL = proto.String(direct), nil
		case *waE2E.VideoMessage:
			m.DirectPath, m.URL = proto.String(direct), nil
		case *waE2E.AudioMessage:
			m.DirectPath, m.URL = proto.String(direct), nil
		case *waE2E.DocumentMessage:
			m.DirectPath, m.URL = proto.String(direct), nil
		case *waE2E.ImageMessage:
			m.DirectPath, m.URL = proto.String(direct), nil
		}
	}
	if s, ok := d.(*waE2E.StickerMessage); ok && s.GetDirectPath() != "" && placeholderURL(s.GetURL()) {
		return byDirectPath{s}, nil
	}
	return d, nil
}

type byDirectPath struct{ *waE2E.StickerMessage }

func (byDirectPath) GetURL() string { return "" }

func placeholderURL(u string) bool {
	u = strings.TrimSpace(strings.ToLower(u))
	return u == "https://a.whatsapp.net" || u == "http://a.whatsapp.net"
}

// staleMedia is a path that no longer names the blob: gone from the cdn, or
// a body that is not what the message says because it was moved.
func staleMedia(err error) bool {
	return errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) ||
		errors.Is(err, whatsmeow.ErrInvalidMediaEncSHA256)
}

// progressFile counts what DownloadToFile writes in order. the decrypt pass
// after uses WriteAt and is not counted; a seek resets the count.
type progressFile struct {
	*os.File
	position int64
	last     time.Time
	seen     uint64
	report   func(uint64)
}

const progressEvery = 150 * time.Millisecond

func (p *progressFile) Write(b []byte) (int, error) {
	n, err := p.File.Write(b)
	p.position += int64(n)
	if got := uint64(p.position); got != p.seen && time.Since(p.last) >= progressEvery {
		p.last, p.seen = time.Now(), got
		p.report(got)
	}
	return n, err
}

// ReadFrom goes through Write: the one *os.File has would copy in the
// kernel and nothing would be counted.
func (p *progressFile) ReadFrom(r io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			w, werr := p.Write(buf[:n])
			total += int64(w)
			if werr != nil {
				return total, werr
			}
			if w < n {
				return total, io.ErrShortWrite
			}
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

func (p *progressFile) Seek(off int64, whence int) (int64, error) {
	pos, err := p.File.Seek(off, whence)
	if err == nil {
		p.position = pos
	}
	return pos, err
}

func imageSize(r io.Reader) (int32, int32) {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > math.MaxInt32 || cfg.Height > math.MaxInt32 {
		return 0, 0
	}
	return int32(cfg.Width), int32(cfg.Height)
}

// mediaExtension is the cache file's suffix, honest enough for a player to
// sniff; the mime type stays the truth.
func mediaExtension(mimeType string) string {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mimeType)), ";")
	switch strings.TrimSpace(base) {
	case "application/was":
		return ".zip"
	case "image/gif":
		return ".gif"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/3gpp":
		return ".3gp"
	case "video/quicktime":
		return ".mov"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/aac", "audio/x-m4a":
		return ".m4a"
	case "audio/amr":
		return ".amr"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/flac":
		return ".flac"
	case "application/pdf":
		return ".pdf"
	}
	return ".jpg"
}

// fileExtension keeps a document's own extension, so report.pdf is not
// report.jpg in a file manager.
func fileExtension(sm appstore.Message) string {
	if sm.MediaKind != appstore.MediaKindDocument {
		return mediaExtension(sm.MediaMimeType)
	}
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(sm.MediaFileName))); ext != "" && len(ext) <= 16 {
		return ext
	}
	if ext := mediaExtension(sm.MediaMimeType); ext != ".jpg" {
		return ext
	}
	return ".bin"
}

func isAnimatedSticker(mimeType string) bool {
	return strings.EqualFold(strings.TrimSpace(mimeType), "application/was")
}

// extractLottie pulls animation.json out of an animated sticker's archive.
func extractLottie(archive string) (string, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return "", fmt.Errorf("open the animated sticker: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != "animation/animation.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(rc, lottieJSONMax+1))
		rc.Close()
		if err != nil {
			return "", err
		}
		if len(data) == 0 || len(data) > lottieJSONMax {
			return "", Errorf(ErrRejected, "the animated sticker's json is empty or too big")
		}
		out := strings.TrimSuffix(archive, filepath.Ext(archive)) + ".json"
		if err := writeFileAtomic(out, data, 0o600); err != nil {
			return "", err
		}
		return out, nil
	}
	return "", Errorf(ErrRejected, "the animated sticker has no animation.json")
}

const (
	posterTimeout = 20 * time.Second
	// past the fade from black most clips open on
	posterSeek  = "1"
	posterScale = "scale='min(1280,iw)':'min(1280,ih)':force_original_aspect_ratio=decrease"
)

// extractPoster writes a representative frame of a video as a jpeg, at most
// 1280 a side. ffmpeg's thumbnail filter picks the least typical frame past
// the opening; a short clip falls back to the first frame.
func extractPoster(ctx context.Context, src, out string) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, posterTimeout)
	defer cancel()
	tmp, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+"-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	tries := [][]string{
		{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-ss", posterSeek, "-i", src,
			"-vf", "thumbnail=100," + posterScale, "-frames:v", "1", "-q:v", "2", "-f", "image2", tmpPath},
		{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", src,
			"-vf", posterScale, "-frames:v", "1", "-q:v", "2", "-f", "image2", tmpPath},
	}
	var last error
	for _, args := range tries {
		outb, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// ffmpeg exits 0 with nothing written when the seek is past the end
		if info, serr := os.Stat(tmpPath); err == nil && serr == nil && info.Size() > 0 {
			last = nil
			break
		}
		last = fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(outb)))
	}
	if last != nil {
		return last
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, out)
}

const (
	waveformMaxSource = 20 << 20
	waveformTimeout   = 10 * time.Second
)

// waveform is a voice note's 64 bucket envelope, scaled 0-100 to its peak
// the way whatsapp sends it, for a sender that sent none.
func waveform(ctx context.Context, path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > waveformMaxSource {
		return nil, err
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, waveformTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", path, "-f", "s16le", "-ac", "1", "-ar", "8000", "-")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var env envelope
	_, rerr := env.ReadFrom(out)
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	if rerr != nil {
		return nil, rerr
	}
	return env.buckets(), nil
}

// envelopeChunks is how many chunks an envelope holds before it halves them
const envelopeChunks = 4096

// envelope is the sum of squares of s16le samples in chunks, so a long note
// never holds its pcm. chunks start one sample long and double when there
// are too many: a bucket edge is off by at most 1/64 of a bucket.
type envelope struct {
	sums []float64
	per  int
	n    int
	odd  []byte
}

func (e *envelope) ReadFrom(r io.Reader) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for {
		k, err := r.Read(buf)
		total += int64(k)
		e.Write(buf[:k])
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func (e *envelope) Write(b []byte) (int, error) {
	n := len(b)
	if len(e.odd) == 1 && len(b) > 0 {
		e.add(e.odd[0], b[0])
		e.odd, b = e.odd[:0], b[1:]
	}
	for len(b) >= 2 {
		e.add(b[0], b[1])
		b = b[2:]
	}
	e.odd = append(e.odd, b...)
	return n, nil
}

func (e *envelope) add(lo, hi byte) {
	if e.per == 0 {
		e.per = 1
	}
	if e.n%e.per == 0 {
		if len(e.sums) == envelopeChunks {
			for i := range envelopeChunks / 2 {
				e.sums[i] = e.sums[2*i] + e.sums[2*i+1]
			}
			e.sums = e.sums[:envelopeChunks/2]
			e.per *= 2
		}
		e.sums = append(e.sums, 0)
	}
	v := float64(int16(uint16(lo) | uint16(hi)<<8))
	e.sums[len(e.sums)-1] += v * v
	e.n++
}

// buckets is the envelope in waveformBuckets, 0-100 to the loudest. nil is
// too short or silent.
func (e *envelope) buckets() []byte {
	if e.n < waveformBuckets {
		return nil
	}
	sums := make([]float64, waveformBuckets)
	counts := make([]int, waveformBuckets)
	for j, s := range e.sums {
		first, end := j*e.per, min((j+1)*e.per, e.n)
		// a chunk across a bucket edge is shared out by its samples
		for i := first; i < end; {
			b := i * waveformBuckets / e.n
			next := min(end, ((b+1)*e.n+waveformBuckets-1)/waveformBuckets)
			sums[b] += s * float64(next-i) / float64(end-first)
			counts[b] += next - i
			i = next
		}
	}
	peak := 0.0
	rms := make([]float64, waveformBuckets)
	for i := range rms {
		if counts[i] > 0 {
			rms[i] = math.Sqrt(sums[i] / float64(counts[i]))
			peak = max(peak, rms[i])
		}
	}
	if peak <= 0 {
		return nil
	}
	out := make([]byte, waveformBuckets)
	for i, v := range rms {
		out[i] = byte(min(math.Round(v/peak*100), 100))
	}
	return out
}
