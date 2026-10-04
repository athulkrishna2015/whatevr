//go:build whatevr_capture

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codelif/whatevr/platform"
	"github.com/rs/zerolog"
)

func TestRealAccountCaptureGetsItsOwnDevice(t *testing.T) {
	state, runtime := t.TempDir(), t.TempDir()
	t.Setenv(platform.SocketEnv, "/production/account.sock")
	t.Setenv("XDG_CONFIG_HOME", "/production/config")
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("XDG_DATA_HOME", "/nowhere/data")
	t.Setenv("XDG_CACHE_HOME", "/nowhere/cache")
	name, guard := "probe", false
	run := capturePrepare(zerolog.Nop(), &captureFlagSet{name: &name, guard: &guard}, "")
	if run == nil || !run.guard || run.dir != filepath.Join(state, "whatevr", "captures", "probe") {
		t.Fatalf("got %+v", run)
	}
	home := filepath.Join(run.dir, "home")
	for env, want := range map[string]string{
		"XDG_DATA_HOME":   filepath.Join(home, "data"),
		"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
		"XDG_STATE_HOME":  filepath.Join(home, "state"),
		"XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_RUNTIME_DIR": filepath.Join(runtime, "whatevr-capture", "probe"),
	} {
		if got := os.Getenv(env); got != want {
			t.Errorf("%s=%s, want %s", env, got, want)
		}
	}
	paths, err := platform.SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if paths == "/production/account.sock" {
		t.Fatal("capture reused the production socket")
	}
}

func TestMockCaptureStaysInTheMockAndUnguarded(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_DATA_HOME", "/mock/data")
	name, guard := "probe", false
	run := capturePrepare(zerolog.Nop(), &captureFlagSet{name: &name, guard: &guard}, "busy")
	if run == nil || run.guard || run.mock != "busy" {
		t.Fatalf("got %+v", run)
	}
	if os.Getenv("XDG_DATA_HOME") != "/mock/data" {
		t.Fatal("a mock capture moved the mock's dirs")
	}
}

func TestSendGuardAloneRecordsNothing(t *testing.T) {
	name, guard := "", true
	run := capturePrepare(zerolog.Nop(), &captureFlagSet{name: &name, guard: &guard}, "")
	if run == nil || !run.guard || run.dir != "" {
		t.Fatalf("got %+v", run)
	}
}

func TestTheAllowlistComesFromConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if allow, err := guardAllow(); err != nil || len(allow) != 0 {
		t.Fatalf("no file gave %v, %v", allow, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "whatevr"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "whatevr", "send-guard")
	if err := os.WriteFile(file, []byte("# friends\n+91 77700 00001\n\n917770000002 # me again\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	allow, err := guardAllow()
	if err != nil || len(allow) != 2 || allow[0].User != "917770000001" || allow[1].User != "917770000002" {
		t.Fatalf("got %v, %v", allow, err)
	}
	if err := os.WriteFile(file, []byte("mum\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := guardAllow(); err == nil {
		t.Error("a name passed as a number")
	}
}
