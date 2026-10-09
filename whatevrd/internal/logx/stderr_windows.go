//go:build windows

package logx

import (
	"io"
	"os"
)

func platformStderrWriter(_ *os.File, fallback io.Writer) io.Writer { return fallback }
