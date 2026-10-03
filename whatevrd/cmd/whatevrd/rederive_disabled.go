//go:build !whatevr_core

package main

import (
	"fmt"
	"io"
)

func runRederive(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "whatevrd rederive: this whatevrd was built without the new core; rebuild with -tags whatevr_core")
	return 2
}
