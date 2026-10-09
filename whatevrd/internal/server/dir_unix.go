//go:build unix

package server

import (
	"fmt"
	"os"
	"syscall"
)

// checkDir wants the socket's directory ours and closed to everyone else.
func checkDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is open to other users", dir)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("%s belongs to another user", dir)
	}
	return nil
}
