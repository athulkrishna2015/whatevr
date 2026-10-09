// package logx is whatevrd's logging: one zerolog root per run, written as
// JSONL to a run file and to stderr (native journald under systemd), carried
// to code through ctx with zerolog.Ctx.
package logx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/journal"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/journald"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// TimeFormat is local time with the offset, to the millisecond.
	TimeFormat = "2006-01-02T15:04:05.000Z07:00"

	FileLevelEnv   = "WHATEVRD_LOG_LEVEL"
	StderrLevelEnv = "WHATEVRD_LOG_STDERR"

	DefaultFileLevel   = zerolog.InfoLevel
	DefaultStderrLevel = zerolog.WarnLevel
	// KeepRuns counts whole runs, every part of a kept run stays.
	KeepRuns = 10
	// PartMB is where lumberjack starts the next part of the same run.
	PartMB = 1024
)

func init() {
	zerolog.TimeFieldFormat = TimeFormat
}

type Config struct {
	// Dir holds the run files. empty means stderr only.
	Dir         string
	FileLevel   zerolog.Level
	StderrLevel zerolog.Level
	Stderr      *os.File
}

type Run struct {
	ID     string
	Start  time.Time
	Path   string
	Logger zerolog.Logger
	file   *lumberjack.Logger
}

// LevelsFromEnv reads the two level variables, defaults when unset.
func LevelsFromEnv() (file, stderr zerolog.Level, err error) {
	if file, err = envLevel(FileLevelEnv, DefaultFileLevel); err != nil {
		return
	}
	stderr, err = envLevel(StderrLevelEnv, DefaultStderrLevel)
	return
}

func envLevel(name string, def zerolog.Level) (zerolog.Level, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def, nil
	}
	l, err := zerolog.ParseLevel(strings.ToLower(v))
	if err != nil || l == zerolog.NoLevel {
		return def, fmt.Errorf("%s=%q is not a log level (trace, debug, info, warn, error)", name, v)
	}
	return l, nil
}

// Start opens a new run: prunes old runs, picks the id and makes the root
// logger the default for zerolog.Ctx and the stdlib log package.
func Start(cfg Config) (*Run, error) {
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	id, err := newRunID()
	if err != nil {
		return nil, err
	}
	r := &Run{ID: id, Start: time.Now()}

	writers := []io.Writer{&zerolog.FilteredLevelWriter{Writer: zerolog.LevelWriterAdapter{Writer: stderrWriter(cfg.Stderr)}, Level: cfg.StderrLevel}}
	min := cfg.StderrLevel
	if cfg.Dir != "" {
		if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
			return nil, err
		}
		if err := pruneRuns(cfg.Dir, KeepRuns-1); err != nil {
			return nil, fmt.Errorf("prune old log runs: %w", err)
		}
		r.Path = runFilePath(cfg.Dir, r.Start, id)
		r.file = &lumberjack.Logger{Filename: r.Path, MaxSize: PartMB, Compress: true}
		writers = append(writers, &zerolog.FilteredLevelWriter{Writer: zerolog.LevelWriterAdapter{Writer: r.file}, Level: cfg.FileLevel})
		min = minLevel(min, cfg.FileLevel)
	}
	r.Logger = zerolog.New(zerolog.MultiLevelWriter(writers...)).Level(min).With().Timestamp().Str("run", id).Logger()
	makeDefault(r.Logger)
	return r, nil
}

func (r *Run) Close() error {
	if r.file == nil {
		return nil
	}
	return r.file.Close()
}

// Console is a stderr-only logger for binaries without run files.
func Console(w *os.File, level zerolog.Level) zerolog.Logger {
	l := zerolog.New(consoleWriter(w)).Level(level).With().Timestamp().Logger()
	makeDefault(l)
	return l
}

func makeDefault(l zerolog.Logger) {
	zerolog.DefaultContextLogger = &l
	std := l.With().Str("module", "stdlog").Logger()
	log.SetFlags(0)
	log.SetOutput(stdlogWriter{&std})
}

// WithContext puts l in ctx, the way every layer adds its fields.
func WithContext(ctx context.Context, l zerolog.Logger) context.Context {
	return l.WithContext(ctx)
}

func stderrWriter(f *os.File) io.Writer {
	if ok, err := journal.StderrIsJournalStream(); err == nil && ok && f == os.Stderr {
		return journald.NewJournalDWriter()
	}
	return consoleWriter(f)
}

func consoleWriter(f *os.File) io.Writer {
	return zerolog.ConsoleWriter{
		Out:        f,
		NoColor:    !isatty.IsTerminal(f.Fd()),
		TimeFormat: "15:04:05.000",
	}
}

// stdlogWriter takes whatever a library prints through package log.
type stdlogWriter struct{ l *zerolog.Logger }

func (w stdlogWriter) Write(p []byte) (int, error) {
	w.l.Info().Msg(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func newRunID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func minLevel(a, b zerolog.Level) zerolog.Level {
	if a < b {
		return a
	}
	return b
}
