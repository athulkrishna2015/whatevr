package app

import (
	"errors"
	"os"
)

// ProcessLock is the held lock file; acquire and release live in the
// platform files.
type ProcessLock struct {
	file *os.File
	path string
}

var ErrAlreadyRunning = errors.New("another whatevrd process is already running")
