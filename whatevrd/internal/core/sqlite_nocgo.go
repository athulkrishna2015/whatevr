//go:build !cgo

package core

import (
	"database/sql"

	"github.com/mattn/go-sqlite3"
)

// StablePlans has no native SQLite handle without cgo. The Windows CI
// cross-compile uses this registration solely to verify platform code; the
// shipped daemon is built with cgo and FTS5.
func init() {
	sql.Register(driverName, &sqlite3.SQLiteDriver{})
	sql.Register(readDriverName, &sqlite3.SQLiteDriver{})
}
