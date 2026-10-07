package whatsapp

import (
	"context"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// Frontend is a connected frontend as the account's own behaviour needs it.
type Frontend struct {
	Focused bool
	// Active is the key of the chat it has open
	Active string
	// Notifies says it shows notifications itself
	Notifies bool
	// Watching is every person whose presence it shows
	Watching []string
}

type frontends struct {
	mu   sync.Mutex
	list []Frontend
	// sent is the presence the server last heard from us
	sent    types.Presence
	offline *time.Timer
	gen     uint64
	// watched is when each person's presence was last asked for
	watched map[string]time.Time
	swept   time.Time
}

// expire drops the times in m older than ttl: one that old counts as never
func expire(m map[string]time.Time, now time.Time, ttl time.Duration) {
	for k, t := range m {
		if now.Sub(t) > ttl {
			delete(m, k)
		}
	}
}

// presenceOffline is how long the account stays available after the last
// focused frontend goes, so a short look away never shows as last seen
var presenceOffline = 30 * time.Second

// presenceAgain is how often a watched person's presence is asked for
// again: the server stops telling after a while
const presenceAgain = 5 * time.Minute

// SetFrontends is every connected frontend, each time one comes, goes or
// changes.
func (c *Client) SetFrontends(fs []Frontend) {
	f := &c.fronts
	f.mu.Lock()
	before := focused(f.list)
	f.list = fs
	now := focused(fs)
	var watch []string
	if f.watched == nil {
		f.watched = map[string]time.Time{}
	}
	if t := time.Now(); t.Sub(f.swept) > presenceAgain {
		expire(f.watched, t, presenceAgain)
		f.swept = t
	}
	for _, fe := range fs {
		for _, p := range fe.Watching {
			if t, ok := f.watched[p]; !ok || time.Since(t) > presenceAgain {
				f.watched[p] = time.Now()
				watch = append(watch, p)
			}
		}
	}
	f.mu.Unlock()
	if before != now || len(fs) == 0 {
		c.syncPresence(len(fs) == 0)
	}
	if len(watch) > 0 {
		c.spawn(func(ctx context.Context) { c.subscribePresence(ctx, watch) })
	}
}

func focused(fs []Frontend) bool {
	for _, f := range fs {
		if f.Focused {
			return true
		}
	}
	return false
}

// Looking says a frontend is focused on chat right now.
func (c *Client) Looking(chat string) bool {
	c.fronts.mu.Lock()
	defer c.fronts.mu.Unlock()
	for _, f := range c.fronts.list {
		if f.Focused && f.Active == chat {
			return true
		}
	}
	return false
}

// FrontendNotifies says a connected frontend shows notifications itself.
func (c *Client) FrontendNotifies() bool {
	c.fronts.mu.Lock()
	defer c.fronts.mu.Unlock()
	for _, f := range c.fronts.list {
		if f.Notifies {
			return true
		}
	}
	return false
}

// syncPresence tells the server whether someone is looking. going away
// waits presenceOffline unless now says it may not.
func (c *Client) syncPresence(now bool) {
	cli := c.client()
	if cli == nil || !cli.IsLoggedIn() || cli.Store.PushName == "" {
		return
	}
	f := &c.fronts
	f.mu.Lock()
	want := types.PresenceUnavailable
	if focused(f.list) {
		want = types.PresenceAvailable
	}
	if want == types.PresenceUnavailable && f.sent != types.PresenceUnavailable && !now {
		if f.offline == nil {
			f.gen++
			gen := f.gen
			f.offline = time.AfterFunc(presenceOffline, func() { c.presenceTimer(gen) })
		}
		f.mu.Unlock()
		return
	}
	f.stopTimer()
	if f.sent == want && !now {
		f.mu.Unlock()
		return
	}
	f.sent = want
	f.mu.Unlock()
	if err := cli.SendPresence(c.accountCtx(), want); err != nil {
		c.log.Warn().Err(err).Str("presence", string(want)).Msg("whatsapp: presence")
	}
}

func (f *frontends) stopTimer() {
	f.gen++
	if f.offline != nil {
		f.offline.Stop()
		f.offline = nil
	}
}

func (c *Client) presenceTimer(gen uint64) {
	f := &c.fronts
	f.mu.Lock()
	if gen != f.gen {
		f.mu.Unlock()
		return
	}
	f.offline = nil
	f.mu.Unlock()
	c.syncPresence(true)
}

// subscribePresence asks the server to tell us when each person comes and
// goes. it only answers while we are available ourselves.
func (c *Client) subscribePresence(ctx context.Context, keys []string) {
	cli, err := c.loggedIn()
	if err != nil || !cli.IsConnected() {
		c.fronts.mu.Lock()
		for _, k := range keys {
			delete(c.fronts.watched, k)
		}
		c.fronts.mu.Unlock()
		return
	}
	for _, k := range keys {
		j, err := types.ParseJID(k)
		if err != nil || j.Server == types.GroupServer {
			continue
		}
		if err := cli.SubscribePresence(ctx, j); err != nil {
			c.log.Debug().Err(err).Str("who", k).Msg("whatsapp: presence subscribe")
		}
	}
}

// SetTyping says we are composing in chat, or stopped.
func (c *Client) SetTyping(ctx context.Context, chat string, on, recording bool) error {
	cli, err := c.loggedIn()
	if err != nil {
		return err
	}
	if !cli.IsConnected() {
		return nil
	}
	j, err := c.sendJID(chat)
	if err != nil {
		return err
	}
	state, media := types.ChatPresencePaused, types.ChatPresenceMediaText
	if on {
		state = types.ChatPresenceComposing
	}
	if recording {
		media = types.ChatPresenceMediaAudio
	}
	if err := c.guard(ctx, j); err != nil {
		return err
	}
	return cli.SendChatPresence(ctx, j, state, media)
}
