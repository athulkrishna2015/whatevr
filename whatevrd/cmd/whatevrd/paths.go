package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/codelif/whatevr/platform"
	"github.com/urfave/cli/v3"
)

// instanceDir is the darwin socket dir of a mock or capture run, gone with it
var instanceDir string

func setInstanceSocket(root string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	p, err := platform.InstanceSocket(root)
	if err != nil {
		return err
	}
	instanceDir = filepath.Dir(p)
	return os.Setenv(platform.SocketEnv, p)
}

// removeInstanceDir runs once the sockets in it are closed. Remove, not
// RemoveAll: anything still in there isn't ours to delete.
func removeInstanceDir() {
	if instanceDir != "" {
		_ = os.Remove(instanceDir)
	}
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

func pathsCommand() *cli.Command {
	flags := []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "print resolved platform paths as JSON"}}
	if mockPathsSupported {
		flags = append(flags, &cli.StringFlag{Name: "mock-dir", Usage: "resolve an isolated mock directory without starting a daemon"})
	}
	return &cli.Command{
		Name:  "paths",
		Usage: "print where whatevrd keeps its socket, data, cache and logs",
		Flags: flags,
		Action: func(_ context.Context, c *cli.Command) error {
			if c.Args().Present() {
				return usage(c, "paths takes no positional arguments")
			}
			out, stderr := outputs(c)
			return code(printPaths(c.Bool("json"), c.String("mock-dir"), out, stderr))
		},
	}
}

func printPaths(asJSON bool, instance string, out, stderr io.Writer) int {
	if instance != "" {
		if err := instanceEnvironment(instance); err != nil {
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
	if asJSON {
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
