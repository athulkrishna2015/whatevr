//go:build whatevr_capture

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/codelif/whatevr/platform"
	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	"whatevrd/internal/capture"
)

type captureFlagSet struct {
	name  *string
	guard *bool
}

func captureFlags(zerolog.Logger) *captureFlagSet {
	return &captureFlagSet{
		name:  flag.String("capture", "", "record this run into a capture: a name under the platform capture directory, or a path"),
		guard: flag.Bool("send-guard", false, "refuse every outward send to anyone but this account and the allowlist"),
	}
}

// guardAllow reads who a real-account run may write to besides the account
// itself (the fork's guard adds its own pn and lid): one number with its
// country code per line of the platform config directory's send-guard, # for
// comments. kept out of the repo, they are real people's numbers
func guardAllow() ([]types.JID, error) {
	dir, err := platform.ConfigDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "send-guard"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var allow []types.JID
	for i, line := range strings.Split(string(raw), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.NewReplacer(" ", "", "\t", "", "+", "", "-", "", "\r", "").Replace(line)
		if line == "" {
			continue
		}
		if strings.Trim(line, "0123456789") != "" {
			return nil, fmt.Errorf("send-guard line %d: %q is not a phone number", i+1, line)
		}
		allow = append(allow, types.NewJID(line, types.DefaultUserServer))
	}
	return allow, nil
}

type captureRun struct {
	// dir is empty for a --send-guard run that records nothing
	dir   string
	name  string
	mock  string
	guard bool
	allow []types.JID
	rec   *capture.Recorder
}

// capturePrepare settles where the capture goes and, on the real server,
// moves the whole daemon into the capture's own linked device. it runs after
// mockPrepare, so under --mock a name lands in the mock's scratch state.
func capturePrepare(log zerolog.Logger, f *captureFlagSet, mockScenario string) *captureRun {
	if mockScenario != "" && *f.guard {
		log.Fatal().Msg("--send-guard is for real-account runs, the mock never takes it")
	}
	var allow []types.JID
	if mockScenario == "" {
		// before isolate moves the xdg dirs
		var err error
		if allow, err = guardAllow(); err != nil {
			log.Fatal().Err(err).Msg("read the send guard's allowlist")
		}
	}
	if *f.name == "" {
		if *f.guard {
			return &captureRun{guard: true, allow: allow}
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
	run := &captureRun{dir: dir, name: filepath.Base(dir), mock: mockScenario, guard: mockScenario == "", allow: allow}
	if run.mock == "" {
		isolate(log, run)
	}
	return run
}

// isolate gives a real-account capture its own linked device, so it starts
// from a pairing and never touches the daily driver. the socket stays under
// the real runtime dir: a unix socket path is capped at 108 bytes.
func isolate(log zerolog.Logger, run *captureRun) {
	runtimeBase, err := platform.RuntimeDir()
	if err != nil {
		log.Fatal().Err(err).Msg("resolve capture runtime directory")
	}
	// the capture's daemon has its own socket, never one set for the real one
	if err := os.Unsetenv(app.SocketEnv); err != nil {
		log.Fatal().Err(err).Msg("unset " + app.SocketEnv)
	}
	home := filepath.Join(run.dir, "home")
	for env, path := range map[string]string{
		"XDG_DATA_HOME":   filepath.Join(home, "data"),
		"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
		"XDG_STATE_HOME":  filepath.Join(home, "state"),
		"XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_RUNTIME_DIR": filepath.Join(runtimeBase, "whatevr-capture", run.name),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			log.Fatal().Err(err).Str("path", path).Msg("create capture home")
		}
		if err := os.Setenv(env, path); err != nil {
			log.Fatal().Err(err).Str("env", env).Msg("set capture env")
		}
	}
	if err := setInstanceSocket(filepath.Join(runtimeBase, "whatevr-capture", run.name)); err != nil {
		log.Fatal().Err(err).Msg("resolve capture socket")
	}
}

// captureStart opens the segment and builds what hangs on each client.
func captureStart(ctx context.Context, run *captureRun, runID string) (clientHooks, func()) {
	if run == nil {
		return clientHooks{}, func() {}
	}
	log := zerolog.Ctx(ctx)
	var hooks []func(*whatsmeow.Client)
	inst := clientHooks{}
	if run.guard {
		hooks = append(hooks, func(cli *whatsmeow.Client) {
			cli.SendGuard = &whatsmeow.SendGuard{Allow: run.allow}
		})
		log.Warn().Int("allow", len(run.allow)).Msg("send guard on, outward sends go only to this account and the allowlist")
	}
	stop := func() {}
	if run.dir != "" {
		w, err := capture.Open(run.dir, run.name, capture.Start{
			Version: version, Run: runID, Mock: run.mock, Guard: run.guard, PID: os.Getpid(),
		})
		if err != nil {
			log.Fatal().Err(err).Str("dir", run.dir).Msg("open capture")
		}
		run.rec = capture.NewRecorder(w, log.With().Str("module", "capture").Logger())
		hooks = append(hooks, run.rec.Hook)
		inst.transport = run.rec.Transport(nil)
		log.Info().Str("dir", run.dir).Int("segment", w.Segment()).Msg("capturing")
		if run.mock == "" {
			paths, _ := app.ResolvePaths()
			log.Warn().Str("WHATEVR_SOCKET", paths.SocketPath).Msg("this capture is its own linked device; pass this socket to whattui")
		}
		stop = func() {
			if err := w.Close(); err != nil {
				log.Error().Err(err).Msg("close capture")
			}
		}
	}
	inst.client = func(cli *whatsmeow.Client) {
		for _, h := range hooks {
			h(cli)
		}
	}
	return inst, stop
}

// captureTap records every frame as its json, so a capture reads without
// the schema and the replayer can swap ids in it.
func captureTap(run *captureRun) tap {
	if run == nil || run.rec == nil {
		return nil
	}
	dirs := map[string]string{"in": capture.FrontendReq, "out": capture.FrontendResp}
	return func(conn uint64, dir string, frame []byte) {
		if d, ok := dirs[dir]; ok {
			dir = d
		}
		if len(frame) > 0 {
			var f v2.Frame
			if proto.Unmarshal(frame, &f) == nil {
				if j, err := capture.FrameJSON.Marshal(&f); err == nil {
					frame = j
				}
			}
		}
		run.rec.Frontend(int64(conn), dir, frame)
	}
}
