//go:build whatevr_capture

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestRealAccountCaptureGetsItsOwnDevice(t *testing.T) {
	state, runtime := t.TempDir(), t.TempDir()
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
		"XDG_RUNTIME_DIR": filepath.Join(runtime, "whatevr-capture", "probe"),
	} {
		if got := os.Getenv(env); got != want {
			t.Errorf("%s=%s, want %s", env, got, want)
		}
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
