package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/codelif/whatevr/platform"
)

// SocketEnv names the socket on every OS, for the daemon and its frontends.
const SocketEnv = "WHATEVR_SOCKET"

type Paths struct {
	RuntimeDir string
	// SocketDir/SocketPath serve the whatevr protocol (PROTOCOL.md), the
	// daemon's only socket. WHATEVR_SOCKET moves it
	SocketDir  string
	SocketPath string
	// LockPath is one daemon per socket; the core locks its own db
	LockPath      string
	DataDir       string
	CacheDir      string
	SessionDir    string
	SessionDBPath string
	MediaCacheDir string
	// LogDir holds one jsonl file per run, see internal/logx.
	LogDir string
}

func ResolvePaths() (Paths, error) {
	defaults, err := platform.Resolve()
	if err != nil {
		return Paths{}, err
	}
	runtimeBase := defaults.RuntimeDir
	socket := defaults.SocketPath
	socketDir := filepath.Dir(socket)
	dataDir := defaults.DataDir
	cacheDir := defaults.CacheDir

	return Paths{
		RuntimeDir:    runtimeBase,
		SocketDir:     socketDir,
		SocketPath:    socket,
		LockPath:      LockPath(socket),
		DataDir:       dataDir,
		CacheDir:      cacheDir,
		SessionDir:    filepath.Join(dataDir, "session"),
		SessionDBPath: filepath.Join(dataDir, "session", "whatsmeow.db"),
		MediaCacheDir: filepath.Join(cacheDir, "media"),
		LogDir:        defaults.LogDir,
	}, nil
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.SocketDir, p.DataDir, p.SessionDir, p.CacheDir, p.MediaCacheDir, p.LogDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	return nil
}

// LockPath sits next to the socket under the same name, so one socket gets one
// daemon wherever WHATEVR_SOCKET points
func LockPath(socket string) string {
	return strings.TrimSuffix(socket, ".sock") + ".lock"
}

// StateHome returns the platform's persistent development state location.
func StateHome() (string, error) { return platform.StateHome() }
