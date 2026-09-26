package ui

import (
	"os/exec"
	"strings"
	"testing"
)

// The frame harness names a scenario and a chat inside it, and the screenshot
// script names more. None of that is compiled against anything: a scenario
// renamed in wamock would leave this package waiting sixty seconds for a chat
// that no longer exists, at which point the failure says nothing useful. Ask
// the binary what it has instead.
func TestNamedScenariosExist(t *testing.T) {
	binary, err := whatevrdBinary()
	if err != nil {
		t.Fatalf("mock daemon: %v", err)
	}
	out, err := exec.Command(binary, "--mock-list").Output()
	if err != nil {
		t.Fatalf("--mock-list: %v", err)
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if name, _, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			have[name] = true
		}
	}
	for _, name := range []string{framesScenario, floodScenario, tortureScenario} {
		if !have[name] {
			t.Errorf("scenario %q is gone; whatevrd --mock-list knows %v", name, keys(have))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
