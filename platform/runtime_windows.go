//go:build windows

package platform

import (
	"os"
	"path/filepath"
)

func nativeRuntimeDir() (string, error) {
	return filepath.Join(os.TempDir(), ID), nil
}
