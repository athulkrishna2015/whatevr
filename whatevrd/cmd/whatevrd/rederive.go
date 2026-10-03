package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"whatevrd/internal/app"
	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

const rederiveUsage = `usage: whatevrd rederive [flags]

drops every table the new core derives and folds its whole log again, as a
fold version change does at startup. the log itself is not touched. refuses
while a daemon holds the data directory.

flags:
`

// runRederive is `whatevrd rederive`. it never touches the account.
func runRederive(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("whatevrd rederive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	data := fs.String("data", "", "directory holding core.db (default: the daemon's data directory)")
	fs.Usage = func() {
		fmt.Fprint(stderr, rederiveUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return 2
	}
	dir := *data
	if dir == "" {
		paths, err := app.ResolvePaths()
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd rederive: %v\n", err)
			return 1
		}
		// the daemon folds into the same file, two writers would race
		lock, err := app.AcquireProcessLock(paths.LockPath)
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd rederive: %v\n", err)
			return 1
		}
		defer lock.Close()
		dir = paths.DataDir
	}
	path := filepath.Join(dir, "core.db")
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(stderr, "whatevrd rederive: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return rederive(ctx, path, stdout, stderr)
}

func rederive(ctx context.Context, path string, stdout, stderr io.Writer) int {
	start := time.Now()
	db, err := core.Open(ctx, path, core.Options{Domains: model.Domains(), Log: zerolog.Nop(), Rebuild: true})
	if err != nil {
		fmt.Fprintf(stderr, "whatevrd rederive: %v\n", err)
		return 1
	}
	defer db.Close()
	_, appended := db.Progress()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	done := make(chan error, 1)
	go func() { done <- db.WaitFolded(ctx, appended) }()
	for {
		select {
		case err := <-done:
			if err != nil {
				// folding resumes where it stopped at the next open
				fmt.Fprintf(stderr, "whatevrd rederive: %v\n", err)
				return 1
			}
			failures, err := db.FoldFailures(ctx)
			if err != nil {
				fmt.Fprintf(stderr, "whatevrd rederive: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "folded %d inputs in %s, %d failed\n", appended, time.Since(start).Round(time.Millisecond), len(failures))
			for seq, e := range failures {
				fmt.Fprintf(stdout, "  input %d: %s\n", seq, e)
			}
			if h := db.Health(); h.Fold != nil {
				fmt.Fprintf(stderr, "whatevrd rederive: folding failed: %s\n", h.Fold.Error)
				return 1
			}
			return 0
		case <-tick.C:
			folded, _ := db.Progress()
			fmt.Fprintf(stderr, "folded %d of %d\n", folded, appended)
		case <-ctx.Done():
			fmt.Fprintln(stderr, "whatevrd rederive: stopped, folding carries on at the next open")
			return 1
		}
	}
}
