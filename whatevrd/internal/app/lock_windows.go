//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

var ErrAlreadyRunning = errors.New("another whatevrd process is already running")

type ProcessLock struct {
	file *os.File
}

func AcquireProcessLock(path string) (*ProcessLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, fmt.Errorf("%w: %s", ErrAlreadyRunning, path)
		}
		return nil, err
	}
	return &ProcessLock{file: f}, nil
}

func (l *ProcessLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	var overlapped windows.Overlapped
	err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &overlapped)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
