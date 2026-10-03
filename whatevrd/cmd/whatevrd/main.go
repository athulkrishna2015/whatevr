package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"whatevrd/internal/app"
	"whatevrd/internal/logx"
	"whatevrd/internal/notify"
	"whatevrd/internal/protocol"
	"whatevrd/internal/store"
	"whatevrd/internal/wa"
)

func main() {
	memstatusOff()
	limitMemory()
	if len(os.Args) > 1 && os.Args[1] == "pair" {
		os.Exit(runPair(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "logs" {
		os.Exit(runLogs(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "capture" {
		os.Exit(runCapture(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "rederive" {
		os.Exit(runRederive(os.Args[2:], os.Stdout, os.Stderr))
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
	coreFlagSet := coreFlags(log)
	flag.Parse()
	if flag.NArg() > 0 {
		log.Fatal().Strs("args", flag.Args()).Msg("unexpected arguments")
	}

	// Mock mode repoints the XDG directories at a scratch tree, so it has to
	// settle before anything resolves a path. A capture of the real account
	// does the same for its own linked device. In a release build both return
	// nil.
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
	log.Info().Str("version", protocol.Version).Int("pid", os.Getpid()).Str("file", run.Path).
		Stringer("file_level", fileLevel).Stringer("stderr_level", stderrLevel).Msg("whatevrd starting")

	instrument, stopCapture := captureStart(ctx, capture, run.ID)
	defer stopCapture()

	// Adopt a systemd-activated socket if present (and clear LISTEN_* so it is
	// never inherited by child processes). nil means run standalone.
	activatedListener, err := app.SystemdListener()
	if err != nil {
		log.Fatal().Err(err).Msg("adopt systemd socket")
	}

	processLock, err := app.AcquireProcessLock(paths.LockPath)
	if err != nil {
		log.Fatal().Err(err).Msg("acquire process lock")
	}
	defer processLock.Close()

	db, err := store.Open(ctx, paths.DatabasePath)
	if err != nil {
		log.Fatal().Err(err).Msg("open sqlite database")
	}
	defer db.Close()

	daemon := app.NewDaemon(paths)

	newCore := coreOpen(ctx, log, coreFlagSet, paths, daemon, db, mockTime(mock))
	defer newCore.close()
	instrument = newCore.instrument(instrument)

	// The fake WhatsApp server has to bind before wa.New, because whatsmeow
	// snapshots http.DefaultTransport when it constructs its client.
	stopMock, err := mockStart(ctx, mock, daemon)
	if err != nil {
		log.Fatal().Err(err).Msg("start mock server")
	}
	defer stopMock()

	// The whatevr protocol server (PROTOCOL.md) is the daemon's only frontend
	// interface.
	protocolServer, err := protocol.New(paths.SocketPath, activatedListener, daemon)
	if err != nil {
		log.Fatal().Err(err).Msg("start protocol server")
	}
	if tap := captureTap(capture); tap != nil {
		protocolServer.SetTap(tap)
	}

	// The protocol server routes daemon→frontend pushes (open_chat on a
	// notification click) as connection-directed events.
	var notificationWorker *notify.Worker
	if mockSilencesNotifications(mock) {
		log.Info().Msg("notifications disabled: mock mode")
	} else if notificationWorker, err = notify.NewWorker(ctx, protocolServer); err != nil {
		log.Warn().Err(err).Msg("notifications disabled")
	}
	// Built separately rather than passed straight in: a nil *notify.Worker
	// inside an interface is not a nil interface, and wa.Client checks for a
	// nil interface before it notifies.
	var notifier wa.MessageNotifier
	if notificationWorker != nil {
		notifier = notificationWorker
	}
	if notificationWorker != nil {
		notificationWorker.Start(ctx)
	}

	waClient, err := wa.New(ctx, paths, daemon, db, notifier, instrument)
	if err != nil {
		log.Fatal().Err(err).Msg("initialize WhatsApp client")
	}
	defer waClient.Close()

	// The loopback range server backs media.stream: it hands players the bytes
	// of an in-progress download. It is bound before commands are registered
	// so a media.stream can never arrive before there is somewhere to point it.
	if err := waClient.StartMediaServer(); err != nil {
		log.Warn().Err(err).Msg("media streaming disabled")
	}
	defer waClient.StopMediaServer()

	newCore.register(ctx, protocolServer, daemon, db, waClient)
	if protocol.DevCommandsEnabled() {
		// Not part of PROTOCOL.md, off unless the environment asks for it. See
		// internal/protocol/dev_commands.go.
		protocol.RegisterDevCommands(protocolServer, waClient)
		log.Warn().Str("env", protocol.DevEnvVar).Msg("development commands enabled")
	}
	// Every view and command is registered above; only now do we accept
	// connections, so no client can race a half-populated handler surface.
	protocolServer.Serve(ctx)
	waClient.Start(ctx)

	log.Info().Str("socket", paths.SocketPath).Msg("whatevrd listening")

	select {
	case <-ctx.Done():
		log.Info().Msg("whatevrd shutting down")
		if err := <-protocolServer.Err(); err != nil {
			log.Fatal().Err(err).Msg("protocol server failed during shutdown")
		}
	case err := <-protocolServer.Err():
		if err != nil {
			log.Fatal().Err(err).Msg("protocol server failed")
		}
	}
}
