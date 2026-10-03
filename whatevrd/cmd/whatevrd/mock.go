//go:build whatevr_mock

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"

	"whatevrd/internal/app"
	"whatevrd/internal/capture"
	"whatevrd/internal/wamock"
)

// markerName tags a directory as one this binary created and may therefore
// wipe. Without it a mistyped --mock-dir could delete something that matters.
const markerName = ".whatevr-mock"

type mockRun struct {
	scenario wamock.Scenario
	dir      string
	opts     wamock.Options
	notify   bool
}

// mockSilencesNotifications reports whether this run should keep its hands off
// the session bus. A scenario is synthetic traffic, and a stress scenario is
// hundreds of messages a second: a desktop toast for any of it is wrong, and
// the frontends are what the mock exists to exercise anyway. --mock-notify puts
// them back for the one case where the notifier itself is the thing under test.
func mockSilencesNotifications(run *mockRun) bool {
	return run != nil && !run.notify
}

type mockFlagSet struct {
	scenario, dir, phone, control, now, capture *string
	list, keep, notify                          *bool
	seed                                        *int64
	scanDelay, histDelay, gate                  *time.Duration
	segment                                     *int
	speed                                       *float64
}

func mockFlags(zerolog.Logger) *mockFlagSet {
	return &mockFlagSet{
		scenario:  flag.String("mock", "", "run against a fake WhatsApp server using this scenario"),
		list:      flag.Bool("mock-list", false, "list mock scenarios and exit"),
		dir:       flag.String("mock-dir", "", "scratch directory for mock state (default: a per-scenario dir under XDG_RUNTIME_DIR)"),
		seed:      flag.Int64("mock-seed", 1, "seed for every key and identifier the mock generates"),
		scanDelay: flag.Duration("mock-scan-delay", 0, "how long a published QR sits unscanned before the mock phone pairs"),
		phone:     flag.String("mock-phone", "", "phone number the mock account answers as"),
		keep:      flag.Bool("mock-keep", false, "keep existing mock state instead of starting fresh"),
		histDelay: flag.Duration("mock-history-delay", 0, "how long between history sync chunks, to make the sync view watchable"),
		control:   flag.String("mock-control", "", "bind a control socket here for the quiescence barrier"),
		now:       flag.String("mock-now", "", "pin the clock scenario timestamps hang off, as RFC3339, for reproducible frames"),
		notify:    flag.Bool("mock-notify", false, "let a mock run raise desktop notifications"),
		capture:   flag.String("mock-capture", "", "replay this capture (a name or a path) instead of a scenario"),
		segment:   flag.Int("mock-segment", 1, "which segment of --mock-capture this run plays"),
		speed:     flag.Float64("mock-speed", 0, "replay pace against the recorded clock, 1 is real time, 0 as fast as the gates allow"),
		gate:      flag.Duration("mock-gate", 5*time.Second, "how long a replayed push waits for the client to catch up"),
	}
}

func mockScenario(run *mockRun) string {
	if run == nil {
		return ""
	}
	return run.opts.Scenario
}

// mockPrepare acts on the mock flags and, in mock mode, repoints the XDG
// directories at a scratch tree. It has to run before app.ResolvePaths, so the
// real account database is never opened by a mock daemon.
func mockPrepare(log zerolog.Logger, f *mockFlagSet) *mockRun {
	scenario, list, dir, seed, scanDelay, phone, keep, histDelay, control, now, notify :=
		f.scenario, f.list, f.dir, f.seed, f.scanDelay, f.phone, f.keep, f.histDelay, f.control, f.now, f.notify

	if *list {
		for _, s := range wamock.List() {
			fmt.Printf("%-16s %s\n", s.Name, s.Description)
		}
		os.Exit(0)
	}
	var capturePath string
	if *f.capture != "" {
		if *scenario != "" {
			log.Fatal().Msg("--mock and --mock-capture are two different runs, pick one")
		}
		// names resolve under the real state dir, before it moves
		stateHome, err := app.StateHome()
		if err != nil {
			log.Fatal().Err(err).Msg("resolve state dir")
		}
		if capturePath, err = capture.Resolve(*f.capture, stateHome); err != nil {
			log.Fatal().Err(err).Msg("resolve --mock-capture")
		}
		*scenario = wamock.RecordedScenario
	}
	if *scenario == "" {
		return nil
	}

	found, ok := wamock.Lookup(*scenario)
	if !ok {
		log.Fatal().Str("scenario", *scenario).Msg("unknown mock scenario (try --mock-list)")
	}
	if found.Name == wamock.RecordedScenario && capturePath == "" {
		log.Fatal().Msg("the recorded scenario plays a capture, use --mock-capture")
	}

	root := *dir
	if root == "" {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			log.Fatal().Msg("XDG_RUNTIME_DIR is unset; pass --mock-dir")
		}
		root = filepath.Join(runtimeDir, "whatevr-mock", *scenario)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		log.Fatal().Err(err).Msg("resolve --mock-dir")
	}
	if err := prepareMockDir(root, *keep); err != nil {
		log.Fatal().Err(err).Msg("prepare mock dir")
	}

	// Every XDG base moves, so the socket, the lock, the app database, the
	// whatsmeow session and the run logs all land inside the scratch tree. Two
	// mock runs of different scenarios cannot collide, and neither can touch a
	// real account.
	for env, sub := range map[string]string{
		"XDG_RUNTIME_DIR": "run",
		"XDG_DATA_HOME":   "data",
		"XDG_CACHE_HOME":  "cache",
		"XDG_STATE_HOME":  "state",
	} {
		path := filepath.Join(root, sub)
		if err := os.MkdirAll(path, 0o700); err != nil {
			log.Fatal().Err(err).Str("path", path).Msg("create mock dir")
		}
		if err := os.Setenv(env, path); err != nil {
			log.Fatal().Err(err).Str("env", env).Msg("set mock env")
		}
	}

	opts := wamock.Options{
		Seed:         *seed,
		Scenario:     found.Name,
		AccountPhone: *phone,
		ScanDelay:    *scanDelay,
		HistoryDelay: *histDelay,
		Control:      *control,
		Capture:      capturePath,
		Segment:      *f.segment,
		Speed:        *f.speed,
		Gate:         *f.gate,
	}
	if *now != "" {
		at, err := time.Parse(time.RFC3339, *now)
		if err != nil {
			log.Fatal().Err(err).Msg("parse --mock-now")
		}
		opts.Now = at
	}
	return &mockRun{scenario: found, dir: root, opts: opts, notify: *notify}
}

// prepareMockDir makes root exist and, unless asked to keep it, empty. It only
// ever removes a directory carrying our marker file.
func prepareMockDir(root string, keep bool) error {
	info, err := os.Stat(root)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(root, 0o700); err != nil {
			return err
		}
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("%s exists and is not a directory", root)
	case keep:
		// Nothing to do, the caller wants whatever is there.
	default:
		if _, err := os.Stat(filepath.Join(root, markerName)); err != nil {
			return fmt.Errorf("%s was not created by --mock (no %s); refusing to clear it", root, markerName)
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		if err := os.MkdirAll(root, 0o700); err != nil {
			return err
		}
	}
	marker := filepath.Join(root, markerName)
	return os.WriteFile(marker, []byte("whatevrd --mock scratch directory\n"), 0o600)
}

// mockStart brings up the fake server. It must run before wa.New, because
// whatsmeow snapshots http.DefaultTransport when it builds its client.
func mockStart(ctx context.Context, run *mockRun, daemon *app.Daemon) (func(), error) {
	if run == nil {
		return func() {}, nil
	}
	run.opts.Login = daemon
	run.opts.Socket = daemon.Status().Paths.SocketPath
	srv, err := wamock.New(ctx, run.opts)
	if err != nil {
		return nil, err
	}
	if err := srv.Start(ctx); err != nil {
		return nil, err
	}
	if err := srv.StartControl(ctx, run.opts.Control); err != nil {
		return nil, err
	}
	zerolog.Ctx(ctx).Info().Str("scenario", run.scenario.Name).Str("dir", run.dir).Msg("mock mode")
	return func() { _ = srv.Close() }, nil
}

// mockTime is the clocks of a mock run: the wall the control socket can
// jump, and the recording's time during a replay.
func mockTime(run *mockRun) *mockClocks {
	if run == nil {
		return nil
	}
	return &mockClocks{wall: wamock.Wall, stamp: wamock.Stamp}
}
