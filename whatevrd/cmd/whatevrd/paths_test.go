package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codelif/whatevr/platform"
)

func TestPathsQueryDoesNotCreateAccountFiles(t *testing.T) {
	for _, name := range []string{platform.SocketEnv, "XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(name, "")
	}
	root := filepath.Join(t.TempDir(), "isolated")
	var out, stderr bytes.Buffer
	args := []string{"--json"}
	if mockPathsSupported {
		args = append(args, "--mock-dir", root)
	} else {
		t.Setenv("XDG_RUNTIME_DIR", "/tmp/wv-paths-query")
		t.Setenv("XDG_DATA_HOME", root)
	}
	if code := run(append([]string{"whatevrd", "paths"}, args...), &out, &stderr); code != 0 {
		t.Fatalf("paths query: %d %s", code, &stderr)
	}
	var got struct {
		platform.Paths
		ControlPath string
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SocketPath == "" || got.LogDir == "" || got.ControlPath == "" {
		t.Fatal("incomplete paths")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("query created account directories")
	}
	if mockPathsSupported {
		expected, err := platform.InstanceSocket(root)
		if err != nil || got.SocketPath != expected {
			t.Fatalf("wrong instance socket %s, want %s: %v", got.SocketPath, expected, err)
		}
	}
}
