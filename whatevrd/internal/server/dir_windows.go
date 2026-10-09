//go:build windows

package server

import "os"

// Windows local pipe ACLs are assigned by the service/runtime layer; there
// is no Unix socket directory mode to validate.
func checkDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid
	}
	return nil
}
