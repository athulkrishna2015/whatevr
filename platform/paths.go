// Package platform defines the native locations shared by whatevr clients.
package platform

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const ID = "in.codelif.whatevr"
const SocketEnv = "WHATEVR_SOCKET"

type Paths struct {
	RuntimeDir  string
	SocketPath  string
	DataDir     string
	CacheDir    string
	ConfigDir   string
	StateDir    string
	CaptureDir  string
	LogDir      string
	TUICacheDir string
	TUILogDir   string
}

func home() (string, error) { return os.UserHomeDir() }
func base(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}
func support() (string, error) {
	h, err := home()
	return filepath.Join(h, "Library", "Application Support", ID), err
}
func RuntimeDir() (string, error) {
	if p := os.Getenv("XDG_RUNTIME_DIR"); p != "" {
		if !filepath.IsAbs(p) {
			return "", errors.New("XDG_RUNTIME_DIR must be absolute")
		}
		return p, nil
	}
	return nativeRuntimeDir()
}
func ValidateSocket(p string) error {
	if !filepath.IsAbs(p) {
		return errors.New("socket path must be absolute")
	}
	limit := 107
	if runtime.GOOS == "darwin" {
		limit = 103
	}
	if len(p) > limit {
		return fmt.Errorf("socket path is %d bytes; maximum on %s is %d: %s", len(p), runtime.GOOS, limit, p)
	}
	return nil
}
func SocketPath() (string, error) {
	p := os.Getenv(SocketEnv)
	if p == "" {
		dir, err := RuntimeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(dir, "whatevr", "whatevrd.sock")
	}
	return p, ValidateSocket(p)
}

// ClientSocketPath preserves Linux frontends' conventional /run/user fallback.
func ClientSocketPath() (string, error) {
	if runtime.GOOS == "linux" && os.Getenv(SocketEnv) == "" && os.Getenv("XDG_RUNTIME_DIR") == "" {
		p := filepath.Join("/run/user", fmt.Sprint(os.Getuid()), "whatevr", "whatevrd.sock")
		return p, ValidateSocket(p)
	}
	return SocketPath()
}
func StateHome() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	fallback := filepath.Join(h, ".local", "state")
	if runtime.GOOS == "darwin" {
		fallback = filepath.Join(h, "Library", "Application Support", ID, "state")
	}
	return base("XDG_STATE_HOME", fallback), nil
}
func CaptureRoot(stateHome string) string {
	if runtime.GOOS == "darwin" && os.Getenv("XDG_STATE_HOME") == "" {
		native, err := support()
		if err == nil && stateHome == filepath.Join(native, "state") {
			return filepath.Join(native, "captures")
		}
	}
	return filepath.Join(stateHome, "whatevr", "captures")
}
func LogDir() (string, error) {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "whatevr", "logs"), nil
	}
	h, err := home()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "Logs", ID, "daemon"), nil
	}
	return filepath.Join(h, ".local", "state", "whatevr", "logs"), nil
}
func TUICacheDir() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "whattui"), nil
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "Caches", ID, "tui"), nil
	}
	return filepath.Join(h, ".cache", "whattui"), nil
}
func TUILogDir() (string, error) {
	if runtime.GOOS != "darwin" || os.Getenv("XDG_CACHE_HOME") != "" {
		return TUICacheDir()
	}
	h, err := home()
	return filepath.Join(h, "Library", "Logs", ID, "tui"), err
}
func ConfigDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "whatevr"), nil
	}
	if runtime.GOOS == "darwin" {
		p, err := support()
		return filepath.Join(p, "config"), err
	}
	p, err := os.UserConfigDir()
	return filepath.Join(p, "whatevr"), err
}
func Resolve() (Paths, error) {
	var p Paths
	var err error
	if p.RuntimeDir, err = RuntimeDir(); err != nil {
		return p, err
	}
	if p.SocketPath, err = SocketPath(); err != nil {
		return p, err
	}
	h, err := home()
	if err != nil {
		return p, err
	}
	p.DataDir = filepath.Join(base("XDG_DATA_HOME", filepath.Join(h, ".local", "share")), "whatevrd")
	p.CacheDir = filepath.Join(base("XDG_CACHE_HOME", filepath.Join(h, ".cache")), "whatevrd")
	if runtime.GOOS == "darwin" {
		s, _ := support()
		if os.Getenv("XDG_DATA_HOME") == "" {
			p.DataDir = filepath.Join(s, "daemon")
		}
		if os.Getenv("XDG_CACHE_HOME") == "" {
			p.CacheDir = filepath.Join(h, "Library", "Caches", ID, "daemon")
		}
	}
	if p.ConfigDir, err = ConfigDir(); err != nil {
		return p, err
	}
	if p.StateDir, err = StateHome(); err != nil {
		return p, err
	}
	p.CaptureDir = CaptureRoot(p.StateDir)
	if p.LogDir, err = LogDir(); err != nil {
		return p, err
	}
	if p.TUICacheDir, err = TUICacheDir(); err != nil {
		return p, err
	}
	p.TUILogDir, err = TUILogDir()
	return p, err
}

// InstanceSocket keeps Darwin's short sockaddr_un independent of scratch paths.
// Explicit production socket overrides are never used by isolated instances.
func InstanceSocket(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "darwin" {
		return filepath.Join(root, "run", "whatevr", "whatevrd.sock"), nil
	}
	dir, err := nativeRuntimeDir()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(root))
	p := filepath.Join(dir, "whatevr", fmt.Sprintf("i-%x", digest[:8]), "d.sock")
	return p, ValidateSocket(p)
}

// NotificationSocket is shared by the per-user notification app.
func NotificationSocket() (string, error) {
	dir, err := nativeRuntimeDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "whatevr", "notifications.sock")
	return p, ValidateSocket(p)
}
