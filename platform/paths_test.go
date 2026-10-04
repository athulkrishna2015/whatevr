package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func cleanEnvironment(t *testing.T) {
	t.Helper()
	for _, env := range []string{SocketEnv, "XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(env, "")
	}
	t.Setenv("HOME", t.TempDir())
}
func TestNativeDefaults(t *testing.T) {
	cleanEnvironment(t)
	if runtime.GOOS != "darwin" {
		if _, err := Resolve(); err == nil {
			t.Fatal("Linux daemon requires XDG_RUNTIME_DIR")
		}
		t.Setenv("XDG_RUNTIME_DIR", "/tmp/whatevr-test")
	}
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		support := filepath.Join(os.Getenv("HOME"), "Library", "Application Support", ID)
		want := map[string]string{"data": filepath.Join(support, "daemon"), "state": filepath.Join(support, "state"), "config": filepath.Join(support, "config"), "captures": filepath.Join(support, "captures"), "log": filepath.Join(os.Getenv("HOME"), "Library", "Logs", ID, "daemon")}
		got := map[string]string{"data": p.DataDir, "state": p.StateDir, "config": p.ConfigDir, "captures": p.CaptureDir, "log": p.LogDir}
		for name, v := range want {
			if got[name] != v {
				t.Errorf("%s: got %s, want %s", name, got[name], v)
			}
		}
		t.Setenv("TMPDIR", "/unrelated/terminal/tmp")
		second, err := SocketPath()
		if err != nil || second != p.SocketPath {
			t.Fatalf("socket changed with inherited TMPDIR: %s %v", second, err)
		}
	}
	if err := ValidateSocket(p.SocketPath); err != nil {
		t.Fatal(err)
	}
}
func TestExplicitOverrides(t *testing.T) {
	cleanEnvironment(t)
	for name, dir := range map[string]string{"XDG_RUNTIME_DIR": "/tmp/wv-runtime", "XDG_DATA_HOME": "/tmp/wv-data", "XDG_CACHE_HOME": "/tmp/wv-cache", "XDG_STATE_HOME": "/tmp/wv-state", "XDG_CONFIG_HOME": "/tmp/wv-config"} {
		t.Setenv(name, dir)
	}
	t.Setenv(SocketEnv, "/tmp/wv-custom/d.sock")
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{p.SocketPath: "/tmp/wv-custom/d.sock", p.DataDir: "/tmp/wv-data/whatevrd", p.CacheDir: "/tmp/wv-cache/whatevrd", p.LogDir: "/tmp/wv-state/whatevr/logs", p.CaptureDir: "/tmp/wv-state/whatevr/captures", p.ConfigDir: "/tmp/wv-config/whatevr", p.TUICacheDir: "/tmp/wv-cache/whattui", p.TUILogDir: "/tmp/wv-cache/whattui"}
	for got, want := range checks {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}
func TestInvalidSocketOverrides(t *testing.T) {
	cleanEnvironment(t)
	for _, path := range []string{"relative.sock", "/tmp/" + strings.Repeat("a", 108)} {
		t.Setenv(SocketEnv, path)
		if _, err := SocketPath(); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
}
func TestInstanceSockets(t *testing.T) {
	cleanEnvironment(t)
	root := filepath.Join(t.TempDir(), strings.Repeat("instance", 30))
	t.Setenv(SocketEnv, "/production/daemon.sock")
	a, err := InstanceSocket(root)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := InstanceSocket(root)
	other, _ := InstanceSocket(root + "-other")
	if a != b || a == other || a == os.Getenv(SocketEnv) {
		t.Fatal("instance sockets must be deterministic and isolated")
	}
	if runtime.GOOS == "darwin" {
		if err := ValidateSocket(a); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(a, root) {
			t.Fatal("Darwin socket must not inherit long instance path")
		}
	}
}
func TestLogsDoNotRequireRuntime(t *testing.T) {
	cleanEnvironment(t)
	if _, err := LogDir(); err != nil {
		t.Fatal(err)
	}
}

func TestClientSocketDefaults(t *testing.T) {
	cleanEnvironment(t)
	got, err := ClientSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if !strings.HasPrefix(got, "/run/user/") {
			t.Fatalf("lost Linux frontend fallback: %s", got)
		}
	} else if runtime.GOOS == "darwin" {
		daemon, err := SocketPath()
		if err != nil || daemon != got {
			t.Fatalf("daemon and frontend disagree: %s, %s, %v", daemon, got, err)
		}
	}
}
