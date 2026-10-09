//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func AcquireProcessLock(path string) (*ProcessLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	var overlapped windows.Overlapped
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err != nil {
		file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, fmt.Errorf("%w: %s", ErrAlreadyRunning, path)
		}
		return nil, err
	}

	return &ProcessLock{file: file, path: path}, nil
}

func (l *ProcessLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}

	if l.path != "" {
		if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = err
		}
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
