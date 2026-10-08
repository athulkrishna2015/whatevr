package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// foldMax, foldBytes and foldBudget cap one fold transaction, so
	// appends waiting behind it (and the acks waiting on those) never sit
	// out a whole history chunk, and a batch of big conversations is not
	// all in memory at once.
	foldMax    = 512
	foldBytes  = 4 << 20
	foldBudget = 50 * time.Millisecond
	// finishMax ends a batch once it touched this many ids a Finish
	// watches, so the summing up stays as short as the folding.
	finishMax = 512
	// drainMax caps how many queued appends share one commit.
	drainMax = 1024

	retryMin = time.Second
	retryMax = 30 * time.Second
)

type appendReq struct {
	ins  []Input
	done chan appendRes
}

type appendRes struct {
	seqs []int64
	err  error
}

// writer is the only goroutine that writes. it owns the one write connection
// and alternates between committing appends and folding, appends first.
type writer struct {
	db   *DB
	conn *sql.Conn
	ctx  context.Context
	// sync is the synchronous pragma the connection is on: appends commit
	// with FULL, folds with NORMAL. a lost fold is folded again, a lost
	// append is a message whose ack already went out.
	sync string

	appends chan appendReq
	resets  chan resetReq
	stop    chan struct{}
	done    chan struct{}

	retry time.Duration
}

func startWriter(ctx context.Context, db *DB) (*writer, error) {
	conn, err := db.write.Conn(ctx)
	if err != nil {
		return nil, err
	}
	w := &writer{
		db:      db,
		conn:    conn,
		ctx:     context.WithoutCancel(ctx),
		sync:    "FULL",
		appends: make(chan appendReq),
		resets:  make(chan resetReq),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go w.run()
	return w, nil
}

// Append logs in and returns its seq once the commit is durable. an error
// means it is not in the log, and whoever handed it over must not ack it.
func (db *DB) Append(ctx context.Context, in Input) (int64, error) {
	seqs, err := db.AppendBatch(ctx, []Input{in})
	if err != nil {
		return 0, err
	}
	return seqs[0], nil
}

// AppendBatch logs every input in one commit, in order: all of them land or
// none do.
func (db *DB) AppendBatch(ctx context.Context, ins []Input) ([]int64, error) {
	if len(ins) == 0 {
		return nil, nil
	}
	for _, in := range ins {
		if in.Kind == "" {
			return nil, errors.New("core: input without a kind")
		}
	}
	req := appendReq{ins: ins, done: make(chan appendRes, 1)}
	select {
	case db.w.appends <- req:
	case <-db.w.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case res := <-req.done:
		return res.seqs, res.err
	case <-ctx.Done():
		// it may still commit. the caller does not ack, whatsmeow hands it
		// over again, and the fold takes the duplicate as a no-op.
		return nil, ctx.Err()
	}
}

func (w *writer) run() {
	defer close(w.done)
	defer w.conn.Close()
	var wait <-chan time.Time
	for {
		pending := w.db.folded.Load() < w.db.appended.Load()
		if pending && wait == nil {
			select {
			case req := <-w.appends:
				w.commit(w.drain(req))
			case req := <-w.resets:
				req.done <- w.reset(req.backup)
			case <-w.stop:
				return
			default:
				err := w.fold()
				w.db.health.set(&w.db.health.fold, err)
				if err != nil {
					w.retry = min(max(w.retry*2, retryMin), retryMax)
					w.db.log.Error().Err(err).Dur("retry", w.retry).Msg("core: fold commit failed")
					wait = time.After(w.retry)
				} else {
					w.retry = 0
				}
			}
			continue
		}
		select {
		case req := <-w.appends:
			w.commit(w.drain(req))
		case req := <-w.resets:
			req.done <- w.reset(req.backup)
		case <-wait:
			wait = nil
		case <-w.stop:
			return
		}
	}
}

func (w *writer) close() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
}

func (w *writer) drain(first appendReq) []appendReq {
	batch := []appendReq{first}
	for len(batch) < drainMax {
		select {
		case req := <-w.appends:
			batch = append(batch, req)
		default:
			return batch
		}
	}
	return batch
}

func (w *writer) pragma(mode string) error {
	if w.sync == mode {
		return nil
	}
	if _, err := w.conn.ExecContext(w.ctx, `PRAGMA synchronous = `+mode); err != nil {
		return err
	}
	w.sync = mode
	return nil
}

// commit appends a batch in one transaction. it lands whole or not at all.
func (w *writer) commit(batch []appendReq) {
	seqs, err := w.commitTx(batch)
	w.db.health.set(&w.db.health.append, err)
	for i, req := range batch {
		if err != nil {
			req.done <- appendRes{err: err}
		} else {
			req.done <- appendRes{seqs: seqs[i]}
		}
	}
	if err != nil {
		w.db.log.Error().Err(err).Int("requests", len(batch)).Msg("core: append failed")
		return
	}
	last := seqs[len(seqs)-1]
	w.db.appended.Store(last[len(last)-1])
}

func (w *writer) commitTx(batch []appendReq) ([][]int64, error) {
	if err := w.pragma("FULL"); err != nil {
		return nil, err
	}
	tx, err := w.conn.BeginTx(w.ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(w.ctx, `INSERT INTO inputs (kind, v, at, head, body) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	now := w.db.opts.Clock.Now()
	seqs := make([][]int64, len(batch))
	for i, req := range batch {
		seqs[i] = make([]int64, len(req.ins))
		for j, in := range req.ins {
			at := in.At
			if at.IsZero() {
				at = now
			}
			var head any
			if len(in.Head) > 0 {
				head = string(in.Head)
			}
			res, err := stmt.ExecContext(w.ctx, in.Kind, in.V, at.UnixMilli(), head, in.Body)
			if err != nil {
				return nil, err
			}
			if seqs[i][j], err = res.LastInsertId(); err != nil {
				return nil, err
			}
		}
	}
	return seqs, tx.Commit()
}

// fold folds the next inputs after folded_seq in one transaction. each input
// runs under its own savepoint: one that fails is rolled back alone, recorded
// in fold_failures and skipped. an error out of here is the transaction
// itself failing (disk, i/o), and nothing in it counted.
func (w *writer) fold() error {
	if err := w.pragma("NORMAL"); err != nil {
		return err
	}
	start := time.Now()
	from := w.db.folded.Load()
	tx, err := w.conn.BeginTx(w.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	inputs, err := readInputs(w.ctx, tx, from, foldMax, foldBytes)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		// appended ran ahead of a failed append's batch; nothing to fold.
		// Leave appended alone: it is monotonic and only advances on
		// successful commits, so regressing it would mask writer lag from
		// Progress/WaitFolded. The next append advances it again.
		return nil
	}
	ftx := newTx(w.ctx, tx, w.db.watch)
	through := from
	for _, in := range inputs {
		if err := w.foldOne(ftx, in); err != nil {
			return err
		}
		through = in.Seq
		if time.Since(start) > foldBudget || ftx.watchedCount() >= finishMax {
			break
		}
	}
	if err := ftx.finish(w.db.opts.Domains); err != nil {
		return err
	}
	if err := metaSet(w.ctx, tx, "folded_seq", fmt.Sprint(through)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	w.db.setFolded(through)
	c := ftx.change(through)
	if e := w.db.log.Debug(); e.Enabled() {
		msgs := c.Keys["message"]
		e.Int64("from", from+1).Int64("input", through).Int("chats", len(c.Keys["chat"])).
			Strs("msg", msgs[:min(len(msgs), 32)]).Dur("dur", time.Since(start)).Msg("core: folded")
	}
	if w.db.opts.OnChange != nil && !c.Empty() {
		w.db.opts.OnChange(c)
	}
	return nil
}

func (w *writer) foldOne(t *Tx, in Input) error {
	folds := w.db.folds[in.Kind]
	if len(folds) == 0 {
		// no fold for this kind yet. when one is added its domain version
		// changes, and the rebuild picks the input up.
		return nil
	}
	if _, err := t.tx.ExecContext(w.ctx, `SAVEPOINT fold_one`); err != nil {
		return err
	}
	var foldErr error
	for _, f := range folds {
		if foldErr = runFold(f, t, in); foldErr != nil {
			break
		}
	}
	if foldErr == nil {
		t.keep()
		_, err := t.tx.ExecContext(w.ctx, `RELEASE fold_one`)
		return err
	}
	t.drop()
	if _, err := t.tx.ExecContext(w.ctx, `ROLLBACK TO fold_one`); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(w.ctx, `RELEASE fold_one`); err != nil {
		return err
	}
	w.db.log.Error().Err(foldErr).Int64("input", in.Seq).Str("kind", in.Kind).Msg("core: fold failed, input skipped")
	_, err := t.tx.ExecContext(w.ctx, `INSERT INTO fold_failures (seq, kind, error) VALUES (?, ?, ?)
		ON CONFLICT (seq) DO UPDATE SET error = excluded.error`, in.Seq, in.Kind, foldErr.Error())
	return err
}

// runFold turns a panicking fold into a failed one.
func runFold(f FoldFunc, t *Tx, in Input) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return f(t, in)
}

// FoldFailures lists the inputs whose fold failed since the last rebuild.
func (db *DB) FoldFailures(ctx context.Context) (map[int64]string, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT seq, error FROM fold_failures ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var seq int64
		var e string
		if err := rows.Scan(&seq, &e); err != nil {
			return nil, err
		}
		out[seq] = e
	}
	return out, rows.Err()
}

// Failure is the last time a part of the writer failed, until it next works.
type Failure struct {
	At    time.Time
	Error string
}

// Health is what is failing in the writer right now: appends (nothing new is
// logged) and fold commits (views stop moving). nil is working.
type Health struct {
	Append, Fold *Failure
}

type health struct {
	mu           sync.Mutex
	append, fold *Failure
}

func (h *health) set(slot **Failure, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil {
		*slot = nil
		return
	}
	if *slot == nil {
		*slot = &Failure{At: time.Now()}
	}
	(*slot).Error = err.Error()
}

func (db *DB) Health() Health {
	db.health.mu.Lock()
	defer db.health.mu.Unlock()
	var h Health
	if db.health.append != nil {
		a := *db.health.append
		h.Append = &a
	}
	if db.health.fold != nil {
		f := *db.health.fold
		h.Fold = &f
	}
	return h
}
