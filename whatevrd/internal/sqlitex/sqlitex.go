// Package sqlitex is what every sqlite database of the daemon opens with.
//go:build cgo

package sqlitex

/*
extern int sqlite3_db_config(void*, int, ...);
// sqlite3_db_config is variadic, cgo cannot call it as is. 1007 is
// SQLITE_DBCONFIG_ENABLE_QPSG; on says what it is now.
static int stablePlans(void *db, int *on) { return sqlite3_db_config(db, 1007, 1, on); }
*/
import "C"

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// StablePlans turns on sqlite's query planner stability guarantee. without
// it a statement whose plan could hang on a bound value is compiled again
// each time it runs, and the driver rebinds every value on every run: a kept
// statement cost as much as a new one. the driver has no way to set it, so
// this reaches for its connection handle.
func StablePlans(conn *sqlite3.SQLiteConn) error {
	f := reflect.ValueOf(conn).Elem().FieldByName("db")
	if !f.IsValid() || f.Kind() != reflect.Pointer || f.IsNil() {
		return errors.New("sqlitex: no sqlite handle in the driver's connection")
	}
	var on C.int
	if rc := C.stablePlans(f.UnsafePointer(), &on); rc != 0 || on != 1 {
		return fmt.Errorf("sqlitex: qpsg: rc %d, on %d", rc, on)
	}
	return nil
}

// DSN is dsn with stmts statements kept compiled per connection and no lock
// of sqlite's own around each call: database/sql never hands one connection
// to two goroutines at once.
func DSN(dsn string, stmts int) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%s_stmt_cache_size=%d&_mutex=no", dsn, sep, stmts)
}
