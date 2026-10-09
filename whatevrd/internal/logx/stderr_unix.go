//go:build !windows

package logx

import (
	"io"
	"os"

	"github.com/coreos/go-systemd/v22/journal"
	"github.com/rs/zerolog/journald"
)

func platformStderrWriter(f *os.File, fallback io.Writer) io.Writer {
	if ok, err := journal.StderrIsJournalStream(); err == nil && ok && f == os.Stderr {
		return journald.NewJournalDWriter()
	}
	return fallback
}
