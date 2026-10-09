package logx

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type Filter struct{ Field, Value string }

// ParseFilter takes field=value.
func ParseFilter(s string) (Filter, bool) {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return Filter{}, false
	}
	return Filter{k, v}, true
}

type ReadOptions struct {
	Filters  []Filter
	MinLevel zerolog.Level
	// JSON prints the lines as stored instead of console text.
	JSON   bool
	Color  bool
	Follow bool
	// Poll is how often a followed file is checked, 250ms when zero.
	Poll time.Duration
}

// Read prints a run's matching lines, every part in order, then keeps
// following the live part when asked until ctx ends.
func Read(ctx context.Context, run RunFiles, opts ReadOptions, out io.Writer) error {
	p := printer{opts: opts, out: out}
	if !opts.JSON {
		p.console = zerolog.ConsoleWriter{
			Out:           out,
			NoColor:       !opts.Color,
			TimeFormat:    "2006-01-02 15:04:05.000",
			FieldsExclude: []string{"run"},
		}
	}
	for i, part := range run.Parts {
		live := i == len(run.Parts)-1 && !strings.HasSuffix(part, ".gz")
		if live && opts.Follow {
			return p.follow(ctx, part)
		}
		if err := p.readFile(part); err != nil {
			return err
		}
	}
	return nil
}

type printer struct {
	opts    ReadOptions
	out     io.Writer
	console zerolog.ConsoleWriter
}

func (p *printer) readFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		defer gz.Close()
		r = gz
	}
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			p.line(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// follow reads path forever. lumberjack rotates by renaming the live file and
// making a new one under the same name, so a changed inode means drain the
// old handle and reopen.
func (p *printer) follow(ctx context.Context, path string) error {
	poll := p.opts.Poll
	if poll == 0 {
		poll = 250 * time.Millisecond
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { f.Close() }()
	br := bufio.NewReaderSize(f, 64*1024)
	var pending []byte
	for {
		chunk, err := br.ReadBytes('\n')
		pending = append(pending, chunk...)
		if err == nil {
			p.line(pending)
			pending = pending[:0]
			continue
		}
		if err != io.EOF {
			return err
		}
		if !sameFile(f, path) {
			next, err := os.Open(path)
			if err == nil {
				// whatever is left in the old handle was read above
				f.Close()
				f, br = next, bufio.NewReaderSize(next, 64*1024)
				continue
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(poll):
		}
	}
}

func sameFile(f *os.File, path string) bool {
	a, err := f.Stat()
	if err != nil {
		return true
	}
	b, err := os.Stat(path)
	if err != nil {
		// mid rotation, the new file isn't there yet
		return true
	}
	return os.SameFile(a, b)
}

func (p *printer) line(line []byte) {
	line = bytes.TrimRight(line, "\n")
	if len(line) == 0 {
		return
	}
	if !p.matches(line) {
		return
	}
	if p.opts.JSON {
		p.out.Write(line)
		p.out.Write([]byte{'\n'})
		return
	}
	if _, err := p.console.Write(line); err != nil {
		// not json, show it as is
		fmt.Fprintf(p.out, "%s\n", line)
	}
}

func (p *printer) matches(line []byte) bool {
	if len(p.opts.Filters) == 0 && p.opts.MinLevel <= zerolog.TraceLevel {
		return true
	}
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(line))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		return len(p.opts.Filters) == 0
	}
	if s, ok := m[zerolog.LevelFieldName].(string); ok {
		if l, err := zerolog.ParseLevel(s); err == nil && l < p.opts.MinLevel {
			return false
		}
	}
	for _, f := range p.opts.Filters {
		v, ok := m[f.Field]
		if !ok || !matchValue(v, f.Value) {
			return false
		}
	}
	return true
}

// matchValue is v equal to want, or holding it when v is a list: a fold or
// a view diff names every message it touched in one field.
func matchValue(v any, want string) bool {
	if l, ok := v.([]any); ok {
		for _, e := range l {
			if matchValue(e, want) {
				return true
			}
		}
		return false
	}
	s, ok := v.(string)
	if !ok {
		s = fmt.Sprint(v)
	}
	return s == want
}
