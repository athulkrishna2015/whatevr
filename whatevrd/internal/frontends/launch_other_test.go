//go:build !darwin

package frontends

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultTerminalFallsBackToTERMINAL(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	f := Frontend{ID: "whattui", Exec: []string{"whattui"}, Terminal: true}
	t.Setenv("TERMINAL", "foot")
	got, err := defaultTerminal(f)
	if err != nil || strings.Join(got, " ") != "foot -e whattui" {
		t.Fatalf("%q %v", got, err)
	}
	t.Setenv("TERMINAL", "")
	if _, err := defaultTerminal(f); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("no terminal: %v", err)
	}
}
