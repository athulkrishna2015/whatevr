package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"

	"whatevrd/internal/app"
	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

// rederiveCommand is `whatevrd rederive`. it never touches the account.
func rederiveCommand() *cli.Command {
	return &cli.Command{
		Name:     "rederive",
		Usage:    "rebuild every derived table from the log",
		Category: "debug",
		Description: `drops every table the core derives and folds its whole log again, as a
fold version change does at startup. the log itself is not touched. refuses
while a daemon or anything else has the store open. exits 1 when any input
failed to fold.`,
		Flags: []cli.Flag{&cli.StringFlag{Name: "data", Usage: "directory holding whatevr.db (default: the daemon's data directory)"}},
		Action: func(_ context.Context, c *cli.Command) error {
			if c.Args().Present() {
				return usage(c, "rederive takes no arguments")
			}
			stdout, stderr := outputs(c)
			return code(runRederive(c.String("data"), stdout, stderr))
		},
	}
}

func runRederive(data string, stdout, stderr io.Writer) int {
	dir := data
	if dir == "" {
		paths, err := app.ResolvePaths()
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd rederive: %v\n", err)
			return 1
		}
		dir = paths.DataDir
	}
	path := filepath.Join(dir, "whatevr.db")
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
			if len(failures) > 0 {
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
