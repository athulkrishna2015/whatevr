//go:build cgo

package core

import (
	"database/sql"

	"github.com/mattn/go-sqlite3"

	"whatevrd/internal/sqlitex"
)

func init() {
	sql.Register(driverName, &sqlite3.SQLiteDriver{ConnectHook: sqlitex.StablePlans})
	sql.Register(readDriverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			if err := sqlitex.StablePlans(conn); err != nil {
				return err
			}
			for _, pragma := range []string{
				`PRAGMA busy_timeout = 5000`,
				`PRAGMA query_only = ON`,
				`PRAGMA foreign_keys = ON`,
				`PRAGMA mmap_size = 0`,
			} {
				if _, err := conn.Exec(pragma, nil); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
