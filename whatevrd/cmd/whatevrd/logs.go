package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"

	"github.com/codelif/whatevr/platform"
	"whatevrd/internal/logx"
)

// logsCommand is `whatevrd logs`, it never touches the daemon or the account.
func logsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "print a run's log, the newest run by default",
		ArgsUsage: "[RUN] [field=value ...] | list",
		Description: `RUN is a run id prefix. every field=value has to match a line's field for
it to print. list prints every run.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Usage: "log directory (default: platform log directory)"},
			&cli.BoolFlag{Name: "f", Usage: "keep printing new lines"},
			&cli.StringFlag{Name: "level", Value: "trace", Usage: "lowest level to print"},
			&cli.BoolFlag{Name: "json", Usage: "print the stored json lines"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			stdout, stderr := outputs(c)
			return code(runLogs(c.Args().Slice(), c.String("dir"), c.String("level"), c.Bool("f"), c.Bool("json"), stdout, stderr))
		},
	}
}

func runLogs(positional []string, dir, level string, follow, asJSON bool, stdout, stderr io.Writer) int {
	minLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		fmt.Fprintf(stderr, "whatevrd logs: %v\n", err)
		return 1
	}
	opts := logx.ReadOptions{MinLevel: minLevel, JSON: asJSON, Follow: follow}
	var runID string
	list := false
	for _, a := range positional {
		if f, ok := logx.ParseFilter(a); ok {
			opts.Filters = append(opts.Filters, f)
			continue
		}
		switch {
		case a == "list" && !list && runID == "":
			list = true
		case runID == "" && !list:
			runID = a
		default:
			fmt.Fprintf(stderr, "whatevrd logs: unexpected %q\n", a)
			return 1
		}
	}

	if dir == "" {
		logDir, err := platform.LogDir()
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd logs: %v\n", err)
			return 1
		}
		dir = logDir
	}
	runs, err := logx.ListRuns(dir)
	if err != nil {
		fmt.Fprintf(stderr, "whatevrd logs: %v\n", err)
		return 1
	}
	if list {
		for _, r := range runs {
			var size int64
			for _, p := range r.Parts {
				if info, err := os.Stat(p); err == nil {
					size += info.Size()
				}
			}
			fmt.Fprintf(stdout, "%s  %s  %d part(s)  %s\n", r.ID, r.Start.Local().Format("2006-01-02 15:04:05"), len(r.Parts), humanSize(size))
		}
		return 0
	}
	run, ok := logx.FindRun(runs, runID)
	if !ok {
		if runID == "" {
			fmt.Fprintf(stderr, "whatevrd logs: no runs in %s\n", dir)
		} else {
			fmt.Fprintf(stderr, "whatevrd logs: no single run matches %q (try whatevrd logs list)\n", runID)
		}
		return 1
	}
	if f, ok := stdout.(*os.File); ok {
		opts.Color = isatty.IsTerminal(f.Fd())
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := logx.Read(ctx, run, opts, stdout); err != nil && !strings.Contains(err.Error(), "broken pipe") {
		fmt.Fprintf(stderr, "whatevrd logs: %v\n", err)
		return 1
	}
	return 0
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
