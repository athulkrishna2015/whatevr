//go:build !windows

package frontends

import (
	"os/exec"
	"syscall"
)

// its own session: stopping the daemon must not take the frontend along.
func configureChild(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
