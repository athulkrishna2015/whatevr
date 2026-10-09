//go:build !cgo

package core

import "github.com/mattn/go-sqlite3"

// StablePlans has no native SQLite handle without cgo. The Windows CI
// cross-compile uses this registration solely to verify platform code; the
// shipped daemon is built with cgo and FTS5.
func readConnectHook(*sqlite3.SQLiteConn) error { return nil }
