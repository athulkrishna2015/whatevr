package frontends

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ErrNoTerminal is a terminal frontend with no terminal to run it in.
var ErrNoTerminal = errors.New("no terminal to run it in: set one with whatevrd frontend terminal set")

// Launch starts f and doesn't wait for it. terminal is the user's terminal
// argv, which the frontend's argv is appended to; empty means the platform's.
func Launch(f Frontend, terminal []string) error {
	if f.App != "" {
		return openApp(f.App)
	}
	if len(f.Exec) == 0 {
		return errors.New("frontend " + f.ID + " has nothing to run")
	}
	argv := f.Exec
	if f.Terminal {
		var err error
		if argv, err = inTerminal(f, terminal); err != nil {
			return err
		}
	}
	return start(argv)
}

func inTerminal(f Frontend, terminal []string) ([]string, error) {
	if len(terminal) > 0 {
		return append(append([]string{}, terminal...), f.Exec...), nil
	}
	return defaultTerminal(f)
}

func start(argv []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	argv = escapeService(append([]string{path}, argv[1:]...))
	cmd := exec.Command(argv[0], argv[1:]...)
	// its own session: stopping the daemon must not take the frontend along
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// escapeService moves argv out of a systemd service's cgroup, which systemd
// kills whole when the daemon stops or restarts.
func escapeService(argv []string) []string {
	if os.Getenv("INVOCATION_ID") == "" {
		return argv
	}
	run, err := exec.LookPath("systemd-run")
	if err != nil {
		return argv
	}
	return append([]string{run, "--user", "--scope", "--collect", "--quiet", "--"}, argv...)
}
