package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"whatevrd/internal/app"
	"whatevrd/internal/commands"
	"whatevrd/internal/conn"
	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/logx"
	"whatevrd/internal/model"
	"whatevrd/internal/notify"
	"whatevrd/internal/server"
	"whatevrd/internal/status"
	"whatevrd/internal/views"
	"whatevrd/internal/whatsapp"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	memstatusOff()
	limitMemory()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "paths":
			os.Exit(runPaths(os.Args[2:], os.Stdout, os.Stderr))
		case "service":
			os.Exit(runService(os.Args[2:], os.Stdout, os.Stderr))
		case "notifications":
			os.Exit(runNotifications(os.Args[2:], os.Stdout, os.Stderr))
		case "pair":
			os.Exit(runPair(os.Args[2:], os.Stdout, os.Stderr))
		case "logs":
			os.Exit(runLogs(os.Args[2:], os.Stdout, os.Stderr))
		case "capture":
			os.Exit(runCapture(os.Args[2:], os.Stdout, os.Stderr))
		case "mock":
			os.Exit(runMock(os.Args[2:], os.Stdout, os.Stderr))
		case "rederive":
			os.Exit(runRederive(os.Args[2:], os.Stdout, os.Stderr))
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	heapProfiles(ctx)

	fileLevel, stderrLevel, levelErr := logx.LevelsFromEnv()
	// stderr only until the run file exists
	log := logx.Console(os.Stderr, zerolog.InfoLevel)
	if levelErr != nil {
		log.Fatal().Err(levelErr).Msg("bad log level")
	}

	// every flag is parsed in every build, so a typo stops here instead of
	// starting a daemon on the real account
	mockFlagSet := mockFlags(log)
	captureFlagSet := captureFlags(log)
	flag.Parse()
	if flag.NArg() > 0 {
		log.Fatal().Strs("args", flag.Args()).Msg("unexpected arguments")
	}

	// mock and capture runs move the XDG dirs, so they settle before any
	// path resolves. both are nil in a release build
	mock := mockPrepare(log, mockFlagSet)
	capture := capturePrepare(log, captureFlagSet, mockScenario(mock))

	paths, err := app.ResolvePaths()
	if err != nil {
		log.Fatal().Err(err).Msg("resolve paths")
	}
	if err := paths.Ensure(); err != nil {
		log.Fatal().Err(err).Msg("create runtime/data directories")
	}

	run, err := logx.Start(logx.Config{Dir: paths.LogDir, FileLevel: fileLevel, StderrLevel: stderrLevel})
	if err != nil {
		log.Fatal().Err(err).Msg("start the run log")
	}
	defer run.Close()
	log = run.Logger
	ctx = log.WithContext(ctx)
	log.Info().Str("version", version).Int("pid", os.Getpid()).Str("file", run.Path).
		Stringer("file_level", fileLevel).Stringer("stderr_level", stderrLevel).Msg("whatevrd starting")

	hooks, stopCapture := captureStart(ctx, capture, run.ID)
	defer stopCapture()

	// a socket systemd handed over, with LISTEN_* cleared so no child
	// inherits it. nil runs standalone
	activated, err := app.SystemdListener()
	if err != nil {
		log.Fatal().Err(err).Msg("adopt systemd socket")
	}
	processLock, err := app.AcquireProcessLock(paths.LockPath)
	if err != nil {
		log.Fatal().Err(err).Msg("acquire process lock")
	}
	defer processLock.Close()

	clocks := mockTime(mock)
	var clock core.Clock
	var wall func() time.Time
	if clocks != nil {
		clock, wall = clockFunc(clocks.stamp), clocks.wall
	}

	// the views hear every change the log and the live hub make
	var reads atomic.Pointer[views.Reads]
	tell := func(c core.Change) {
		if rs := reads.Load(); rs != nil {
			rs.Tell(c)
		}
	}
	corePath := filepath.Join(paths.DataDir, "whatevr.db")
	db, err := core.Open(ctx, corePath, core.Options{
		Clock:      clock,
		Domains:    model.Domains(),
		OnChange:   tell,
		Log:        log.With().Str("module", "core").Logger(),
		LogIndexes: []string{model.IDIndex},
	})
	if err != nil {
		log.Fatal().Err(err).Msg("open the core")
	}
	defer db.Close()
	ids, err := model.LoadIDs(ctx, db)
	if err != nil {
		log.Fatal().Err(err).Msg("read ids")
	}
	mockIDs(mock, ids)

	hub := live.New()
	qr := newQRWatch(hub)
	hub.OnChange = func(c core.Change) {
		tell(c)
		qr.changed(c)
	}

	board := status.New()
	poke := make(chan struct{}, 1)
	board.OnChange = func(status.Kind, *status.Problem) {
		hub.Problems()
		select {
		case poke <- struct{}{}:
		default:
		}
	}
	board.Provide("sync", syncProblems(db))
	board.Provide("outbox", outboxProblems(db))
	board.Provide("core", coreProblems(db))
	media := &mediaWatch{board: board, log: log, now: time.Now}

	// the fake server binds before the client: whatsmeow snapshots
	// http.DefaultTransport when it builds one
	stopMock, err := mockStart(ctx, mock, qr, paths.SocketPath)
	if err != nil {
		log.Fatal().Err(err).Msg("start mock server")
	}
	defer stopMock()

	srv, err := server.New(server.Options{
		Listener:   activated,
		SocketPath: paths.SocketPath,
		Version:    version,
		DataDir:    paths.DataDir,
		CacheDir:   paths.CacheDir,
		Log:        log.With().Str("module", "server").Logger(),
		Tap:        captureTap(capture),
	})
	if err != nil {
		log.Fatal().Err(err).Msg("start the protocol server")
	}

	// a nil *notify.Worker in the interface would not be a nil interface
	var notifier whatsapp.Notifier
	if mockSilencesNotifications(mock) {
		log.Info().Msg("notifications disabled: mock mode")
	} else if w, err := notify.NewWorker(ctx, log.With().Str("module", "notify").Logger(), srv.OpenChat); err != nil {
		log.Warn().Err(err).Msg("notifications disabled")
	} else {
		w.Start(ctx)
		notifier = w
	}

	var network conn.Network
	if mock == nil {
		network = conn.WatchNetwork(ctx, log.With().Str("module", "conn").Logger())
	}
	var client *whatsapp.Client
	rs := views.New(views.Options{
		Core:     db,
		IDs:      ids,
		Live:     hub,
		Board:    board,
		MediaDir: paths.MediaCacheDir,
		Log:      log.With().Str("module", "views").Logger(),
		Login:    func() { client.WantLogin() },
		Shown:    func(keys []string) { client.WantAvatars(keys) },
	})
	client, err = whatsapp.New(ctx, whatsapp.Options{
		Paths:     paths,
		Core:      db,
		IDs:       ids,
		Live:      hub,
		World:     rs.World,
		Log:       log.With().Str("module", "whatsapp").Logger(),
		Hook:      hooks.client,
		Transport: hooks.transport,
		Network:   network,
		Wall:      wall,
		Conn: func(s conn.Status) {
			if ps, ok := connProblems(s); ok {
				if err := board.Only("conn", ps...); err != nil {
					log.Error().Err(err).Msg("status: conn")
				}
			}
		},
		Mocked:   mock != nil,
		Notifier: notifier,
		Media:    media.result,
		Relink:   oldDaemon(paths.DataDir),
	})
	if err != nil {
		log.Fatal().Err(err).Msg("start the whatsapp client")
	}
	defer client.Close()
	reads.Store(rs)

	rs.Register(srv)
	commands.Register(commands.Options{Server: srv, Client: client, Reads: rs, Log: log.With().Str("module", "commands").Logger()})
	go rs.Run(ctx)
	go watchBoard(ctx, log.With().Str("module", "status").Logger(), board, poke, 30*time.Second)
	// every view and command is on the server before it accepts
	srv.Serve(ctx)
	go client.Run(ctx)
	log.Info().Str("socket", paths.SocketPath).Msg("whatevrd listening")

	select {
	case <-ctx.Done():
		log.Info().Msg("whatevrd shutting down")
		if err := <-srv.Err(); err != nil {
			log.Fatal().Err(err).Msg("protocol server failed during shutdown")
		}
	case err := <-srv.Err():
		if err != nil {
			log.Fatal().Err(err).Msg("protocol server failed")
		}
	}
}

type clockFunc func() time.Time

func (f clockFunc) Now() time.Time { return f() }

// qrWatch hands the mock every QR the daemon shows, as the mock's phone
// scans what is on screen.
type qrWatch struct {
	hub  *live.Hub
	wake chan struct{}
}

func newQRWatch(hub *live.Hub) *qrWatch { return &qrWatch{hub: hub, wake: make(chan struct{}, 1)} }

func (q *qrWatch) changed(c core.Change) {
	if _, ok := c.Keys[live.TouchLogin]; !ok && !c.All[live.TouchLogin] {
		return
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *qrWatch) QRCodes() (<-chan string, func()) {
	out := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		last := ""
		for {
			if l := q.hub.Login(); l.State == live.ShowingQR && l.QR != "" && l.QR != last {
				last = l.QR
				select {
				case out <- l.QR:
				case <-done:
					return
				}
			}
			select {
			case <-q.wake:
			case <-done:
				return
			}
		}
	}()
	return out, func() { close(done) }
}

// oldDaemon clears what the daemon before this core kept, nil when it left
// nothing behind
func oldDaemon(dir string) func() error {
	if _, err := os.Stat(filepath.Join(dir, "whatevrd.db")); err != nil {
		return nil
	}
	return func() error {
		files, err := filepath.Glob(filepath.Join(dir, "whatevrd-before-logout-*.db*"))
		if err != nil {
			return err
		}
		for _, f := range []string{"whatevrd.db", "whatevrd.db-wal", "whatevrd.db-shm", "whatevrd.db-journal"} {
			files = append(files, filepath.Join(dir, f))
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		return nil
	}
}
