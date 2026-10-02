package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRun(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func logsDir(t *testing.T) string {
	dir := t.TempDir()
	writeRun(t, dir, "whatevrd-2026-10-01T10:00:00+05:30-aaaa0001.jsonl",
		`{"level":"info","run":"aaaa0001","time":"2026-10-01T10:00:01.000+05:30","message":"old run"}`)
	writeRun(t, dir, "whatevrd-2026-10-02T10:00:00+05:30-bbbb0002.jsonl",
		`{"level":"info","run":"bbbb0002","chat":"1@s.whatsapp.net","msg":"m1","time":"2026-10-02T10:00:01.000+05:30","message":"stored text message"}`,
		`{"level":"debug","run":"bbbb0002","chat":"1@s.whatsapp.net","time":"2026-10-02T10:00:02.000+05:30","message":"recompute diff"}`,
		`{"level":"warn","run":"bbbb0002","chat":"2@s.whatsapp.net","time":"2026-10-02T10:00:03.000+05:30","message":"other chat"}`)
	return dir
}

func runLogsArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runLogs(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestLogsNewestRunWithFiltersAndFlagsAnywhere(t *testing.T) {
	dir := logsDir(t)
	code, out, errs := runLogsArgs(t, "chat=1@s.whatsapp.net", "--dir", dir, "--json", "--level", "info")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, `"msg":"m1"`) {
		t.Fatalf("got %q", out)
	}
}

func TestLogsPicksARunByPrefix(t *testing.T) {
	dir := logsDir(t)
	code, out, _ := runLogsArgs(t, "--dir", dir, "aaaa")
	if code != 0 || !strings.Contains(out, "old run") || strings.Contains(out, "other chat") {
		t.Fatalf("exit %d, got %q", code, out)
	}
	if code, _, errs := runLogsArgs(t, "--dir", dir, "cccc"); code != 1 || !strings.Contains(errs, "no single run") {
		t.Fatalf("exit %d, %q", code, errs)
	}
}

func TestLogsList(t *testing.T) {
	dir := logsDir(t)
	code, out, _ := runLogsArgs(t, "--dir", dir, "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 || !strings.HasPrefix(lines[0], "aaaa0001") || !strings.HasPrefix(lines[1], "bbbb0002") {
		t.Fatalf("exit %d, got %q", code, out)
	}
}

func TestLogsRejectsJunk(t *testing.T) {
	dir := logsDir(t)
	if code, _, _ := runLogsArgs(t, "--dir", dir, "aaaa", "bbbb"); code != 2 {
		t.Fatalf("two runs: exit %d", code)
	}
	if code, _, _ := runLogsArgs(t, "--dir", dir, "--level", "loud"); code != 2 {
		t.Fatalf("bad level: exit %d", code)
	}
	if code, _, _ := runLogsArgs(t, "--nope"); code != 2 {
		t.Fatalf("unknown flag: exit %d", code)
	}
}
