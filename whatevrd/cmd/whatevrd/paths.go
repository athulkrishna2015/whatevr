package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/codelif/whatevr/platform"
)

func setInstanceSocket(root string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	p, err := platform.InstanceSocket(root)
	if err != nil {
		return err
	}
	return os.Setenv(platform.SocketEnv, p)
}

// instanceEnvironment only changes this process; querying paths creates no files.
func instanceEnvironment(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	for name, sub := range map[string]string{"XDG_RUNTIME_DIR": "run", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state", "XDG_CONFIG_HOME": "config"} {
		if err := os.Setenv(name, filepath.Join(root, sub)); err != nil {
			return err
		}
	}
	if err := os.Unsetenv(platform.SocketEnv); err != nil {
		return err
	}
	return setInstanceSocket(root)
}
func runPaths(args []string, out, stderr io.Writer) int {
	fs := flag.NewFlagSet("paths", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print resolved platform paths as JSON")
	var instance *string
	if mockPathsSupported {
		instance = fs.String("mock-dir", "", "resolve an isolated mock directory without starting a daemon")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "paths takes no positional arguments")
		return 2
	}
	if instance != nil && *instance != "" {
		if err := instanceEnvironment(*instance); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	p, err := platform.Resolve()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// The control endpoint is optional, but uses the same short instance directory.
	result := struct {
		platform.Paths
		ControlPath string
	}{p, filepath.Join(filepath.Dir(p.SocketPath), "control.sock")}
	if *asJSON {
		err = json.NewEncoder(out).Encode(result)
	} else {
		var raw []byte
		raw, err = json.MarshalIndent(result, "", "  ")
		if err == nil {
			_, err = fmt.Fprintln(out, string(raw))
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
