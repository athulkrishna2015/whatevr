package core

import (
	"context"
	"database/sql"
	"fmt"
)

type resetReq struct {
	backup string
	done   chan error
}

// Reset empties the log and every derived table, for a logout: the next
// account starts from nothing. backup, when set, is where the store is
// copied first. seqs keep counting up from where they were.
func (db *DB) Reset(ctx context.Context, backup string) error {
	req := resetReq{backup: backup, done: make(chan error, 1)}
	select {
	case db.w.resets <- req:
	case <-db.w.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *writer) reset(backup string) error {
	if backup != "" {
		if _, err := w.conn.ExecContext(w.ctx, `VACUUM INTO ?`, backup); err != nil {
			return err
		}
	}
	if err := w.pragma("FULL"); err != nil {
		return err
	}
	tx, err := w.conn.BeginTx(w.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var last sql.NullInt64
	if err := tx.QueryRowContext(w.ctx, `SELECT seq FROM sqlite_sequence WHERE name = 'inputs'`).Scan(&last); err != nil && err != sql.ErrNoRows {
		return err
	}
	if _, err := tx.ExecContext(w.ctx, `DELETE FROM inputs`); err != nil {
		return err
	}
	if err := w.db.rebuildTx(w.ctx, tx, signature(w.db.opts.Domains)); err != nil {
		return err
	}
	if err := metaSet(w.ctx, tx, "folded_seq", fmt.Sprint(last.Int64)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	w.db.appended.Store(last.Int64)
	w.db.setFolded(last.Int64)
	if w.db.opts.OnChange != nil {
		all := map[string]bool{}
		for _, k := range []string{"chat", "message", "person", "pins", "sync", "sticker", "blocklist", "group", "appstate", "call"} {
			all[k] = true
		}
		w.db.opts.OnChange(Change{Through: last.Int64, All: all})
	}
	w.db.log.Info().Str("backup", backup).Msg("core: reset")
	return nil
}
