//go:build whatevr_capture

package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/app"
	"whatevrd/internal/capture"
	"whatevrd/internal/protocol"
	"whatevrd/internal/wa"
)

type captureFlagSet struct {
	name  *string
	guard *bool
}

func captureFlags(zerolog.Logger) *captureFlagSet {
	return &captureFlagSet{
		name:  flag.String("capture", "", "record this run into a capture: a name under $XDG_STATE_HOME/whatevr/captures, or a path"),
		guard: flag.Bool("send-guard", false, "refuse every outward send to anyone but this account and the allowlist"),
	}
}

// guardAllow is who a real-account run may write to besides the account
// itself. the fork's guard adds the account's own pn and lid.
var guardAllow = []types.JID{types.NewJID("910000000000", types.DefaultUserServer)}

type captureRun struct {
	// dir is empty for a --send-guard run that records nothing
	dir   string
	name  string
	mock  string
	guard bool
	rec   *capture.Recorder
}

// capturePrepare settles where the capture goes and, on the real server,
// moves the whole daemon into the capture's own linked device. it runs after
// mockPrepare, so under --mock a name lands in the mock's scratch state.
func capturePrepare(log zerolog.Logger, f *captureFlagSet, mockScenario string) *captureRun {
	if mockScenario != "" && *f.guard {
		log.Fatal().Msg("--send-guard is for real-account runs, the mock never takes it")
	}
	if *f.name == "" {
		if *f.guard {
			return &captureRun{guard: true}
		}
		return nil
	}
	stateHome, err := app.StateHome()
	if err != nil {
		log.Fatal().Err(err).Msg("resolve state dir")
	}
	dir, err := capture.Resolve(*f.name, stateHome)
	if err != nil {
		log.Fatal().Err(err).Msg("resolve --capture")
	}
	run := &captureRun{dir: dir, name: filepath.Base(dir), mock: mockScenario, guard: mockScenario == ""}
	if run.mock == "" {
		isolate(log, run)
	}
	return run
}

// isolate gives a real-account capture its own linked device, so it starts
// from a pairing and never touches the daily driver. the socket stays under
// the real runtime dir: a unix socket path is capped at 108 bytes.
func isolate(log zerolog.Logger, run *captureRun) {
	runtimeBase := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeBase == "" {
		log.Fatal().Msg("XDG_RUNTIME_DIR is unset")
	}
	home := filepath.Join(run.dir, "home")
	for env, path := range map[string]string{
		"XDG_DATA_HOME":   filepath.Join(home, "data"),
		"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
		"XDG_STATE_HOME":  filepath.Join(home, "state"),
		"XDG_RUNTIME_DIR": filepath.Join(runtimeBase, "whatevr-capture", run.name),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			log.Fatal().Err(err).Str("path", path).Msg("create capture home")
		}
		if err := os.Setenv(env, path); err != nil {
			log.Fatal().Err(err).Str("env", env).Msg("set capture env")
		}
	}
}

// captureStart opens the segment and builds what wa hangs on each client.
func captureStart(ctx context.Context, run *captureRun, runID string) (wa.Instrument, func()) {
	if run == nil {
		return wa.Instrument{}, func() {}
	}
	log := zerolog.Ctx(ctx)
	var hooks []func(*whatsmeow.Client)
	inst := wa.Instrument{}
	if run.guard {
		hooks = append(hooks, func(cli *whatsmeow.Client) {
			cli.SendGuard = &whatsmeow.SendGuard{Allow: guardAllow}
		})
		log.Warn().Stringer("allow", guardAllow[0]).Msg("send guard on, outward sends go only to this account and the allowlist")
	}
	stop := func() {}
	if run.dir != "" {
		w, err := capture.Open(run.dir, run.name, capture.Start{
			Version: protocol.Version, Run: runID, Mock: run.mock, Guard: run.guard, PID: os.Getpid(),
		})
		if err != nil {
			log.Fatal().Err(err).Str("dir", run.dir).Msg("open capture")
		}
		run.rec = capture.NewRecorder(w, log.With().Str("module", "capture").Logger())
		hooks = append(hooks, run.rec.Hook)
		inst.Transport = run.rec.Transport(nil)
		log.Info().Str("dir", run.dir).Int("segment", w.Segment()).Msg("capturing")
		if run.mock == "" {
			log.Warn().Str("XDG_RUNTIME_DIR", os.Getenv("XDG_RUNTIME_DIR")).
				Msg("this capture is its own linked device, run whattui with this XDG_RUNTIME_DIR")
		}
		stop = func() {
			if err := w.Close(); err != nil {
				log.Error().Err(err).Msg("close capture")
			}
		}
	}
	inst.Client = func(cli *whatsmeow.Client) {
		for _, h := range hooks {
			h(cli)
		}
	}
	return inst, stop
}

func captureTap(run *captureRun) protocol.Tap {
	if run == nil || run.rec == nil {
		return nil
	}
	return run.rec.Frontend
}
