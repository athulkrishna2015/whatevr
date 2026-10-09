//go:build !cgo

package sqlitex

import (
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// StablePlans cannot configure SQLite when cgo is disabled. The Windows CI
// cross-compile uses this registration solely to verify platform code; the
// shipped daemon is built with cgo and FTS5.
func StablePlans(*sqlite3.SQLiteConn) error { return nil }

// DSN keeps parity with the cgo build's cache/mutex settings.
func DSN(dsn string, stmts int) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%s_stmt_cache_size=%d&_mutex=no", dsn, sep, stmts)
}
