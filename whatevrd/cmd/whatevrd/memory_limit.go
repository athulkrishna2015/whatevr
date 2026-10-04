package main

import (
	"os"
	"runtime/debug"
)

// Collect earlier after bursts; an explicit GOMEMLIMIT takes precedence.
const goMemoryLimit = 64 << 20

func limitMemory() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(goMemoryLimit)
	}
}
