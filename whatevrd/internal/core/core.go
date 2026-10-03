// Package core is the new daemon store: an append-only log of every input the
// daemon took, and derived tables folded from it by one writer goroutine.
//
// whatever arrives (a message, a receipt, an app state mutation, a network
// job's result) is appended before it is acted on, and the append is durable
// before Append returns. folding happens after, from the log, so a crash
// between the two only means folding again from the last folded seq. derived
// tables are a pure function of the log: change a fold, bump its domain's
// version, and the next open rebuilds them from seq 1.
package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"

	"whatevrd/internal/sqlitex"
)

const (
	driverName     = "whatevrd-core"
	readDriverName = "whatevrd-core-ro"

	// statements each connection keeps compiled. a fold runs the same few
	// dozen over and over, and compiling one costs more than running it.
	writeStmts = 256
	readStmts  = 64

	// schemaVersion covers the log's own tables. derived tables are versioned
	// per domain and rebuilt, never migrated.
	schemaVersion = 1
)

var ErrClosed = errors.New("core: closed")

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

type Options struct {
	// Clock stamps each input's receive time. nil is the system clock.
	Clock Clock
	// Domains fold the log into derived tables, in this order for each input.
	Domains []Domain
	// OnChange runs on the writer goroutine after every fold commit. it must
	// not block.
	OnChange func(Change)
	Log      zerolog.Logger
	// Rebuild drops every derived table at open and folds the whole log
	// again, as a fold signature change does
	Rebuild bool
	// LogIndexes are CREATE INDEX IF NOT EXISTS statements on inputs, for
	// readers that look facts up in the log itself rather than in a fold
	LogIndexes []string
}

type DB struct {
	opts Options
	log  zerolog.Logger

	write *sql.DB
	read  *sql.DB

	folds  map[string][]FoldFunc
	tables []string
	// watch is every kind some Finish watches
	watch map[string]bool

	appended atomic.Int64
	folded   atomic.Int64

	foldedMu   sync.Mutex
	foldedWait chan struct{}

	health health
	w      *writer
}

// Open opens or creates the store at path and starts its writer. a fold
// signature that differs from the stored one drops every derived table and
// folds the whole log again.
func Open(ctx context.Context, path string, opts Options) (*DB, error) {
	if opts.Clock == nil {
		opts.Clock = SystemClock{}
	}
	db := &DB{opts: opts, log: opts.Log, folds: map[string][]FoldFunc{}, watch: map[string]bool{}, foldedWait: make(chan struct{})}
	seenTable := map[string]bool{}
	for _, d := range opts.Domains {
		for _, k := range d.Watch {
			db.watch[k] = true
		}
		for kind, f := range d.Folds {
			db.folds[kind] = append(db.folds[kind], f)
		}
		for _, t := range d.Tables {
			if seenTable[t] {
				return nil, fmt.Errorf("core: table %s owned by two domains", t)
			}
			seenTable[t] = true
			db.tables = append(db.tables, t)
		}
	}

	write, err := sql.Open(driverName, sqlitex.DSN(path, writeStmts))
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)
	db.write = write
	if err := db.setup(ctx); err != nil {
		write.Close()
		return nil, err
	}

	read, err := sql.Open(readDriverName, sqlitex.DSN(path, readStmts))
	if err != nil {
		write.Close()
		return nil, err
	}
	read.SetMaxOpenConns(4)
	read.SetMaxIdleConns(4)
	if err := read.PingContext(ctx); err != nil {
		read.Close()
		write.Close()
		return nil, err
	}
	db.read = read

	w, err := startWriter(ctx, db)
	if err != nil {
		read.Close()
		write.Close()
		return nil, err
	}
	db.w = w
	return db, nil
}

func (db *DB) setup(ctx context.Context) error {
	for _, pragma := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = FULL`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA mmap_size = 0`,
		// a checkpoint syncs the whole file: every 4 MB of history (the
		// default) spent more on syncing than on folding
		`PRAGMA wal_autocheckpoint = 16384`,
	} {
		if _, err := db.write.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	var version int
	if err := db.write.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("core: schema version %d is newer than %d", version, schemaVersion)
	}

	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		// autoincrement: a seq is never handed out twice, even if the newest
		// row were ever deleted.
		`CREATE TABLE IF NOT EXISTS inputs (
			seq  INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			v    INTEGER NOT NULL,
			at   INTEGER NOT NULL,
			head TEXT,
			body BLOB
		)`,
		`CREATE TABLE IF NOT EXISTS core_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		// an input whose fold failed. it is skipped, not retried, so one bad
		// input cannot wedge every input after it; a rebuild tries it again.
		`CREATE TABLE IF NOT EXISTS fold_failures (
			seq   INTEGER PRIMARY KEY,
			kind  TEXT NOT NULL,
			error TEXT NOT NULL
		)`,
		fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion),
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, stmt := range db.opts.LogIndexes {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("core: log index: %w", err)
		}
	}

	sig := signature(db.opts.Domains)
	stored, err := metaGet(ctx, tx, "fold_signature")
	if err != nil {
		return err
	}
	if stored != sig || db.opts.Rebuild {
		if err := db.rebuildTx(ctx, tx, sig); err != nil {
			return err
		}
	}

	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM inputs`).Scan(&last); err != nil {
		return err
	}
	folded, err := metaGet(ctx, tx, "folded_seq")
	if err != nil {
		return err
	}
	var f int64
	if folded != "" {
		fmt.Sscan(folded, &f)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	db.appended.Store(last.Int64)
	db.folded.Store(f)
	return nil
}

// rebuildTx drops the derived tables of the previous signature and of this
// one, creates this one's, and rewinds folding to the start of the log.
func (db *DB) rebuildTx(ctx context.Context, tx *sql.Tx, sig string) error {
	old, err := metaGet(ctx, tx, "derived_tables")
	if err != nil {
		return err
	}
	var drop []string
	if old != "" {
		if err := json.Unmarshal([]byte(old), &drop); err != nil {
			return fmt.Errorf("core: derived_tables: %w", err)
		}
	}
	drop = append(drop, db.tables...)
	for _, t := range drop {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoteIdent(t)); err != nil {
			return err
		}
	}
	for _, d := range db.opts.Domains {
		for _, stmt := range d.Schema {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("core: domain %s schema: %w", d.Name, err)
			}
		}
	}
	tables, _ := json.Marshal(db.tables)
	if _, err := tx.ExecContext(ctx, `DELETE FROM fold_failures`); err != nil {
		return err
	}
	for k, v := range map[string]string{"fold_signature": sig, "derived_tables": string(tables), "folded_seq": "0"} {
		if err := metaSet(ctx, tx, k, v); err != nil {
			return err
		}
	}
	db.log.Info().Str("signature", sig).Msg("core: derived tables rebuilt, folding the log again")
	return nil
}

// Close stops the writer once the appends already handed to it are
// committed. folding left undone resumes at the next open.
func (db *DB) Close() error {
	db.w.close()
	return errors.Join(db.read.Close(), db.write.Close())
}

// Read is the reader pool. it sees folded state only, never a fold in flight.
func (db *DB) Read() *sql.DB { return db.read }

// Progress is the last folded seq and the last appended one. folded below
// appended means the writer is catching up, after a rebuild for instance.
func (db *DB) Progress() (folded, appended int64) {
	return db.folded.Load(), db.appended.Load()
}

// WaitFolded returns once every input up to seq is folded.
func (db *DB) WaitFolded(ctx context.Context, seq int64) error {
	for {
		db.foldedMu.Lock()
		wait := db.foldedWait
		db.foldedMu.Unlock()
		if db.folded.Load() >= seq {
			return nil
		}
		select {
		case <-wait:
		case <-db.w.done:
			if db.folded.Load() >= seq {
				return nil
			}
			return ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (db *DB) setFolded(seq int64) {
	db.folded.Store(seq)
	db.foldedMu.Lock()
	close(db.foldedWait)
	db.foldedWait = make(chan struct{})
	db.foldedMu.Unlock()
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func metaGet(ctx context.Context, q execer, key string) (string, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT value FROM core_meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func metaSet(ctx context.Context, q execer, key, value string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO core_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
