package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func startRun(t *testing.T, dir string, file, stderr zerolog.Level) (*Run, *os.File) {
	t.Helper()
	errFile, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Start(Config{Dir: dir, FileLevel: file, StderrLevel: stderr, Stderr: errFile})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.Close()
		errFile.Close()
		zerolog.DefaultContextLogger = nil
	})
	return r, errFile
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("not json: %s", line)
		}
		out = append(out, m)
	}
	return out
}

func TestRunWritesJSONWithRunAndLocalTime(t *testing.T) {
	dir := t.TempDir()
	r, _ := startRun(t, dir, zerolog.InfoLevel, zerolog.Disabled)
	r.Logger.Info().Str("chat", "c1").Msg("hello")
	r.Logger.Debug().Msg("below the file level")

	if want := filepath.Join(dir, "whatevrd-"+r.Start.Format(time.RFC3339)+"-"+r.ID+".jsonl"); r.Path != want {
		t.Fatalf("path %s, want %s", r.Path, want)
	}
	lines := readLines(t, r.Path)
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %v", lines)
	}
	l := lines[0]
	if l["run"] != r.ID || l["chat"] != "c1" || l["message"] != "hello" || l["level"] != "info" {
		t.Fatalf("bad line %v", l)
	}
	ts, err := time.Parse(TimeFormat, l["time"].(string))
	if err != nil {
		t.Fatalf("time %q not in %s: %v", l["time"], TimeFormat, err)
	}
	if _, off := ts.Zone(); func() int { _, o := time.Now().Zone(); return o }() != off {
		t.Fatalf("time %q is not local", l["time"])
	}
	if len(r.ID) != 8 {
		t.Fatalf("run id %q is not 8 hex", r.ID)
	}
}

func TestStderrAndFileLevelsAreSeparate(t *testing.T) {
	dir := t.TempDir()
	r, errFile := startRun(t, dir, zerolog.InfoLevel, zerolog.WarnLevel)
	r.Logger.Info().Msg("file only")
	r.Logger.Warn().Msg("both")

	if got := len(readLines(t, r.Path)); got != 2 {
		t.Fatalf("file has %d lines, want 2", got)
	}
	data, _ := os.ReadFile(errFile.Name())
	if strings.Contains(string(data), "file only") || !strings.Contains(string(data), "both") {
		t.Fatalf("stderr got %q", data)
	}
}

func TestStartMakesTheRootTheCtxDefault(t *testing.T) {
	r, _ := startRun(t, t.TempDir(), zerolog.InfoLevel, zerolog.Disabled)
	zerolog.Ctx(context.Background()).Info().Msg("bare ctx")
	ctx := WithContext(context.Background(), r.Logger.With().Str("conn", "3").Logger())
	zerolog.Ctx(ctx).Info().Msg("with fields")
	lines := readLines(t, r.Path)
	if len(lines) != 2 || lines[0]["run"] != r.ID || lines[1]["conn"] != "3" {
		t.Fatalf("got %v", lines)
	}
}

func TestLevelsFromEnv(t *testing.T) {
	t.Setenv(FileLevelEnv, "")
	t.Setenv(StderrLevelEnv, "")
	if f, s, err := LevelsFromEnv(); err != nil || f != zerolog.InfoLevel || s != zerolog.WarnLevel {
		t.Fatalf("defaults: %v %v %v", f, s, err)
	}
	t.Setenv(FileLevelEnv, "DEBUG")
	t.Setenv(StderrLevelEnv, "error")
	if f, s, err := LevelsFromEnv(); err != nil || f != zerolog.DebugLevel || s != zerolog.ErrorLevel {
		t.Fatalf("set: %v %v %v", f, s, err)
	}
	t.Setenv(FileLevelEnv, "loud")
	if _, _, err := LevelsFromEnv(); err == nil {
		t.Fatal("expected an error for a bad level")
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListRunsGroupsPartsAndPrunesWholeRuns(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.FixedZone("IST", 5*3600+1800))
	var ids []string
	for i := range 12 {
		id := strings.Repeat(string(rune('a'+i%6)), 8)
		if i >= 6 {
			id = id[:7] + "0"
		}
		ids = append(ids, id)
		live := runFilePath(dir, base.Add(time.Duration(i)*time.Hour), id)
		touch(t, live)
		if i == 11 {
			stem := strings.TrimSuffix(live, ".jsonl")
			touch(t, stem+"-2026-10-02T20-00-00.000.jsonl.gz")
			touch(t, stem+"-2026-10-02T21-00-00.000.jsonl")
			// gzip still running: both copies on disk
			touch(t, stem+"-2026-10-02T21-00-00.000.jsonl.gz")
		}
	}
	touch(t, filepath.Join(dir, "notes.txt"))

	runs, err := ListRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 12 || runs[0].ID != ids[0] || runs[11].ID != ids[11] {
		t.Fatalf("got %d runs: %v", len(runs), runs)
	}
	last := runs[11].Parts
	if len(last) != 3 || !strings.HasSuffix(last[0], "20-00-00.000.jsonl.gz") ||
		!strings.HasSuffix(last[1], "21-00-00.000.jsonl") || !strings.HasSuffix(last[2], ids[11]+".jsonl") {
		t.Fatalf("parts out of order: %v", last)
	}

	if err := pruneRuns(dir, 9); err != nil {
		t.Fatal(err)
	}
	runs, _ = ListRuns(dir)
	if len(runs) != 9 || runs[0].ID != ids[3] || len(runs[8].Parts) != 3 {
		t.Fatalf("after prune: %v", runs)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("prune touched a file that isn't ours")
	}
}

func TestStartKeepsTheNewestTenRuns(t *testing.T) {
	dir := t.TempDir()
	for i := range 12 {
		touch(t, runFilePath(dir, time.Now().Add(-time.Duration(12-i)*time.Hour), fmt.Sprintf("%08x", i)))
	}
	r, _ := startRun(t, dir, zerolog.InfoLevel, zerolog.Disabled)
	r.Logger.Info().Msg("x")
	runs, _ := ListRuns(dir)
	if len(runs) != KeepRuns || runs[len(runs)-1].ID != r.ID {
		t.Fatalf("got %d runs, newest %s", len(runs), runs[len(runs)-1].ID)
	}
}

func TestFindRun(t *testing.T) {
	runs := []RunFiles{{ID: "3fa9c2e1"}, {ID: "3fb00000"}, {ID: "77777777"}}
	if r, ok := FindRun(runs, ""); !ok || r.ID != "77777777" {
		t.Fatal("empty should be the newest")
	}
	if r, ok := FindRun(runs, "3fa"); !ok || r.ID != "3fa9c2e1" {
		t.Fatal("prefix")
	}
	if _, ok := FindRun(runs, "3f"); ok {
		t.Fatal("an ambiguous prefix must not pick one")
	}
}

func TestReadFiltersAcrossRotatedGzippedParts(t *testing.T) {
	dir := t.TempDir()
	r, _ := startRun(t, dir, zerolog.DebugLevel, zerolog.Disabled)
	r.Logger.Info().Str("msg", "A").Msg("first")
	if err := r.file.Rotate(); err != nil {
		t.Fatal(err)
	}
	r.Logger.Debug().Str("msg", "A").Msg("second")
	r.Logger.Info().Str("msg", "B").Msg("other")
	r.Logger.Warn().Int("n", 3).Str("msg", "A").Msg("third")

	// lumberjack gzips in the background
	deadline := time.Now().Add(5 * time.Second)
	var run RunFiles
	for {
		runs, _ := ListRuns(dir)
		run = runs[0]
		if len(run.Parts) == 2 && strings.HasSuffix(run.Parts[0], ".gz") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rotated part never gzipped: %v", run.Parts)
		}
		time.Sleep(10 * time.Millisecond)
	}

	var out bytes.Buffer
	if err := Read(t.Context(), run, ReadOptions{Filters: []Filter{{"msg", "A"}}, MinLevel: zerolog.TraceLevel, JSON: true}, &out); err != nil {
		t.Fatal(err)
	}
	got := strings.Count(out.String(), "\n")
	if got != 3 || !strings.Contains(out.String(), "first") || strings.Contains(out.String(), "other") {
		t.Fatalf("got %q", out.String())
	}

	out.Reset()
	Read(t.Context(), run, ReadOptions{Filters: []Filter{{"msg", "A"}, {"n", "3"}}, MinLevel: zerolog.InfoLevel}, &out)
	if s := out.String(); !strings.Contains(s, "third") || strings.Contains(s, "second") || strings.Contains(s, r.ID) {
		t.Fatalf("console got %q", s)
	}
}

// a fold or a view diff lists every message it touched; one of them is
// enough to match
func TestReadFilterMatchesAListElement(t *testing.T) {
	dir := t.TempDir()
	r, _ := startRun(t, dir, zerolog.DebugLevel, zerolog.Disabled)
	r.Logger.Debug().Strs("msg", []string{"c:A", "c:B"}).Msg("folded")
	r.Logger.Debug().Strs("msg", []string{"c:C"}).Msg("other")
	r.Logger.Info().Str("msg", "c:B").Msg("single")
	runs, _ := ListRuns(dir)
	var out bytes.Buffer
	if err := Read(t.Context(), runs[0], ReadOptions{Filters: []Filter{{"msg", "c:B"}}, MinLevel: zerolog.TraceLevel, JSON: true}, &out); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.Count(s, "\n") != 2 || !strings.Contains(s, "folded") || strings.Contains(s, "other") {
		t.Fatalf("got %q", s)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestFollowCrossesARotation(t *testing.T) {
	dir := t.TempDir()
	r, _ := startRun(t, dir, zerolog.InfoLevel, zerolog.Disabled)
	r.Logger.Info().Msg("before")
	runs, _ := ListRuns(dir)

	ctx, cancel := context.WithCancel(t.Context())
	out := &lockedBuffer{}
	done := make(chan error)
	go func() {
		done <- Read(ctx, runs[0], ReadOptions{JSON: true, Follow: true, Poll: 5 * time.Millisecond}, out)
	}()

	waitFor := func(s string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(out.String(), s) {
			if time.Now().After(deadline) {
				t.Fatalf("never saw %q in %q", s, out.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor("before")
	r.Logger.Info().Msg("old part tail")
	if err := r.file.Rotate(); err != nil {
		t.Fatal(err)
	}
	r.Logger.Info().Msg("new part")
	waitFor("new part")
	waitFor("old part tail")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
