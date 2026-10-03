// Package v1 serves PROTOCOL.md v1 from the new core: the protocol package's
// views read through the interfaces here, which answer from the model, and
// every fold commit wakes the views it touched.
//
// until the new core owns sending, media and avatars, those stay with the
// old wa client: commands go to it, and a row takes the media path and
// avatar the old store keeps for the same message. message content is
// decoded by the old ingest builders, run without storing.
package v1

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/app"
	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/store"
	"whatevrd/internal/wa"
)

type Adapter struct {
	core   *core.DB
	r      *model.Reader
	wa     *wa.Client
	old    *store.DB
	daemon *app.Daemon
	log    zerolog.Logger

	mu    sync.Mutex
	world *model.World
	// worldGen moves whenever the world is dropped or patched: a world read
	// from before that is not kept
	worldGen uint64
	previews *previews

	queue         chan core.Change
	resyncPending atomic.Bool
}

func New(db *core.DB, waClient *wa.Client, old *store.DB, daemon *app.Daemon, log zerolog.Logger) *Adapter {
	return &Adapter{core: db, r: model.NewReader(db.Read()), wa: waClient, old: old, daemon: daemon, log: log,
		previews: newPreviews(), queue: make(chan core.Change, 256)}
}

// Bind hands the adapter the wa client once it exists: the core opens first
// so the client's hooks can log into it.
func (a *Adapter) Bind(waClient *wa.Client) { a.wa = waClient }

// World is identity for this read. it is kept, and patched with the people
// a fold touched.
func (a *Adapter) World(ctx context.Context) (*model.World, error) {
	a.mu.Lock()
	w, gen := a.world, a.worldGen
	a.mu.Unlock()
	if w != nil {
		return w, nil
	}
	w, err := a.r.World(ctx)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.worldGen == gen {
		a.world = w
	}
	a.mu.Unlock()
	return w, nil
}

func (a *Adapter) forgetWorld() {
	a.mu.Lock()
	a.world = nil
	a.worldGen++
	a.mu.Unlock()
}

// patchWorld reads persons again into the kept world. only the Run
// goroutine changes a kept world, so nothing patches it at the same time.
func (a *Adapter) patchWorld(ctx context.Context, persons []string) {
	a.mu.Lock()
	w := a.world
	a.worldGen++
	a.mu.Unlock()
	if w == nil {
		return
	}
	n, err := a.r.Patch(ctx, w, persons)
	if err != nil {
		a.log.Warn().Err(err).Msg("v1: patch world")
		a.forgetWorld()
		return
	}
	a.mu.Lock()
	if a.world == w {
		a.world = n
	}
	a.mu.Unlock()
}

// ChatID is the id v1 knows a chat by: the phone number when there is one,
// as the old core did, so a chat a frontend already holds keeps its id.
func ChatID(w *model.World, key string) string {
	if model.IsGroup(key) {
		return key
	}
	// a number that moved to another lid is that lid's now: the old one keeps
	// its own id, or two chats would answer to one
	if pn := w.PN(key); pn != "" && w.Now(pn) == key {
		return pn
	}
	return key
}

// MessageID is chat:id, the old core's message id.
func MessageID(chatID, id string) string { return chatID + ":" + id }

// SplitMessageID takes a v1 message id apart. the chat ends at the first
// colon after its server part, ids never hold one before it.
func SplitMessageID(v1 string) (chat, id string, ok bool) {
	at := strings.IndexByte(v1, '@')
	if at < 0 {
		return "", "", false
	}
	i := strings.IndexByte(v1[at:], ':')
	if i < 0 {
		return "", "", false
	}
	return v1[:at+i], v1[at+i+1:], true
}

// key is the model key behind a v1 chat id.
func key(w *model.World, chatID string) string { return w.Now(model.Norm(chatID)) }

var errNotFound = sql.ErrNoRows

// OnChange turns what a fold commit touched into the events the views wake
// on. it runs on the writer goroutine, so it only queues.
func (a *Adapter) OnChange(c core.Change) {
	select {
	case a.queue <- c:
	default:
		// a full queue means the views are far behind: one resync covers it
		a.resyncPending.Store(true)
	}
}

// Run publishes queued changes until ctx ends.
func (a *Adapter) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-a.queue:
			if a.resyncPending.Swap(false) {
				a.forgetWorld()
				a.previews.dropAll()
				a.daemon.PublishResync()
			}
			a.publish(ctx, c)
		}
	}
}

func (a *Adapter) publish(ctx context.Context, c core.Change) {
	if c.All["person"] {
		a.forgetWorld()
	} else if ps := c.Keys["person"]; len(ps) > 0 {
		a.patchWorld(ctx, ps)
	}
	// before any view hears of it: a view reads again the moment it does
	if c.All["chat"] || c.All["message"] || c.All["person"] || len(c.Keys["person"]) > 0 {
		a.previews.dropAll()
	} else {
		addrs := slices.Concat(c.Keys["chat"], c.Keys["chatrow"])
		for _, mk := range c.Keys["message"] {
			if addr, _, ok := strings.Cut(mk, ":"); ok {
				addrs = append(addrs, addr)
			}
		}
		a.previews.drop(addrs...)
	}
	if c.All["chat"] || c.All["message"] || c.All["person"] {
		a.daemon.PublishResync()
		return
	}
	w, err := a.World(ctx)
	if err != nil {
		a.log.Warn().Err(err).Msg("v1: world for change")
		a.daemon.PublishResync()
		return
	}
	chats := map[string]bool{}
	for _, addr := range c.Keys["chat"] {
		chats[ChatID(w, w.Now(addr))] = true
	}
	for _, p := range c.Keys["person"] {
		if p == "self" {
			continue
		}
		// a name moved: the dm with them, and any group they talk in, which
		// the resync below would be too blunt for
		chats[ChatID(w, w.Now(p))] = true
	}
	for _, mk := range c.Keys["message"] {
		addr, id, ok := strings.Cut(mk, ":")
		if !ok {
			continue
		}
		chat := ChatID(w, w.Now(addr))
		a.daemon.PublishMessageUpdated(app.Message{ID: MessageID(chat, id), ChatID: chat})
		chats[chat] = true
	}
	for _, g := range c.Keys["pins"] {
		chats[ChatID(w, w.Now(g))] = true
	}
	for id := range chats {
		if id != "" {
			a.daemon.PublishChatUpdated(app.Chat{ID: id})
		}
	}
	if len(c.Keys["sync"]) > 0 || c.All["sync"] {
		a.publishSync(ctx)
	}
	if len(c.Keys["blocklist"]) > 0 || c.All["blocklist"] {
		a.daemon.PublishBlocklistChanged()
	}
	if len(c.Keys["sticker"]) > 0 || c.All["sticker"] {
		a.daemon.PublishStickerLibraryChanged(app.StickerSourceFavorite)
	}
}

// publishSync tells the sync view where history stands: each sync type's
// own newest progress, never summed.
func (a *Adapter) publishSync(ctx context.Context) {
	p, err := a.r.HistoryProgress(ctx)
	if err != nil || p.SyncType == "" {
		return
	}
	a.daemon.PublishHistorySyncProgress(app.HistorySyncEvent{
		SyncType:        historyType(p.SyncType),
		ProgressPercent: p.Progress,
		ChunkOrder:      p.Chunk,
		IsComplete:      p.Complete,
		Phase:           map[bool]app.HistorySyncPhase{true: app.HistorySyncPhaseComplete, false: app.HistorySyncPhaseProcessing}[p.Complete],
	})
}

func historyType(t string) app.HistorySyncType {
	switch t {
	case "INITIAL_BOOTSTRAP":
		return app.HistorySyncTypeInitialBootstrap
	case "RECENT":
		return app.HistorySyncTypeRecent
	case "FULL":
		return app.HistorySyncTypeFull
	case "PUSH_NAME":
		return app.HistorySyncTypePushName
	case "ON_DEMAND":
		return app.HistorySyncTypeOnDemand
	case "INITIAL_STATUS_V3":
		return app.HistorySyncTypeInitialStatusV3
	case "NON_BLOCKING_DATA":
		return app.HistorySyncTypeNonBlockingData
	}
	return app.HistorySyncTypeUnspecified
}

func jid(s string) types.JID {
	j, _ := types.ParseJID(s)
	return j
}

func ms(t int64) time.Time { return time.UnixMilli(t) }

func isNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
