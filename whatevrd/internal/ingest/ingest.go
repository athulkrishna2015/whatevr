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
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

// Log is where inputs go. core.DB is one.
type Log interface {
	AppendBatch(ctx context.Context, ins []core.Input) ([]int64, error)
}

type Ingest struct {
	ctx context.Context
	log Log
	// db is log when it is the core itself, which the jobs need to read
	db   *core.DB
	jobs *jobs
	// KeepHistoryMedia leaves downloaded history blobs on the server, for
	// another reader of the same blobs
	KeepHistoryMedia bool
	// OnDemand hears the chats of each on-demand history blob once it is
	// logged: the phone answered them
	OnDemand func(chats []string)
}

// New hands inputs to log for as long as ctx lives. once it ends every
// append fails, so nothing more is acked.
func New(ctx context.Context, log Log) *Ingest {
	g := &Ingest{ctx: ctx, log: log}
	g.db, _ = log.(*core.DB)
	return g
}

// Attach makes cli hand everything to the log and ack only what landed. it
// has to run before the client connects.
func (g *Ingest) Attach(cli *whatsmeow.Client) {
	cli.SynchronousAck = true
	cli.EnableDecryptedEventBuffer = true
	cli.AppStateMutationsHandler = g.appState
	// the jobs download history, so a blob is never fetched before its
	// notification is in the log
	cli.ManualHistorySyncDownload = true
	g.wrapLIDs(cli)
	cli.AddEventHandlerWithSuccessStatus(g.handle)
	cli.SentMessageHandler = func(ctx context.Context, to types.JID, msg *waE2E.Message, resp whatsmeow.SendResponse) {
		g.sent(cli, to, msg, resp)
	}
	cli.AddEventHandler(func(evt any) {
		switch evt.(type) {
		case *events.PairSuccess:
			// dispatched from the pairing goroutine right after the save
			// that dropped the wrapper, before anything reads the map
			g.wrapLIDs(cli)
		case *events.Connected:
			g.loggedIn(cli)
		}
	})
	// asked for at pairing, so it has to be set before one
	store.DeviceProps.HistorySyncConfig.SupportInlineContacts = proto.Bool(true)
	if g.db != nil {
		g.startJobs(cli, g.db.Read())
		// a collection that stops decoding gets a full sync, then the phone
		// is asked; never the reset that unlinks every device
		cli.AppStateRecovery = g.recovery()
	}
}

// sent logs what this device sent. the server already has it, so a failed
// append can only be reported: there is no ack to withhold.
func (g *Ingest) sent(cli *whatsmeow.Client, to types.JID, msg *waE2E.Message, resp whatsmeow.SendResponse) {
	in, err := sentInput(cli.Store.GetJID().ToNonAD(), to, msg, resp)
	var seqs []int64
	if err == nil {
		seqs, err = g.log.AppendBatch(g.ctx, []core.Input{in})
	}
	if err != nil {
		zerolog.Ctx(g.ctx).Error().Err(err).Str("stanza", resp.ID).Msg("ingest: sent message not logged")
		return
	}
	zerolog.Ctx(g.ctx).Info().Str("kind", in.Kind).Int64("input", seqs[0]).Str("stanza", resp.ID).Str("chat", to.String()).Msg("ingest: logged")
}

// loggedIn logs whose account this is and has the groups fetched.
func (g *Ingest) loggedIn(cli *whatsmeow.Client) {
	if in, ok := selfInput(cli.Store); ok {
		if _, err := g.log.AppendBatch(g.ctx, []core.Input{in}); err != nil {
			zerolog.Ctx(g.ctx).Error().Err(err).Msg("ingest: own addresses not logged")
		}
	}
	if g.jobs != nil {
		signal(g.jobs.groups)
	}
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
	seqs, err := g.log.AppendBatch(g.ctx, ins)
	if err != nil {
		log.Error().Err(err).Str("kind", ins[0].Kind).Msg("ingest: append failed, not acked")
		return false
	}
	logged(log, evt, ins, seqs)
	if ins[0].Kind == core.KindHistoryNotification && g.jobs != nil {
		evt := evt.(*events.Message)
		g.jobs.queueHistory(model.Blob{ID: evt.Info.ID, Notif: evt.Message.GetProtocolMessage().GetHistorySyncNotification()})
	}
	return true
}

// logged ties what came off the wire to its place in the log: `whatevrd
// logs stanza=<id>` finds the input, `input=<seq>` the rest.
func logged(log *zerolog.Logger, evt any, ins []core.Input, seqs []int64) {
	ev := log.Info().Str("kind", ins[0].Kind).Int64("input", seqs[0]).Int("inputs", len(ins))
	switch e := evt.(type) {
	case *events.Message:
		ev = ev.Str("stanza", e.Info.ID).Str("chat", e.Info.Chat.String())
	case *events.Receipt:
		ev = ev.Strs("stanza", e.MessageIDs).Str("chat", e.Chat.String())
	case *events.UndecryptableMessage:
		ev = ev.Str("stanza", e.Info.ID).Str("chat", e.Info.Chat.String())
	}
	ev.Msg("ingest: logged")
}

// appState runs before whatsmeow saves the collection's new version. an
// error leaves the version where it was, so the mutations come again.
func (g *Ingest) appState(ctx context.Context, name appstate.WAPatchName, version uint64, mutations []appstate.Mutation, snapshot bool) error {
	ins, err := appStateInputs(name, version, mutations, snapshot)
	if err != nil {
		return err
	}
	seqs, err := g.log.AppendBatch(g.ctx, ins)
	if err != nil {
		zerolog.Ctx(g.ctx).Error().Err(err).Str("collection", string(name)).Uint64("version", version).Msg("ingest: app state not logged, version kept")
		return err
	}
	zerolog.Ctx(g.ctx).Info().Str("kind", core.KindAppState).Int64("input", seqs[0]).Int("inputs", len(ins)).
		Str("collection", string(name)).Uint64("version", version).Msg("ingest: logged")
	return nil
}
