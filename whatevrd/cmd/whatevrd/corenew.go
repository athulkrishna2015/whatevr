//go:build whatevr_core

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"

	"whatevrd/internal/app"
	"whatevrd/internal/conn"
	"whatevrd/internal/core"
	"whatevrd/internal/ingest"
	"whatevrd/internal/model"
	"whatevrd/internal/protocol"
	"whatevrd/internal/status"
	"whatevrd/internal/store"
	"whatevrd/internal/v1"
	"whatevrd/internal/wa"
)

type coreFlagSet struct {
	core *string
}

func coreFlags(zerolog.Logger) *coreFlagSet {
	return &coreFlagSet{core: flag.String("core", "new", "which core serves the frontends: new, or old (debug builds only)")}
}

// newCore is the new core running beside the old one: every event lands in
// both, the frontends read the new one.
type newCore struct {
	db      *core.DB
	adapter *v1.Adapter
	ingest  *ingest.Ingest
	conn    *conn.Machine
	board   *status.Board
	poke    chan struct{}
	// wa is the old client, there once register runs: the machine's login
	// and version hooks go through it
	wa atomic.Pointer[wa.Client]
	// mocked runs watch no host network: the mock is on loopback
	mocked bool
	log    zerolog.Logger
	path   string
}

func coreOpen(ctx context.Context, log zerolog.Logger, f *coreFlagSet, paths app.Paths, daemon *app.Daemon, old *store.DB, mocked *mockClocks) *newCore {
	switch *f.core {
	case "old":
		return nil
	case "new":
	default:
		log.Fatal().Str("core", *f.core).Msg("--core is old or new")
	}
	var adapter atomic.Pointer[v1.Adapter]
	var clock core.Clock
	var wall func() time.Time
	if mocked != nil {
		clock, wall = clockFunc(mocked.stamp), mocked.wall
	}
	db, err := core.Open(ctx, filepath.Join(paths.DataDir, "core.db"), core.Options{
		Clock:   clock,
		Domains: model.Domains(),
		OnChange: func(c core.Change) {
			if a := adapter.Load(); a != nil {
				a.OnChange(c)
			}
		},
		Log: log.With().Str("module", "core").Logger(),
	})
	if err != nil {
		log.Fatal().Err(err).Msg("open the new core")
	}
	a := v1.New(db, nil, old, daemon, log.With().Str("module", "v1").Logger())
	adapter.Store(a)
	g := ingest.New(ctx, db)
	g.KeepHistoryMedia = true
	path := filepath.Join(paths.DataDir, "core.db")
	n := &newCore{db: db, adapter: a, ingest: g, mocked: mocked != nil, log: log, path: path,
		board: status.New(), poke: make(chan struct{}, 1)}
	n.board.OnChange = func(status.Kind, *status.Problem) {
		select {
		case n.poke <- struct{}{}:
		default:
		}
	}
	n.board.Provide("sync", syncProblems(db))
	n.board.Provide("outbox", outboxProblems(db))
	n.board.Provide("core", coreProblems(db))
	var network conn.Network
	if !n.mocked {
		network = conn.WatchNetwork(ctx, log.With().Str("module", "conn").Logger())
	}
	n.conn = conn.New(conn.Options{
		Network: network,
		Log:     log.With().Str("module", "conn").Logger(),
		Publish: func(s conn.Status) {
			publishConnection(daemon, s)
			if ps, ok := connProblems(s); ok {
				if err := n.board.Only("conn", ps...); err != nil {
					log.Error().Err(err).Msg("status: conn")
				}
			}
		},
		Login: func(ctx context.Context, cli *whatsmeow.Client) error {
			w := n.wa.Load()
			if w == nil {
				return errors.New("no client yet")
			}
			err := w.QRLogin(ctx, cli)
			if wa.QRExpired(err) {
				return conn.ErrRetryNow
			}
			return err
		},
		Before: func(ctx context.Context) {
			if w := n.wa.Load(); w != nil && !n.mocked {
				w.RefreshVersion(ctx)
			}
		},
		Wall: wall,
	})
	log.Warn().Str("db", filepath.Join(paths.DataDir, "core.db")).Msg("new core on: frontends read it, the old core still acts")
	return n
}

type clockFunc func() time.Time

func (f clockFunc) Now() time.Time { return f() }

// publishConnection is the machine's state in v1's words.
func publishConnection(daemon *app.Daemon, s conn.Status) {
	state := app.StateOffline
	switch s.Kind {
	case conn.Starting:
		state = app.StateStarting
	case conn.Online:
		state = app.StateOnline
	case conn.Connecting:
		state = app.StateConnecting
		if s.Attempt > 1 || strings.HasPrefix(s.Detail, "Reconnecting") {
			state = app.StateReconnecting
		}
	case conn.NeedLogin, conn.LoggedOut:
		state = app.StateNeedLogin
	}
	var next int64
	if !s.Next.IsZero() {
		next = s.Next.Unix()
	}
	daemon.SetConnection(state, s.Detail, int32(s.Attempt), next, s.Manual)
}

// outbox is the core's outbox as the old client sees it.
type outbox struct{ *ingest.Ingest }

func (o outbox) MaySend(ctx context.Context, chat, id string) error {
	err := o.Ingest.MaySend(ctx, chat, id)
	if errors.Is(err, ingest.ErrSent) {
		return wa.ErrAlreadySent
	}
	return err
}

// connection is the machine as the old client sees it.
type connection struct{ m *conn.Machine }

func (c connection) Reconnect()  { c.m.Kick(conn.ReasonManual) }
func (c connection) WantOnline() { c.m.Kick(conn.ReasonSend) }

// instrument has every whatsmeow client log into the new core too, and the
// old core leave history blobs for it.
func (n *newCore) instrument(inst wa.Instrument) wa.Instrument {
	if n == nil {
		return inst
	}
	prev := inst.Client
	inst.Client = func(cli *whatsmeow.Client) {
		if prev != nil {
			prev(cli)
		}
		n.ingest.Attach(cli)
		n.conn.Attach(cli)
	}
	inst.KeepHistoryMedia = true
	inst.Connection = connection{n.conn}
	inst.Outbox = outbox{n.ingest}
	inst.Guard = n.ingest.Guard
	media := &mediaWatch{board: n.board, log: n.log, now: time.Now}
	inst.Media = media.result
	inst.Wipe = func(ctx context.Context) error {
		backup := filepath.Join(filepath.Dir(n.path), fmt.Sprintf("core-before-logout-%d.db", time.Now().Unix()))
		return n.db.Reset(ctx, backup)
	}
	return inst
}

// register puts the views and commands on the server, served from the new
// core when it is on.
func (n *newCore) register(ctx context.Context, s *protocol.Server, daemon *app.Daemon, old *store.DB, waClient *wa.Client) {
	if n == nil {
		protocol.RegisterDaemonViews(s, daemon, old, waClient)
		protocol.RegisterDaemonCommands(s, waClient)
		return
	}
	n.adapter.Bind(waClient)
	n.wa.Store(waClient)
	go n.adapter.Run(ctx)
	if !n.mocked {
		go conn.WatchSleep(ctx, n.log, func() { n.conn.Kick(conn.ReasonResume) })
	}
	go n.conn.Run(ctx)
	go watchBoard(ctx, n.log.With().Str("module", "status").Logger(), n.board, n.poke, 30*time.Second)
	protocol.RegisterDaemonViews(s, daemon, n.adapter, waClient)
	protocol.RegisterDaemonCommands(s, n.adapter.Commands())
}

func (n *newCore) close() {
	if n != nil {
		n.db.Close()
	}
}
