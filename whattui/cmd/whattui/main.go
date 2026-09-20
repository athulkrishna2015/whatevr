// Command whattui is a terminal frontend for whatevr.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/ui"
)

func main() {
	var (
		socket   = flag.String("socket", "", "whatevrd socket (default: $XDG_RUNTIME_DIR/whatevr/whatevrd.sock)")
		showCaps = flag.Bool("caps", false, "print what was detected about this terminal and exit")
	)
	flag.Parse()

	if err := run(*socket, *showCaps); err != nil {
		fmt.Fprintf(os.Stderr, "whattui: %v\n", err)
		os.Exit(1)
	}
}

func run(socket string, showCaps bool) (err error) {
	vx, err := vaxis.New(vaxis.Options{
		WithTTY:     "",
		CSIuBitMask: vaxis.CSIuDisambiguate,
	})
	if err != nil {
		return fmt.Errorf("terminal setup: %w", err)
	}

	// A panic must never leave the terminal in the alt screen with the mouse
	// grabbed and the cursor hidden. Close first, then say what happened.
	defer func() {
		if r := recover(); r != nil {
			vx.Close()
			path := writeCrashLog(r, debug.Stack())
			err = fmt.Errorf("crashed: %v (details in %s)", r, path)
		}
	}()

	caps := term.Detect(vx)
	if showCaps {
		vx.Close()
		fmt.Print(caps.Report())
		return nil
	}

	defer vx.Close()

	client := proto.New(socket, "whattui")
	return ui.New(vx, caps, client).Run()
}

// writeCrashLog puts the panic somewhere it can be read after the terminal has
// been handed back, and returns where. A crash that scrolls past is a crash
// nobody can report.
func writeCrashLog(r any, stack []byte) string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "(nowhere: no cache directory)"
		}
		dir = filepath.Join(home, ".cache")
	}
	dir = filepath.Join(dir, "whattui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "(nowhere: " + err.Error() + ")"
	}
	path := filepath.Join(dir, "crash.log")
	body := fmt.Sprintf("%s\npanic: %v\n\n%s\n", time.Now().Format(time.RFC3339), r, stack)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "(nowhere: " + err.Error() + ")"
	}
	return path
}
