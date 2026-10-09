//go:build !darwin

package platform

import "errors"

func nativeRuntimeDir() (string, error) { return "", errors.New("XDG_RUNTIME_DIR is not set") }
