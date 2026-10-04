package whatsapp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/mediastream"
	appstore "whatevrd/internal/store"
)

// streamHost is the cdn host for media a retry left with only a direct path
const streamHost = "mmg.whatsapp.net"

// Stream is a player's way into a file still downloading.
type Stream struct {
	ID       string
	URL      string
	Mime     string
	Size     uint64
	Duration time.Duration
}

// StreamUpdate says a stream failed late: Path is the whole file to play
// instead, empty when that failed too.
type StreamUpdate struct {
	StreamID string
	Ref      Ref
	Path     string
	Err      error
}

type streamEntry struct {
	chat, id string
	stream   *mediastream.Stream
	// active counts handlers that began on the sparse file; it closes once
	// they are done after the file is whole
	active    int
	part      string
	final     string
	completed string
	size      int64
	mime      string
	asked     map[string]func(StreamUpdate)
}

// StreamMedia starts or joins a ranged fetch of ref's media and returns a
// loopback url a player can open now. the fetch runs to the end, so the row
// gets its path like any download; update hears a late failure.
func (c *Client) StreamMedia(ctx context.Context, ref Ref, update func(StreamUpdate)) (Stream, error) {
	md := c.media
	t, err := md.target(ctx, ref)
	if err != nil {
		return Stream{}, err
	}
	if p := t.sm.MediaLocalPath; p != "" {
		if _, err := os.Stat(p); err == nil {
			return Stream{}, Errorf(ErrRejected, "the media is already downloaded")
		}
	}
	if t.location() || len(t.sm.MediaPayload) == 0 {
		return Stream{}, Errorf(ErrRejected, "the media can't be streamed")
	}
	dm, err := downloadable(t.sm, t.m.Facts.Local.DirectPath)
	if err != nil {
		return Stream{}, err
	}
	u, err := streamURL(dm, t.sm.MediaKind)
	if err != nil {
		return Stream{}, err
	}
	addr, token, err := md.srv.start(md)
	if err != nil {
		return Stream{}, err
	}
	sid, err := randomToken(18)
	if err != nil {
		return Stream{}, err
	}
	md.mu.Lock()
	_, joining := md.streams[t.key]
	md.mu.Unlock()
	release := func() {}
	if !joining {
		if release, err = md.admit.take(ctx, true); err != nil {
			return Stream{}, err
		}
	}
	e, err := md.ensureStream(t, dm, u, sid, update, release)
	if err != nil {
		return Stream{}, err
	}
	return Stream{
		ID:       sid,
		URL:      fmt.Sprintf("http://%s/media/%s?t=%s", addr, url.PathEscape(t.key), token),
		Mime:     e.mime,
		Size:     uint64(e.size),
		Duration: time.Duration(t.sm.MediaDurationSecs) * time.Second,
	}, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// streamURL is where the encrypted bytes are: the message's own url, or one
// built from the direct path the way whatsmeow builds it.
func streamURL(dm downloadableMedia, kind string) (string, error) {
	if u := strings.TrimSpace(dm.GetURL()); u != "" && !placeholderURL(u) {
		return u, nil
	}
	dp := strings.TrimSpace(dm.GetDirectPath())
	if !strings.HasPrefix(dp, "/") {
		return "", Errorf(ErrRejected, "the media has nowhere to stream from")
	}
	return fmt.Sprintf("https://%s%s&hash=%s&mms-type=%s&__wa-mms=", streamHost, dp,
		base64.URLEncoding.EncodeToString(dm.GetFileEncSHA256()), mmsType(kind)), nil
}

func mmsType(kind string) string {
	switch streamAppInfo(kind) {
	case whatsmeow.MediaVideo:
		return "video"
	case whatsmeow.MediaAudio:
		return "audio"
	case whatsmeow.MediaDocument:
		return "document"
	}
	return "image"
}

// streamAppInfo is the hkdf info that ties the key to its media type.
func streamAppInfo(kind string) whatsmeow.MediaType {
	switch kind {
	case appstore.MediaKindVideo, appstore.MediaKindGIF, appstore.MediaKindVideoNote:
		return whatsmeow.MediaVideo
	case appstore.MediaKindVoice, appstore.MediaKindAudio:
		return whatsmeow.MediaAudio
	case appstore.MediaKindDocument:
		return whatsmeow.MediaDocument
	}
	return whatsmeow.MediaImage
}

// ensureStream is the running stream of a message, started if there is
// none: two players share one fetch. release is the admission slot a new
// fetch holds until it stops.
func (md *media) ensureStream(t target, dm downloadableMedia, u, sid string, update func(StreamUpdate), release func()) (*streamEntry, error) {
	c := md.c
	md.mu.Lock()
	defer md.mu.Unlock()
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	if e, ok := md.streams[t.key]; ok {
		if e.completed == "" {
			e.asked[sid] = update
		}
		return e, nil
	}
	keys, err := mediastream.DeriveKeys(dm.GetMediaKey(), string(streamAppInfo(t.sm.MediaKind)))
	if err != nil {
		return nil, Errorf(ErrRejected, "the media can't be streamed: %v", err)
	}
	dir := filepath.Join(c.o.Paths.MediaCacheDir, "messages", t.m.Chat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	final := filepath.Join(dir, safeMediaFileName(t.m.ID, fileExtension(t.sm)))
	var sidecar []byte
	switch m := dm.(type) {
	case *waE2E.VideoMessage:
		sidecar = m.GetStreamingSidecar()
	case *waE2E.AudioMessage:
		sidecar = m.GetStreamingSidecar()
	}
	src := mediastream.Source{URL: u, Keys: keys, Sidecar: sidecar, PlaintextLen: int64(dm.GetFileLength()),
		FileSHA256: dm.GetFileSHA256(), Mime: t.sm.MediaMimeType}
	if err := src.Valid(); err != nil {
		return nil, Errorf(ErrRejected, "the media can't be streamed: %v", err)
	}
	if src.PlaintextLen > maxInboundBytes {
		return nil, Errorf(ErrRejected, "the media is over 2 GiB")
	}
	chat, id, key := t.m.Chat, t.m.ID, t.key
	s, err := mediastream.New(src, final+".part", md.http,
		func(got, total int64) {
			c.live.SetTransfer(live.Transfer{Chat: chat, ID: id, Done: uint64(got), Total: uint64(total)})
		},
		func(err error) {
			release()
			go md.finishStream(key, t, err)
		})
	if err != nil {
		return nil, err
	}
	e := &streamEntry{chat: chat, id: id, stream: s, part: final + ".part", final: final, size: s.Size(),
		mime: t.sm.MediaMimeType, asked: map[string]func(StreamUpdate){sid: update}}
	md.streams[key] = e
	started = true
	c.live.SetTransfer(live.Transfer{Chat: chat, ID: id, Done: uint64(s.ReadyBytes()), Total: uint64(s.Size())})
	return e, nil
}

// finishStream makes a whole stream an ordinary file; players keep their
// url. a failed one keeps what it fetched and falls back to a download.
func (md *media) finishStream(key string, t target, err error) {
	c := md.c
	md.mu.Lock()
	e, ok := md.streams[key]
	md.mu.Unlock()
	if !ok {
		return
	}
	ctx := c.accountCtx()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		c.log.Warn().Err(err).Str("id", e.id).Msg("whatsapp: stream failed, downloading instead")
		md.recover(ctx, key, t, e, errors.Is(err, mediastream.ErrRangeUnsupported))
		return
	}
	if err := os.Rename(e.part, e.final); err != nil {
		md.recover(ctx, key, t, e, true)
		return
	}
	os.Remove(e.part + ".idx")
	md.mu.Lock()
	e.completed = e.final
	var done *mediastream.Stream
	if e.active == 0 {
		done, e.stream = e.stream, nil
	}
	// players switch to the path when the row has it; only a failure is told
	e.asked = nil
	md.mu.Unlock()
	if done != nil {
		done.Close()
	}
	c.live.EndTransfer(e.chat, e.id)
	c.logLocal(ctx, core.LocalHead{Chat: e.chat, ID: e.id, Op: core.MediaFile, Path: e.final})
	md.after(e.chat, e.id, t.sm, e.final)
}

func (md *media) recover(ctx context.Context, key string, t target, e *streamEntry, discard bool) {
	md.dropStream(key, discard)
	md.mu.Lock()
	asked := e.asked
	e.asked = nil
	md.mu.Unlock()
	path, err := md.fetch(ctx, t, true)
	for sid, tell := range asked {
		if tell != nil {
			tell(StreamUpdate{StreamID: sid, Ref: Ref{Chat: t.m.Chat, ID: t.m.ID}, Path: path, Err: err})
		}
	}
}

// dropStream stops and forgets a stream; discard also drops its bytes, for
// a part file that can't be trusted.
func (md *media) dropStream(key string, discard bool) {
	md.mu.Lock()
	e, ok := md.streams[key]
	delete(md.streams, key)
	md.mu.Unlock()
	if !ok || e.stream == nil {
		return
	}
	if discard {
		e.stream.Discard()
		return
	}
	e.stream.Close()
}

func (md *media) closeStreams() {
	md.mu.Lock()
	keys := make([]string, 0, len(md.streams))
	for k := range md.streams {
		keys = append(keys, k)
	}
	md.mu.Unlock()
	for _, k := range keys {
		md.dropStream(k, false)
	}
}

// streamServer is the loopback range server, bound on first use with a
// token fresh per process, so an old url means nothing to a new daemon.
type streamServer struct {
	mu    sync.Mutex
	addr  string
	token string
	srv   *http.Server
}

func (s *streamServer) start(md *media) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		return s.addr, s.token, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", fmt.Errorf("bind the stream server: %w", err)
	}
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		ln.Close()
		return "", "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/media/", md.serve)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.addr, s.token = ln.Addr().String(), hex.EncodeToString(tok)
	go func(srv *http.Server) {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			md.c.log.Warn().Err(err).Msg("whatsapp: stream server stopped")
		}
	}(s.srv)
	return s.addr, s.token, nil
}

func (s *streamServer) stop() {
	s.mu.Lock()
	srv := s.srv
	s.srv, s.addr, s.token = nil, "", ""
	s.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func (md *media) serve(w http.ResponseWriter, r *http.Request) {
	md.srv.mu.Lock()
	token := md.srv.token
	md.srv.mu.Unlock()
	if token == "" || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key, err := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/media/"))
	if err != nil || key == "" {
		http.NotFound(w, r)
		return
	}
	md.mu.Lock()
	e, ok := md.streams[key]
	done := ""
	if ok {
		done = e.completed
		if done == "" {
			e.active++
		}
	}
	md.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if e.mime != "" {
		w.Header().Set("Content-Type", e.mime)
	}
	if done != "" {
		f, err := os.Open(done)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.Error(w, "stat", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, filepath.Base(done), info.ModTime(), f)
		return
	}
	defer md.release(key, e)
	// ServeContent does the ranges; the reader makes them wait for bytes
	http.ServeContent(w, r, "", time.Time{}, &streamReader{s: e.stream, ctx: r.Context()})
}

func (md *media) release(key string, e *streamEntry) {
	md.mu.Lock()
	cur, ok := md.streams[key]
	if !ok || cur != e {
		md.mu.Unlock()
		return
	}
	e.active = max(e.active-1, 0)
	var done *mediastream.Stream
	if e.active == 0 && e.completed != "" {
		done, e.stream = e.stream, nil
	}
	md.mu.Unlock()
	if done != nil {
		done.Close()
	}
}

// streamReader reads a stream still arriving: a read past what landed moves
// the fetch there and waits, which is what a player expects of a slow file.
type streamReader struct {
	s      *mediastream.Stream
	ctx    context.Context
	offset int64
}

func (r *streamReader) Read(p []byte) (int, error) {
	if r.offset >= r.s.Size() {
		return 0, io.EOF
	}
	if err := r.s.WaitFor(r.ctx, r.offset); err != nil {
		return 0, err
	}
	n, err := r.s.ReadAt(p, r.offset)
	r.offset += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

func (r *streamReader) Seek(off int64, whence int) (int64, error) {
	var to int64
	switch whence {
	case io.SeekStart:
		to = off
	case io.SeekCurrent:
		to = r.offset + off
	case io.SeekEnd:
		to = r.s.Size() + off
	default:
		return 0, fmt.Errorf("stream: bad whence %d", whence)
	}
	if to < 0 {
		return 0, errors.New("stream: negative seek")
	}
	r.offset = to
	if to < r.s.Size() {
		r.s.SeekTo(to)
	}
	return to, nil
}
