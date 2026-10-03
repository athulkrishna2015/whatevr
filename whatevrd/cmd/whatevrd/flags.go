package main

import (
	"os"
	"strings"
	"time"
)

// usesFlag reports whether the command line has -name or any -name-*.
func usesFlag(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == "--" {
			break
		}
		arg = strings.TrimLeft(arg, "-")
		if arg == name || strings.HasPrefix(arg, name+"=") || strings.HasPrefix(arg, name+"-") {
			return true
		}
	}
	return false
}

// mockClocks are the clocks a mock run's daemon reads instead of the
// machine's: wall for timers and backoff, stamp for what the core stamps on
// arrivals. nil outside a mock run.
type mockClocks struct {
	wall, stamp func() time.Time
}
