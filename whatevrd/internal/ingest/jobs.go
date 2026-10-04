package ingest

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

// jobs fetch what an event only points at: history blobs and groups. a
// result goes in the log like any other input, a fetch that can never work
// goes in as its failure so nobody tries forever.
type jobs struct {
	g    *Ingest
	read *sql.DB

	mu  sync.Mutex
	cli *whatsmeow.Client
	// history downloads come off the media servers, two at a time is the
	// phone's pace with room to spare
	history *pool[model.Blob]
	// group info is an iq the server rate limits
	group  *pool[string]
	groups chan struct{}
}

const (
	historyWorkers = 2
	groupWorkers   = 1
	groupGap       = 200 * time.Millisecond
	// jobPage is how much pending work is read from the store at once
	jobPage = 64
	// historyPiece is the most messages one history input holds, so no one
	// fold reads a chat's whole history
	historyPiece = 500
	// historyBatch is about how many bytes of a blob go in one append
	historyBatch = 4 << 20
)

// startJobs runs the jobs on cli, or moves running ones to it: a new
// pairing makes a new client.
func (g *Ingest) startJobs(cli *whatsmeow.Client, read *sql.DB) {
	if g.jobs != nil {
		g.jobs.mu.Lock()
		g.jobs.cli = cli
		g.jobs.mu.Unlock()
		return
	}
	j := &jobs{g: g, cli: cli, read: read, groups: make(chan struct{}, 1)}
	j.history = newPool("history", historyWorkers, 0, func(b model.Blob) string { return b.ID }, j.download)
	j.group = newPool("group", groupWorkers, groupGap, func(g string) string { return g }, j.fetchGroup)
	g.jobs = j
	j.history.start(g.ctx)
	j.group.start(g.ctx)
	go j.requeueHistory(g.ctx)
	go j.groupLoop()
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (j *jobs) client() *whatsmeow.Client {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cli
}

func (j *jobs) queueHistory(b model.Blob) { j.history.add(b) }

// requeueHistory queues every notification the log has no download for:
// the last run ended before it got to them. one still folding at startup
// is not pending yet, so it waits for the fold first.
func (j *jobs) requeueHistory(ctx context.Context) {
	log := zerolog.Ctx(ctx)
	if err := j.g.Folded(ctx); err != nil {
		if ctx.Err() == nil {
			log.Error().Err(err).Msg("ingest: pending history not read")
		}
		return
	}
	queued := 0
	for after := int64(0); ; {
		pending, next, err := model.PendingHistory(ctx, j.read, after, jobPage)
		if err != nil {
			log.Error().Err(err).Msg("ingest: pending history not read")
			return
		}
		for _, b := range pending {
			if j.history.add(b) {
				queued++
			}
		}
		if next == 0 || !j.history.below(ctx, historyWorkers) {
			break
		}
		after = next
	}
	if queued > 0 {
		log.Info().Int("blobs", queued).Msg("ingest: history left from the last run queued")
	}
}

// gone says a blob will never download: the server dropped it or it does
// not decode.
func gone(err error) bool {
	for _, e := range []error{
		whatsmeow.ErrMediaDownloadFailedWith403, whatsmeow.ErrMediaDownloadFailedWith404, whatsmeow.ErrMediaDownloadFailedWith410,
		whatsmeow.ErrNoURLPresent, whatsmeow.ErrInvalidMediaHMAC, whatsmeow.ErrInvalidMediaEncSHA256, whatsmeow.ErrInvalidMediaSHA256,
		whatsmeow.ErrFileLengthMismatch,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func (j *jobs) download(ctx context.Context, b model.Blob) error {
	log := zerolog.Ctx(ctx)
	h := core.HistoryExtraHead{
		Notification: b.ID, SyncType: b.Notif.GetSyncType().String(),
		ChunkOrder: b.Notif.GetChunkOrder(), Progress: b.Notif.GetProgress(),
	}
	cli := j.client()
	if cli == nil {
		return errors.New("no client yet")
	}
	// one time for the whole blob: its messages keep their order across pieces
	at := time.Now()
	var batch []core.Input
	var first int64
	size, convs, pieces := 0, 0, 0
	var took time.Duration
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		start := time.Now()
		seqs, err := j.g.log.AppendBatch(ctx, batch)
		if err != nil {
			return err
		}
		took += time.Since(start)
		if first == 0 {
			first = seqs[0]
		}
		batch, size = nil, 0
		return nil
	}
	// a blob that dies halfway leaves its first conversations logged. it is
	// downloaded again, and the fold takes them again as no-ops
	hs, err := cli.DownloadHistorySyncStream(ctx, b.Notif, func(c *waHistorySync.Conversation) error {
		ins, err := historyPieces(h, c, at)
		if err != nil {
			return err
		}
		convs++
		pieces += len(ins)
		for _, in := range ins {
			batch = append(batch, in)
			if size += len(in.Body); size >= historyBatch {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if !gone(err) && !undecodable(err) {
			return err
		}
		h.Error = err.Error()
		in, ierr := input(core.KindHistoryExtra, h, nil)
		if ierr != nil {
			return ierr
		}
		in.At = at
		if err := flush(); err != nil {
			return err
		}
		if _, err := j.g.log.AppendBatch(ctx, []core.Input{in}); err != nil {
			return err
		}
		log.Error().Str("notification", b.ID).Str("error", h.Error).Int("conversations", convs).Msg("ingest: history blob lost for good")
		return nil
	}
	body, err := marshal.Marshal(hs)
	if err != nil {
		return err
	}
	h.Conversations = convs
	in, err := input(core.KindHistoryExtra, h, body)
	if err != nil {
		return err
	}
	in.At = at
	batch = append(batch, in)
	if err := flush(); err != nil {
		return err
	}
	log.Info().Int64("input", first).Str("notification", b.ID).Str("type", h.SyncType).Uint32("chunk", h.ChunkOrder).Uint32("progress", h.Progress).
		Int("conversations", convs).Int("pieces", pieces).Dur("append", took).Msg("ingest: history blob logged")
	// a chunk names groups the account may have left
	signal(j.groups)
	if b.Notif.InitialHistBootstrapInlinePayload == nil && !j.g.KeepHistoryMedia {
		if err := cli.DeleteMedia(ctx, whatsmeow.MediaHistory, b.Notif.GetDirectPath(), b.Notif.GetFileEncSHA256(), b.Notif.GetEncHandle()); err != nil {
			log.Debug().Err(err).Str("notification", b.ID).Msg("ingest: history blob not deleted from the server")
		}
	}
	return nil
}

// undecodable is a blob that downloaded but does not decompress or parse.
// whatsmeow says so only in words.
func undecodable(err error) bool {
	for _, s := range []string{"failed to prepare to decompress", "failed to decompress", "failed to unmarshal"} {
		if strings.Contains(err.Error(), s) {
			return true
		}
	}
	return false
}

// historyPieces is c as inputs of at most historyPiece messages, each with
// the offset of its first. the first piece has the rest of c, the others only
// its id.
func historyPieces(h core.HistoryExtraHead, c *waHistorySync.Conversation, at time.Time) ([]core.Input, error) {
	msgs := c.Messages
	ins := make([]core.Input, 0, max(1, (len(msgs)+historyPiece-1)/historyPiece))
	for off := 0; off == 0 || off < len(msgs); off += historyPiece {
		piece := c
		if off > 0 {
			piece = &waHistorySync.Conversation{ID: c.ID}
		}
		piece.Messages = msgs[off:min(off+historyPiece, len(msgs))]
		body, err := marshal.Marshal(piece)
		if err != nil {
			return nil, err
		}
		in, err := input(core.KindHistoryConversation, core.HistoryConversationHead{
			Notification: h.Notification, SyncType: h.SyncType, ChunkOrder: h.ChunkOrder, ID: c.GetID(), Offset: off,
		}, body)
		if err != nil {
			return nil, err
		}
		in.At = at
		ins = append(ins, in)
	}
	return ins, nil
}

func (j *jobs) groupLoop() {
	ctx := j.g.ctx
	for {
		select {
		case <-j.groups:
		case <-ctx.Done():
			return
		}
		if !j.client().IsLoggedIn() {
			continue
		}
		j.fetchGroups(ctx)
		// one round at a time, history chunks come by the dozen
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

// fetchGroups takes every joined group in one call, then asks one by one
// about groups named somewhere that the account is no longer in.
func (j *jobs) fetchGroups(ctx context.Context) {
	log := zerolog.Ctx(ctx)
	cli := j.client()
	joined, err := cli.GetJoinedGroups(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("ingest: joined groups not fetched")
		return
	}
	ins := make([]core.Input, 0, len(joined))
	for _, gi := range joined {
		h := fullGroup(gi, time.Time{})
		in, err := input(core.KindGroupInfo, h, nil)
		if err != nil {
			continue
		}
		ins = append(ins, in)
	}
	if _, err := j.g.log.AppendBatch(ctx, ins); err != nil {
		log.Error().Err(err).Msg("ingest: joined groups not logged")
		return
	}
	if j.g.db != nil {
		if _, appended := j.g.db.Progress(); appended > 0 {
			_ = j.g.db.WaitFolded(ctx, appended)
		}
	}
	for after := ""; ; {
		missing, err := model.UnfetchedGroups(ctx, j.read, after, jobPage)
		if err != nil {
			log.Error().Err(err).Msg("ingest: unfetched groups not read")
			return
		}
		for _, g := range missing {
			j.group.add(g)
		}
		if len(missing) < jobPage || !j.group.below(ctx, groupWorkers) {
			return
		}
		after = missing[len(missing)-1]
	}
}

// fetchGroup asks about one group named somewhere that the account is not
// in. a group that is gone is logged as gone.
func (j *jobs) fetchGroup(ctx context.Context, g string) error {
	gj, err := types.ParseJID(g)
	if err != nil {
		return nil
	}
	cli := j.client()
	if !cli.IsLoggedIn() {
		return whatsmeow.ErrNotLoggedIn
	}
	var h core.GroupInfoHead
	info, err := cli.GetGroupInfo(ctx, gj)
	switch {
	case err == nil:
		h = fullGroup(info, time.Time{})
	case errors.Is(err, whatsmeow.ErrGroupNotFound), errors.Is(err, whatsmeow.ErrNotInGroup):
		h = core.GroupInfoHead{JID: g, Error: err.Error()}
	default:
		return err
	}
	in, err := input(core.KindGroupInfo, h, nil)
	if err != nil {
		return nil
	}
	_, err = j.g.log.AppendBatch(ctx, []core.Input{in})
	return err
}
