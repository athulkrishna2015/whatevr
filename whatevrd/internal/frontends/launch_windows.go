//go:build windows

package frontends

import "os/exec"

// Windows has no Unix session id to detach from.
func configureChild(*exec.Cmd) {}
