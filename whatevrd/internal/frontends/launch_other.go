//go:build !darwin

package frontends

import (
	"errors"
	"os"
	"os/exec"
)

func defaultTerminal(f Frontend) ([]string, error) {
	// the freedesktop way: it picks the user's default terminal
	if _, err := exec.LookPath("xdg-terminal-exec"); err == nil {
		return append([]string{"xdg-terminal-exec"}, f.Exec...), nil
	}
	if t := os.Getenv("TERMINAL"); t != "" {
		return append([]string{t, "-e"}, f.Exec...), nil
	}
	return nil, ErrNoTerminal
}

func openApp(string) error { return errors.New("app frontends only exist on macOS") }
