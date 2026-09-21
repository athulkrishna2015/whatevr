//go:build whatevr_mock

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"whatevrd/internal/app"
	"whatevrd/internal/wamock"
)

// markerName tags a directory as one this binary created and may therefore
// wipe. Without it a mistyped --mock-dir could delete something that matters.
const markerName = ".whatevr-mock"

type mockRun struct {
	scenario wamock.Scenario
	dir      string
	opts     wamock.Options
}

// mockPrepare parses the mock flags and, in mock mode, repoints the XDG
// directories at a scratch tree. It has to run before app.ResolvePaths, so the
// real account database is never opened by a mock daemon.
func mockPrepare() *mockRun {
	var (
		scenario  = flag.String("mock", "", "run against a fake WhatsApp server using this scenario")
		list      = flag.Bool("mock-list", false, "list mock scenarios and exit")
		dir       = flag.String("mock-dir", "", "scratch directory for mock state (default: a per-scenario dir under XDG_RUNTIME_DIR)")
		seed      = flag.Int64("mock-seed", 1, "seed for every key and identifier the mock generates")
		scanDelay = flag.Duration("mock-scan-delay", 0, "how long a published QR sits unscanned before the mock phone pairs")
		phone     = flag.String("mock-phone", "", "phone number the mock account answers as")
		keep      = flag.Bool("mock-keep", false, "keep existing mock state instead of starting fresh")
		histDelay = flag.Duration("mock-history-delay", 0, "how long between history sync chunks, to make the sync view watchable")
	)
	flag.Parse()

	if *list {
		for _, s := range wamock.List() {
			fmt.Printf("%-16s %s\n", s.Name, s.Description)
		}
		os.Exit(0)
	}
	if *scenario == "" {
		return nil
	}

	found, ok := wamock.Lookup(*scenario)
	if !ok {
		log.Fatalf("unknown mock scenario %q (try --mock-list)", *scenario)
	}

	root := *dir
	if root == "" {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			log.Fatal("XDG_RUNTIME_DIR is unset; pass --mock-dir")
		}
		root = filepath.Join(runtimeDir, "whatevr-mock", *scenario)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		log.Fatalf("resolve --mock-dir: %v", err)
	}
	if err := prepareMockDir(root, *keep); err != nil {
		log.Fatalf("prepare mock dir: %v", err)
	}

	// The whole XDG triple moves, so the socket, the lock, the app database and
	// the whatsmeow session all land inside the scratch tree. Two mock runs of
	// different scenarios cannot collide, and neither can touch a real account.
	for env, sub := range map[string]string{
		"XDG_RUNTIME_DIR": "run",
		"XDG_DATA_HOME":   "data",
		"XDG_CACHE_HOME":  "cache",
	} {
		path := filepath.Join(root, sub)
		if err := os.MkdirAll(path, 0o700); err != nil {
			log.Fatalf("create %s: %v", path, err)
		}
		if err := os.Setenv(env, path); err != nil {
			log.Fatalf("set %s: %v", env, err)
		}
	}

	opts := wamock.Options{
		Seed:         *seed,
		Scenario:     found.Name,
		AccountPhone: *phone,
		ScanDelay:    *scanDelay,
		HistoryDelay: *histDelay,
	}
	return &mockRun{scenario: found, dir: root, opts: opts}
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
	srv, err := wamock.New(run.opts)
	if err != nil {
		return nil, err
	}
	if err := srv.Start(ctx); err != nil {
		return nil, err
	}
	log.Printf("mock mode: scenario %q, state in %s", run.scenario.Name, run.dir)
	return func() { _ = srv.Close() }, nil
}
