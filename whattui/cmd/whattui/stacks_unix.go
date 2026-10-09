//go:build !windows

package main

import "syscall"

func dumpStacksOnDiagnosticSignal() {
	dumpStacksOn(syscall.SIGUSR1)
}
