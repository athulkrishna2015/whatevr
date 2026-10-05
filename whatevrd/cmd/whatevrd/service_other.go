//go:build !darwin

package main

import (
	"fmt"
	"io"
)

func runService(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "service commands are available on macOS; on Linux use systemctl --user with the installed units")
	return 2
}

func serviceLoaded() bool { return false }

func asService() bool { return false }
