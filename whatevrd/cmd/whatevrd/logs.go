package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"

	"whatevrd/internal/app"
	"whatevrd/internal/logx"
)

const logsUsage = `usage: whatevrd logs [flags] [RUN] [field=value ...]
       whatevrd logs [flags] list

prints a run's log, the newest run when RUN (an id prefix) is left out.
every field=value has to match a line's field for it to print.

flags:
`

// runLogs is `whatevrd logs`, it never touches the daemon or the account.
func runLogs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("whatevrd logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "log directory (default: $XDG_STATE_HOME/whatevr/logs)")
	follow := fs.Bool("f", false, "keep printing new lines")
	level := fs.String("level", "trace", "lowest level to print")
	asJSON := fs.Bool("json", false, "print the stored json lines")
	fs.Usage = func() {
		fmt.Fprint(stderr, logsUsage)
		fs.PrintDefaults()
	}

	// flags may come after positionals
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}

	minLevel, err := zerolog.ParseLevel(*level)
	if err != nil {
		fmt.Fprintf(stderr, "whatevrd logs: %v\n", err)
		return 2
	}
	opts := logx.ReadOptions{MinLevel: minLevel, JSON: *asJSON, Follow: *follow}
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
			return 2
		}
	}

	if *dir == "" {
		paths, err := app.ResolvePaths()
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd logs: %v\n", err)
			return 1
		}
		*dir = paths.LogDir
	}
	runs, err := logx.ListRuns(*dir)
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
			fmt.Fprintf(stderr, "whatevrd logs: no runs in %s\n", *dir)
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
