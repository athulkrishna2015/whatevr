package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

var (
	// ErrNotQueued is a send this daemon never queued: a message history
	// marked pending or failed is the phone's business, not ours to resend.
	ErrNotQueued = errors.New("not queued by this daemon")
	ErrCancelled = errors.New("cancelled")
	ErrSent      = errors.New("already sent")
	// ErrGuarded is the daemon's half of the send guard: the fork's refuses
	// the stanza at the socket, this one before anything is built for it.
	ErrGuarded = errors.New("blocked by the send guard")
)

// Once is a send's key and a digest of the rest of its params, zero for a
// send without a key.
type Once struct{ Key, Params string }

// Queued logs that this daemon queued id for chat: body as it will go, file
// the media to upload into it first. only a send logged here ever goes out.
func (g *Ingest) Queued(ctx context.Context, chat, id string, body *waE2E.Message, file string, o Once) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = proto.Marshal(body); err != nil {
			return err
		}
	}
	return g.append(ctx, core.OutboxHead{Op: core.OutboxQueue, Chat: chat, ID: id, File: file, Key: o.Key, Params: o.Params}, raw)
}

// Cancel takes a queued send back, if it has not gone out.
func (g *Ingest) Cancel(ctx context.Context, chat, id string) error {
	return g.outbox(ctx, core.OutboxHead{Op: core.OutboxCancel, Chat: chat, ID: id})
}

// Attempted logs a try that failed. final says it will not be tried again.
func (g *Ingest) Attempted(ctx context.Context, chat, id string, err error, final bool) {
	msg := "failed"
	if err != nil {
		msg = err.Error()
	}
	if e := g.outbox(ctx, core.OutboxHead{Op: core.OutboxAttempt, Chat: chat, ID: id, Error: msg, Final: final}); e != nil {
		zerolog.Ctx(g.ctx).Warn().Err(e).Str("id", id).Msg("ingest: send attempt not logged")
	}
}

func (g *Ingest) outbox(ctx context.Context, h core.OutboxHead) error {
	return g.append(ctx, h, nil)
}

func (g *Ingest) append(ctx context.Context, h core.OutboxHead, body []byte) error {
	head, err := json.Marshal(h)
	if err != nil {
		return err
	}
	_, err = g.log.AppendBatch(ctx, []core.Input{{Kind: core.KindOutbox, V: 1, Head: head, Body: body}})
	return err
}

// MaySend says whether id may go out to chat now: queued here, not
// cancelled, not already out, and past the send guard when there is one.
func (g *Ingest) MaySend(ctx context.Context, chat, id string) error {
	if g.db == nil {
		return errors.New("no core to ask")
	}
	// the queue input may still be folding
	if err := g.folded(ctx); err != nil {
		return fmt.Errorf("outbox not folded: %w", err)
	}
	r := model.NewReader(g.db.Read())
	o, ok, err := r.Outgoing(ctx, chat, id)
	switch {
	case err != nil:
		return err
	case !ok:
		return ErrNotQueued
	case o.Cancelled:
		return ErrCancelled
	case o.Sent:
		return ErrSent
	}
	return g.guard(ctx, r, chat)
}

// Guard is the daemon's half of the send guard on its own: nil when the
// client has no guard or chat is allowed.
func (g *Ingest) Guard(ctx context.Context, chat string) error {
	if g.db == nil {
		return nil
	}
	return g.guard(ctx, model.NewReader(g.db.Read()), chat)
}

func (g *Ingest) guard(ctx context.Context, r *model.Reader, chat string) error {
	if g.jobs == nil {
		return nil
	}
	cli := g.jobs.client()
	if cli == nil || cli.SendGuard == nil {
		return nil
	}
	w, err := r.World(ctx)
	if err != nil {
		return err
	}
	if model.IsGroup(chat) {
		for _, a := range cli.SendGuard.Allow {
			if a.ToNonAD().String() == chat {
				return nil
			}
		}
		return ErrGuarded
	}
	if w.IsSelf(chat) {
		return nil
	}
	// a person matches by number or lid, whichever the identity map knows
	key := w.Now(chat)
	for _, a := range cli.SendGuard.Allow {
		if w.Now(types.NewJID(a.User, a.Server).String()) == key {
			return nil
		}
	}
	return ErrGuarded
}
