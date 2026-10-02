// Package ingest turns what whatsmeow hands the daemon into inputs for the
// core log, and makes whatsmeow wait for the log before it acks anything.
//
// a message is acked only after its input is durable. if the append fails the
// handler says so, whatsmeow withholds the ack and keeps the plaintext in its
// event buffer, and the server hands the same ciphertext over again later.
package ingest

import (
	"context"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// Log is where inputs go. core.DB is one.
type Log interface {
	AppendBatch(ctx context.Context, ins []core.Input) ([]int64, error)
}

type Ingest struct {
	ctx context.Context
	log Log
}

// New hands inputs to log for as long as ctx lives. once it ends every
// append fails, so nothing more is acked.
func New(ctx context.Context, log Log) *Ingest {
	return &Ingest{ctx: ctx, log: log}
}

// Attach makes cli hand everything to the log and ack only what landed. it
// has to run before the client connects.
func (g *Ingest) Attach(cli *whatsmeow.Client) {
	cli.SynchronousAck = true
	cli.EnableDecryptedEventBuffer = true
	cli.AppStateMutationsHandler = g.appState
	cli.AddEventHandlerWithSuccessStatus(g.handle)
	// asked for at pairing, so it has to be set before one
	store.DeviceProps.HistorySyncConfig.SupportInlineContacts = proto.Bool(true)
}

// handle reports false when the event's inputs could not be appended, which
// is whatsmeow's cue not to ack it.
func (g *Ingest) handle(evt any) bool {
	ins, err := inputsFor(evt)
	log := zerolog.Ctx(g.ctx)
	if err != nil {
		// an event we cannot encode will not encode any better next time
		log.Error().Err(err).Type("event", evt).Msg("ingest: event not logged")
		return true
	}
	if len(ins) == 0 {
		return true
	}
	if _, err := g.log.AppendBatch(g.ctx, ins); err != nil {
		log.Error().Err(err).Str("kind", ins[0].Kind).Msg("ingest: append failed, not acked")
		return false
	}
	return true
}

// appState runs before whatsmeow saves the collection's new version. an
// error leaves the version where it was, so the mutations come again.
func (g *Ingest) appState(ctx context.Context, name appstate.WAPatchName, version uint64, mutations []appstate.Mutation, snapshot bool) error {
	ins, err := appStateInputs(name, version, mutations, snapshot)
	if err != nil {
		return err
	}
	if _, err := g.log.AppendBatch(g.ctx, ins); err != nil {
		zerolog.Ctx(g.ctx).Error().Err(err).Str("collection", string(name)).Uint64("version", version).Msg("ingest: app state not logged, version kept")
		return err
	}
	return nil
}
