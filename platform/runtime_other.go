//go:build !darwin && !windows

package platform

import "errors"

func nativeRuntimeDir() (string, error) { return "", errors.New("XDG_RUNTIME_DIR is not set") }
