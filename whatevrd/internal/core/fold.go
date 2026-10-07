package core

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Domain is one slice of derived state: the tables it owns and how each input
// kind changes them. bump Version on any change to Schema or Folds; the store
// then rebuilds every derived table from the log.
type Domain struct {
	Name    string
	Version int
	Tables  []string
	Schema  []string
	Folds   map[string]FoldFunc
	// Finish runs once per fold commit, after every input of the batch, with
	// each id of the Watch kinds the batch touched (never capped like a
	// Change) and the kinds touched whole. it keeps tables that sum up many
	// rows, so they change in the same commit as the rows. with no Watch it
	// runs on every commit, with nothing touched.
	Watch  []string
	Finish func(tx *Tx, touched map[string][]string, all map[string]bool) error
}

// FoldFunc applies one input to the derived tables. it has to be
// deterministic and order independent: no clock, no network, no randomness,
// and the same set of inputs in any order must leave the same rows. folding
// an input twice must change nothing.
type FoldFunc func(tx *Tx, in Input) error

// Tx is the fold's view of the write transaction.
type Tx struct {
	ctx context.Context
	tx  *sql.Tx
	// cur holds what the input being folded touched. it joins the batch only
	// if that fold succeeds.
	cur     []touch
	touched map[string]map[string]struct{}
	all     map[string]bool
	// watched is every id of the kinds some Finish watches, uncapped
	watch      map[string]bool
	watched    map[string]map[string]struct{}
	watchedAll map[string]bool
}

type touch struct {
	kind, id string
	all      bool
}

func (t *Tx) Context() context.Context { return t.ctx }

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(t.ctx, query, args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(t.ctx, query, args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(t.ctx, query, args...)
}

// QueryContext and QueryRowContext let code written for a *sql.DB read
// inside the fold.
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

// touchAllAt is how many ids of one kind a commit lists before it gives up and
// says the whole kind changed. a history chunk touches thousands.
const touchAllAt = 256

// Touch records that the item kind/id changed, for the views that show it.
func (t *Tx) Touch(kind, id string) { t.cur = append(t.cur, touch{kind: kind, id: id}) }

// TouchAll says every item of kind may have changed.
func (t *Tx) TouchAll(kind string) { t.cur = append(t.cur, touch{kind: kind, all: true}) }

func (t *Tx) keep() {
	for _, c := range t.cur {
		if t.watch[c.kind] {
			switch {
			case c.all:
				t.watchedAll[c.kind] = true
			case t.watched[c.kind] == nil:
				t.watched[c.kind] = map[string]struct{}{c.id: {}}
			default:
				t.watched[c.kind][c.id] = struct{}{}
			}
		}
		if t.all[c.kind] {
			continue
		}
		ids := t.touched[c.kind]
		if c.all || len(ids) >= touchAllAt {
			delete(t.touched, c.kind)
			t.all[c.kind] = true
			continue
		}
		if ids == nil {
			ids = map[string]struct{}{}
			t.touched[c.kind] = ids
		}
		ids[c.id] = struct{}{}
	}
	t.cur = t.cur[:0]
}

func (t *Tx) drop() { t.cur = t.cur[:0] }

// Change is what one fold commit touched.
type Change struct {
	// Through is the last seq the commit folded.
	Through int64
	Keys    map[string][]string
	All     map[string]bool
}

func (c Change) Empty() bool { return len(c.Keys) == 0 && len(c.All) == 0 }

func newTx(ctx context.Context, tx *sql.Tx, watch map[string]bool) *Tx {
	return &Tx{ctx: ctx, tx: tx, touched: map[string]map[string]struct{}{}, all: map[string]bool{},
		watch: watch, watched: map[string]map[string]struct{}{}, watchedAll: map[string]bool{}}
}

// watchedCount is how many watched ids the batch has so far.
func (t *Tx) watchedCount() int {
	n := 0
	for _, ids := range t.watched {
		n += len(ids)
	}
	return n
}

// finish runs each domain's Finish over what the batch touched.
func (t *Tx) finish(domains []Domain) error {
	for _, d := range domains {
		if d.Finish == nil {
			continue
		}
		touched := map[string][]string{}
		all := map[string]bool{}
		for _, k := range d.Watch {
			if t.watchedAll[k] {
				all[k] = true
			}
			if ids := t.watched[k]; len(ids) > 0 {
				list := make([]string, 0, len(ids))
				for id := range ids {
					list = append(list, id)
				}
				sort.Strings(list)
				touched[k] = list
			}
		}
		if len(d.Watch) > 0 && len(touched) == 0 && len(all) == 0 {
			continue
		}
		if err := runFinish(d, t, touched, all); err != nil {
			t.drop()
			return fmt.Errorf("core: %s finish: %w", d.Name, err)
		}
		t.keep()
	}
	return nil
}

func runFinish(d Domain, t *Tx, touched map[string][]string, all map[string]bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return d.Finish(t, touched, all)
}

func (t *Tx) change(through int64) Change {
	c := Change{Through: through, Keys: map[string][]string{}, All: t.all}
	for kind, ids := range t.touched {
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		sort.Strings(list)
		c.Keys[kind] = list
	}
	return c
}

func signature(domains []Domain) string {
	parts := make([]string, len(domains))
	for i, d := range domains {
		parts[i] = fmt.Sprintf("%s:%d", d.Name, d.Version)
	}
	return strings.Join(parts, ",")
}
