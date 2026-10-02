package main

import (
	"os"
	"strings"
)

// usesFlag reports whether the command line has -name or any -name-*.
func usesFlag(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == "--" {
			break
		}
		arg = strings.TrimLeft(arg, "-")
		if arg == name || strings.HasPrefix(arg, name+"=") || strings.HasPrefix(arg, name+"-") {
			return true
		}
	}
	return false
}
