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

// touchAllAt is how many ids of one kind a commit lists before it gives up and
// says the whole kind changed. a history chunk touches thousands.
const touchAllAt = 256

// Touch records that the item kind/id changed, for the views that show it.
func (t *Tx) Touch(kind, id string) { t.cur = append(t.cur, touch{kind: kind, id: id}) }

// TouchAll says every item of kind may have changed.
func (t *Tx) TouchAll(kind string) { t.cur = append(t.cur, touch{kind: kind, all: true}) }

func (t *Tx) keep() {
	for _, c := range t.cur {
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

func newTx(ctx context.Context, tx *sql.Tx) *Tx {
	return &Tx{ctx: ctx, tx: tx, touched: map[string]map[string]struct{}{}, all: map[string]bool{}}
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
