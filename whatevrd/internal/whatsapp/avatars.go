package whatsapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

const (
	maxAvatarBytes = 5 << 20
	// a picture's change comes as an event, so a check that it is still the
	// same can be lazy
	avatarOKFor     = 7 * 24 * time.Hour
	avatarNoneFor   = 24 * time.Hour
	avatarHiddenFor = 7 * 24 * time.Hour
	avatarFailBase  = time.Minute
	avatarFailMax   = time.Hour
	// two at a time, so live traffic keeps the socket
	avatarWorkers = 2
	// background fetches go through a bucket, so a fresh pairing with a big
	// chat list does not hammer the server; what is on screen is not held
	avatarRate  = 2.0
	avatarBurst = 5.0
	avatarQueue = 2048
	// wantAgain is how long a key someone looked at is not looked at again
	wantAgain = 10 * time.Minute
	// refresh is the background pass over the newest chats
	avatarRefresh      = 6 * time.Hour
	avatarRefreshChats = 200
)

// avatars fetches profile pictures, on demand and in the background.
type avatars struct {
	c *Client

	mu      sync.Mutex
	high    []avatarJob
	low     []avatarJob
	queued  map[string]bool
	checked map[string]time.Time
	swept   time.Time
	tokens  float64
	filled  time.Time
	wake    chan struct{}
	kicks   chan struct{}
}

type avatarJob struct {
	jid   string
	force bool
}

func newAvatars(c *Client) *avatars {
	return &avatars{c: c, queued: map[string]bool{}, checked: map[string]time.Time{}, tokens: avatarBurst,
		filled: time.Now(), wake: make(chan struct{}, 1), kicks: make(chan struct{}, 1)}
}

// WantAvatars is people and chats on screen: any picture of theirs that is
// due is fetched first.
func (c *Client) WantAvatars(keys []string) {
	a := c.avatars
	now := time.Now()
	var fresh []string
	a.mu.Lock()
	if now.Sub(a.swept) > wantAgain {
		expire(a.checked, now, wantAgain)
		a.swept = now
	}
	for _, k := range keys {
		if t, ok := a.checked[k]; ok && now.Sub(t) < wantAgain {
			continue
		}
		a.checked[k] = now
		fresh = append(fresh, k)
	}
	a.mu.Unlock()
	if len(fresh) > 0 {
		c.spawn(func(ctx context.Context) { a.queueDue(ctx, fresh, true) })
	}
}

// want is a chat we just sent to, looked at soon.
func (a *avatars) want(chat string) { a.c.WantAvatars([]string{chat}) }

// kick starts a background pass, on connect.
func (a *avatars) kick() {
	select {
	case a.kicks <- struct{}{}:
	default:
	}
}

// changed is a picture event: fetch it again now, or log it gone.
func (a *avatars) changed(evt *events.Picture) {
	c := a.c
	j := fetchJID(c, evt.JID)
	if j == "" {
		return
	}
	if evt.Remove {
		c.spawn(func(ctx context.Context) {
			c.logAvatar(ctx, core.AvatarHead{JID: j, Status: core.AvatarNone})
		})
		return
	}
	a.enqueue(avatarJob{jid: j, force: true}, true)
}

// fetchJID is the address a picture is asked for under: a person by number
// when we know it.
func fetchJID(c *Client, j types.JID) string {
	j = j.ToNonAD()
	if j.IsEmpty() || j.User == "0" || j.Server == types.BroadcastServer || j.Server == types.NewsletterServer {
		return ""
	}
	if j.Server == types.HiddenUserServer {
		if w, err := c.world(); err == nil {
			if pn := w.PN(w.Now(j.String())); pn != "" {
				return pn
			}
		}
	}
	return j.String()
}

// queueDue queues every key whose picture is due.
func (a *avatars) queueDue(ctx context.Context, keys []string, visible bool) {
	c := a.c
	w, err := c.world()
	if err != nil {
		return
	}
	var addrs []string
	jids := map[string]string{}
	for _, k := range keys {
		j, err := types.ParseJID(k)
		if err != nil {
			continue
		}
		f := fetchJID(c, j)
		if f == "" {
			continue
		}
		jids[k] = f
		addrs = append(addrs, w.Addrs(w.Now(model.Norm(k)))...)
	}
	av, err := c.r.Avatars(ctx, addrs)
	if err != nil {
		c.log.Warn().Err(err).Msg("whatsapp: read avatars")
		return
	}
	now := time.Now()
	for k, f := range jids {
		var best model.Avatar
		for _, addr := range w.Addrs(w.Now(model.Norm(k))) {
			if x, ok := av[addr]; ok && (x.LastTry > best.LastTry) {
				best = x
			}
		}
		if due(best, now) {
			a.enqueue(avatarJob{jid: f}, visible)
		}
	}
}

// due says a picture's newest answer is stale, or its last failure has
// waited out its backoff.
func due(x model.Avatar, now time.Time) bool {
	if x.Fails > 0 {
		back := avatarFailBase << min(x.Fails-1, 6)
		return now.Sub(time.UnixMilli(x.LastTry)) >= min(back, avatarFailMax)
	}
	age := now.Sub(time.UnixMilli(x.T))
	switch x.Status {
	case "":
		return true
	case core.AvatarOK:
		if _, err := os.Stat(x.Path); err != nil {
			return true
		}
		return age >= avatarOKFor
	case core.AvatarNone:
		return age >= avatarNoneFor
	case core.AvatarHidden:
		return age >= avatarHiddenFor
	}
	return true
}

func (a *avatars) enqueue(j avatarJob, visible bool) {
	a.mu.Lock()
	if a.queued[j.jid] {
		if visible || j.force {
			// promote it, and keep the force
			for i, x := range a.low {
				if x.jid == j.jid {
					a.low = append(a.low[:i], a.low[i+1:]...)
					x.force = x.force || j.force
					a.high = append(a.high, x)
					break
				}
			}
		}
		a.mu.Unlock()
		return
	}
	q := &a.low
	if visible || j.force {
		q = &a.high
	}
	if len(*q) >= avatarQueue {
		a.mu.Unlock()
		return
	}
	*q = append(*q, j)
	a.queued[j.jid] = true
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// pop is the next job and how long a background one must wait first.
func (a *avatars) pop() (avatarJob, time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.high) > 0 {
		j := a.high[0]
		a.high = a.high[1:]
		delete(a.queued, j.jid)
		return j, 0, true
	}
	if len(a.low) == 0 {
		return avatarJob{}, 0, false
	}
	now := time.Now()
	a.tokens = min(avatarBurst, a.tokens+now.Sub(a.filled).Seconds()*avatarRate)
	a.filled = now
	if a.tokens < 1 {
		return avatarJob{}, time.Duration((1 - a.tokens) / avatarRate * float64(time.Second)), false
	}
	a.tokens--
	j := a.low[0]
	a.low = a.low[1:]
	delete(a.queued, j.jid)
	return j, 0, true
}

func (a *avatars) run(ctx context.Context) {
	for range avatarWorkers {
		go a.work(ctx)
	}
	t := time.NewTicker(avatarRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.kicks:
		}
		a.refresh(ctx)
	}
}

func (a *avatars) work(ctx context.Context) {
	for {
		j, wait, ok := a.pop()
		if ok {
			a.fetch(ctx, j)
			continue
		}
		var timer <-chan time.Time
		if wait > 0 {
			timer = time.After(wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-timer:
		}
	}
}

// refresh queues the due pictures of the newest chats, in the background.
func (a *avatars) refresh(ctx context.Context) {
	c := a.c
	cli := c.client()
	if cli == nil || !cli.IsLoggedIn() {
		return
	}
	chats, err := c.r.Chats(ctx, model.ChatFilter{Limit: avatarRefreshChats})
	if err != nil {
		c.log.Warn().Err(err).Msg("whatsapp: chats for the avatar refresh")
		return
	}
	keys := make([]string, 0, len(chats))
	for _, ch := range chats {
		keys = append(keys, ch.Key)
	}
	a.queueDue(ctx, keys, false)
	a.sweep(ctx)
}

func (a *avatars) fetch(ctx context.Context, j avatarJob) {
	c := a.c
	cli := c.client()
	if cli == nil || !cli.IsLoggedIn() || ctx.Err() != nil {
		return
	}
	jid, err := types.ParseJID(j.jid)
	if err != nil {
		return
	}
	prev := ""
	if !j.force {
		if av, err := c.r.Avatars(ctx, []string{j.jid}); err == nil {
			if x := av[j.jid]; x.Status == core.AvatarOK {
				if _, err := os.Stat(x.Path); err == nil {
					prev = x.PictureID
				}
			}
		}
	}
	id, path, err := a.download(ctx, cli, jid, prev)
	h := core.AvatarHead{JID: j.jid}
	switch {
	case ctx.Err() != nil:
		return
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet):
		h.Status = core.AvatarNone
	case errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
		h.Status = core.AvatarHidden
	case err != nil:
		h.Status, h.Error = core.AvatarError, err.Error()
	case id == "":
		// unchanged since prev
		av, rerr := c.r.Avatars(ctx, []string{j.jid})
		if rerr != nil {
			return
		}
		x := av[j.jid]
		h.Status, h.PictureID, h.Path = core.AvatarOK, x.PictureID, x.Path
	default:
		h.Status, h.PictureID, h.Path = core.AvatarOK, id, path
	}
	c.logAvatar(ctx, h)
}

func (c *Client) logAvatar(ctx context.Context, h core.AvatarHead) {
	if err := c.append(ctx, core.KindAvatar, h, nil); err != nil {
		c.log.Warn().Err(err).Str("jid", h.JID).Msg("whatsapp: log an avatar")
	}
}

var errNotReady = errors.New("WhatsApp is not connected")

// download fetches jid's picture into the cache, under a name made from its
// picture id so a changed picture never reuses a path. "" is unchanged
// since prev.
func (a *avatars) download(ctx context.Context, cli *whatsmeow.Client, jid types.JID, prev string) (string, string, error) {
	if !cli.IsConnected() {
		return "", "", errNotReady
	}
	info, err := cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{ExistingID: prev})
	if err != nil {
		return "", "", err
	}
	if info == nil {
		if prev == "" {
			return "", "", whatsmeow.ErrProfilePictureNotSet
		}
		return "", "", nil
	}
	path, err := a.c.saveAvatar(ctx, jid, info, "avatars")
	return info.ID, path, err
}

// saveAvatar puts a picture whatsapp pointed at into dir in the cache.
func (c *Client) saveAvatar(ctx context.Context, jid types.JID, info *types.ProfilePictureInfo, dir string) (string, error) {
	dir = filepath.Join(c.o.Paths.MediaCacheDir, dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second, Transport: c.o.Transport}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("the picture's server answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > maxAvatarBytes {
		return "", errors.New("the picture is empty or too big")
	}
	ext := ""
	switch http.DetectContentType(data) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		return "", errors.New("the picture is not an image")
	}
	sum := sha256.Sum256([]byte(info.ID))
	safe := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(jid.String())
	path := filepath.Join(dir, safe+"-"+hex.EncodeToString(sum[:8])+ext)
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// sweep drops picture files no avatar names now. an hour old at least: a
// fetch's file lands before its log entry.
func (a *avatars) sweep(ctx context.Context) {
	c := a.c
	dir := filepath.Join(c.o.Paths.MediaCacheDir, "avatars")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keep, err := c.r.AvatarPaths(ctx)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil || keep[p] || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		_ = os.Remove(p)
	}
}

// FetchProfilePicture is a person's picture at full size, for a viewer.
func (c *Client) FetchProfilePicture(ctx context.Context, key string) (string, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return "", err
	}
	j, err := types.ParseJID(key)
	if err != nil {
		return "", Errorf(ErrInvalid, "address %q", key)
	}
	f := fetchJID(c, j)
	if f == "" {
		return "", Errorf(ErrRejected, "no picture for this address")
	}
	jid, _ := types.ParseJID(f)
	info, err := cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{})
	switch {
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), err == nil && info == nil:
		return "", Errorf(ErrNotFound, "no picture set")
	case errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
		return "", Errorf(ErrRejected, "the picture is hidden from us")
	case err != nil:
		return "", err
	}
	path, err := c.saveAvatar(ctx, jid, info, "avatars-full")
	if err != nil {
		return "", Errorf(ErrIO, "%v", err)
	}
	return path, nil
}

// forget drops what is queued and checked, on logout.
func (a *avatars) forget() {
	a.mu.Lock()
	a.high, a.low = nil, nil
	a.queued, a.checked = map[string]bool{}, map[string]time.Time{}
	a.mu.Unlock()
}
