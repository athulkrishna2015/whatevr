// Command whattui is a terminal frontend for whatevr.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
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
		// A terminal that reports its window hidden gets no frames until it
		// says otherwise, which is the right thing everywhere except a
		// compositor that suspends a window nobody is looking at: a dummy
		// monitor, a screenshot harness, a remote session that never says it
		// came back. WHATTUI_ALWAYS_RENDER=1 draws regardless.
		DisableVisibilityReports: os.Getenv("WHATTUI_ALWAYS_RENDER") == "1",
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

	dumpStacksOn(syscall.SIGUSR1)

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
	path := filepath.Join(cacheDir(), "crash.log")
	body := fmt.Sprintf("%s\npanic: %v\n\n%s\n", time.Now().Format(time.RFC3339), r, stack)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "(nowhere: " + err.Error() + ")"
	}
	return path
}

// dumpStacksOn writes every goroutine's stack to the cache directory when the
// signal arrives. A frozen terminal tells you nothing on its own, and this is
// the difference between "it hung" and knowing which goroutine is waiting on
// what.
func dumpStacksOn(sig os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	go func() {
		for range ch {
			buf := make([]byte, 1<<20)
			buf = buf[:runtime.Stack(buf, true)]
			path := filepath.Join(cacheDir(), "stacks.log")
			_ = os.WriteFile(path, buf, 0o600)
		}
	}()
}

// cacheDir is where the crash log and the stack dump go.
func cacheDir() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return os.TempDir()
		}
		dir = filepath.Join(home, ".cache")
	}
	dir = filepath.Join(dir, "whattui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return os.TempDir()
	}
	return dir
}
