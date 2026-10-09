//go:build windows

package main

// Windows has no SIGUSR1 diagnostic signal. Crash logging remains available
// through the panic handler; a named-pipe or console-control trigger can be
// added when Windows daemon support is shipped.
func dumpStacksOnDiagnosticSignal() {}
