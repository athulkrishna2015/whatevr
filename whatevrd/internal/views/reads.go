// Package views serves protocol 2's views and queries from the model and the
// live hub. rows are built here: a model message decoded, its facts laid on,
// every person in it given an id and an avatar.
package views

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/reflect/protoreflect"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
	"whatevrd/internal/status"
)

// touch kinds the reads make up themselves
const (
	// TouchClock is a minute passing: mutes and pins run out on their own
	TouchClock = "clock"
	// TouchSends is the outbox count moving
	TouchSends = "sends"
)

type Options struct {
	Core  *core.DB
	IDs   *model.IDs
	Live  *live.Hub
	Board *status.Board
	// MediaDir is where decoded thumbnails go
	MediaDir string
	Log      zerolog.Logger
	// Login is told a login view opened, to start pairing when logged out
	Login func()
	// RunLog is the current run's log file, tailed by the logs view
	RunLog string
	// Shown hears every person and chat key a row was built with, for
	// fetching what the rows want, avatars first
	Shown func(keys []string)
}

type Reads struct {
	core  *core.DB
	r     *model.Reader
	ids   *model.IDs
	live  *live.Hub
	board *status.Board
	media string
	log   zerolog.Logger
	login func()
	// runLog is the current run's log file, tailed by the logs view
	runLog string
	seen  func(keys []string)
	srv   *server.Server

	mu    sync.Mutex
	world *model.World
	// gen moves whenever the world is dropped or patched: a world read from
	// before that is not kept
	gen      uint64
	previews *previews

	queue  chan core.Change
	resync atomic.Bool
}

func New(o Options) *Reads {
	return &Reads{core: o.Core, r: model.NewReader(o.Core.Read()), ids: o.IDs, live: o.Live, board: o.Board,
		media: o.MediaDir, log: o.Log, login: o.Login, runLog: o.RunLog, seen: o.Shown, previews: newPreviews(), queue: make(chan core.Change, 1024)}
}

// Reader is the model reader the views use.
func (rs *Reads) Reader() *model.Reader { return rs.r }

// IDs is the id registry the views show people under.
func (rs *Reads) IDs() *model.IDs { return rs.ids }

// Tell queues c for Run. it never blocks: the core's writer calls it.
func (rs *Reads) Tell(c core.Change) {
	select {
	case rs.queue <- c:
	default:
		// a full queue means the views are far behind: one resync covers it
		rs.resync.Store(true)
	}
}

// everything is a change that wakes every window
var everything = core.Change{All: map[string]bool{
	"chat": true, "message": true, "person": true, "chatrow": true, "pins": true, "group": true, "live": true,
	"sticker": true, "sync": true, "prefs": true, "privacy": true, "blocklist": true, "call": true, "appstate": true,
	live.TouchConn: true, live.TouchLogin: true, live.TouchTyping: true, live.TouchPresence: true,
	live.TouchTransfer: true, live.TouchNotification: true, live.TouchAbout: true, live.TouchProblems: true,
	live.TouchOlder: true, TouchClock: true, TouchSends: true,
}}

// Run brings the caches up to date with each change, then wakes the windows
// it may have moved, until ctx ends.
func (rs *Reads) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	running := map[[2]string]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			running = rs.sweepLive(ctx, running)
			rs.changed(ctx, core.Change{All: map[string]bool{TouchClock: true}})
		case c := <-rs.queue:
			if rs.resync.Swap(false) {
				rs.forgetWorld()
				rs.previews.dropAll()
				rs.changed(ctx, everything)
				continue
			}
			rs.changed(ctx, c)
		}
	}
}

func (rs *Reads) changed(ctx context.Context, c core.Change) {
	rs.apply(ctx, c)
	if rs.srv != nil {
		rs.srv.Changed(c)
	}
}

// apply drops or patches what c makes stale, before any window hears of it:
// a window reads again the moment it does.
func (rs *Reads) apply(ctx context.Context, c core.Change) {
	if c.All["person"] {
		rs.forgetWorld()
	} else if ps := c.Keys["person"]; len(ps) > 0 {
		rs.patchWorld(ctx, ps)
	}
	if c.All["chat"] || c.All["message"] || c.All["person"] {
		rs.previews.dropAll()
		return
	}
	if ps := c.Keys["person"]; len(ps) > 0 {
		rs.previews.dropPeople(ps...)
	}
	addrs := slices.Concat(c.Keys["chat"], c.Keys["chatrow"])
	for _, mk := range c.Keys["message"] {
		if addr, _, ok := strings.Cut(mk, ":"); ok {
			addrs = append(addrs, addr)
		}
	}
	if len(addrs) > 0 {
		rs.previews.drop(addrs...)
	}
}

// World is identity for this read. it is kept, and patched with the people
// a fold touched.
func (rs *Reads) World(ctx context.Context) (*model.World, error) {
	rs.mu.Lock()
	w, gen := rs.world, rs.gen
	rs.mu.Unlock()
	if w != nil {
		return w, nil
	}
	w, err := rs.r.World(ctx)
	if err != nil {
		return nil, err
	}
	rs.mu.Lock()
	if rs.gen == gen {
		rs.world = w
	}
	rs.mu.Unlock()
	return w, nil
}

func (rs *Reads) forgetWorld() {
	rs.mu.Lock()
	rs.world = nil
	rs.gen++
	rs.mu.Unlock()
}

// patchWorld reads persons again into the kept world. only Run changes a
// kept world, so nothing patches it at the same time.
func (rs *Reads) patchWorld(ctx context.Context, persons []string) {
	rs.mu.Lock()
	w := rs.world
	rs.gen++
	rs.mu.Unlock()
	if w == nil {
		return
	}
	n, err := rs.r.Patch(ctx, w, persons)
	if err != nil {
		rs.log.Warn().Err(err).Msg("views: patch world")
		rs.forgetWorld()
		return
	}
	rs.mu.Lock()
	if rs.world == w {
		rs.world = n
	}
	rs.mu.Unlock()
}

// sweepLive tells of the shares that stopped running since the last sweep,
// and returns the ones running now. an end is time passing, which no fold
// touches.
func (rs *Reads) sweepLive(ctx context.Context, was map[[2]string]bool) map[[2]string]bool {
	shares, err := rs.r.LiveShares(ctx, nil, time.Now().UnixMilli())
	if err != nil {
		rs.log.Warn().Err(err).Msg("views: live shares")
		return was
	}
	now := make(map[[2]string]bool, len(shares))
	for _, s := range shares {
		now[[2]string{s.Chat, s.ID}] = true
	}
	var ended, chats []string
	for k := range was {
		if !now[k] {
			ended = append(ended, k[0]+":"+k[1])
			chats = append(chats, k[0])
		}
	}
	if len(ended) > 0 {
		rs.changed(ctx, core.Change{Keys: map[string][]string{"message": ended, "live": chats}})
	}
	return now
}

// Register serves every view and query on srv, and wakes its windows from
// Run.
func (rs *Reads) Register(srv *server.Server) {
	rs.srv = srv
	type served struct {
		v viewFunc
		// what one item may take
		bytes int
	}
	views := map[protoreflect.FieldNumber]served{
		protoreflect.FieldNumber(v2.Subscribe_Connection_case):     {rs.connectionView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_Login_case):          {rs.loginView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_Sync_case):           {rs.syncView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_Problems_case):       {rs.problemsView, rowBytes},
		protoreflect.FieldNumber(v2.Subscribe_Chats_case):          {rs.chatsView, chatBytes},
		protoreflect.FieldNumber(v2.Subscribe_Chat_case):           {rs.chatView, chatBytes},
		protoreflect.FieldNumber(v2.Subscribe_Messages_case):       {rs.messagesView, messageBytes},
		protoreflect.FieldNumber(v2.Subscribe_Typing_case):         {rs.typingView, typingBytes},
		protoreflect.FieldNumber(v2.Subscribe_Presence_case):       {rs.presenceView, personBytes},
		protoreflect.FieldNumber(v2.Subscribe_Receipts_case):       {rs.receiptsView, personBytes},
		protoreflect.FieldNumber(v2.Subscribe_Self_case):           {rs.selfView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_Contact_case):        {rs.contactView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_Group_case):          {rs.groupView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_GroupMembers_case):   {rs.groupMembersView, personBytes},
		protoreflect.FieldNumber(v2.Subscribe_Privacy_case):        {rs.privacyView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_Preferences_case):    {rs.preferencesView, objectBytes},
		protoreflect.FieldNumber(v2.Subscribe_Blocklist_case):      {rs.blocklistView, personBytes},
		protoreflect.FieldNumber(v2.Subscribe_Starred_case):        {rs.starredView, messageBytes},
		protoreflect.FieldNumber(v2.Subscribe_Pinned_case):         {rs.pinnedView, messageBytes},
		protoreflect.FieldNumber(v2.Subscribe_LiveLocations_case):  {rs.liveLocationsView, rowBytes},
		protoreflect.FieldNumber(v2.Subscribe_ChatMedia_case):      {rs.chatMediaView, messageBytes},
		protoreflect.FieldNumber(v2.Subscribe_ChatLinks_case):      {rs.chatLinksView, messageBytes},
		protoreflect.FieldNumber(v2.Subscribe_Stickers_case):       {rs.stickersView, rowBytes},
		protoreflect.FieldNumber(v2.Subscribe_StickerPacks_case):   {rs.stickerPacksView, rowBytes},
		protoreflect.FieldNumber(v2.Subscribe_StickerPack_case):    {rs.stickerPackView, rowBytes},
		protoreflect.FieldNumber(v2.Subscribe_Transfers_case):      {rs.transfersView, rowBytes},
		protoreflect.FieldNumber(v2.Subscribe_Notifications_case):  {rs.notificationsView, rowBytes},
		protoreflect.FieldNumber(v2.Subscribe_Reactions_case):      {rs.reactionsView, personBytes},
		protoreflect.FieldNumber(v2.Subscribe_PollVotes_case):      {rs.pollVotesView, personBytes},
		protoreflect.FieldNumber(v2.Subscribe_EventResponses_case): {rs.eventResponsesView, personBytes},
		protoreflect.FieldNumber(v2.Subscribe_Logs_case):           {rs.logsView, rowBytes},
	}
	for n, v := range views {
		srv.View(n, v.v, v.bytes)
	}
	srv.Fit = func(it *v2.Upsert, limit int) bool { return Fit(it, limit) }
	srv.Handle(protoreflect.FieldNumber(v2.Request_MessageText_case), rs.messageText)
	srv.Handle(protoreflect.FieldNumber(v2.Request_SearchChats_case), rs.searchChats)
	srv.Handle(protoreflect.FieldNumber(v2.Request_SearchMessages_case), rs.searchMessages)
	srv.Handle(protoreflect.FieldNumber(v2.Request_SearchStickers_case), rs.searchStickers)
}
