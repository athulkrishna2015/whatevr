package model

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"whatevrd/internal/core"
)

// IDIndex is the log index the id registry reads through. it goes into
// core.Options.LogIndexes.
const IDIndex = `CREATE INDEX IF NOT EXISTS inputs_person_id ON inputs (seq) WHERE kind = 'person_id'`

// IDs is the id every address was given the first time it was shown, read
// straight from the log, so it survives restarts and rebuilds without a fold.
// an id belongs to whoever had the address when it was given, so a recycled
// number's id stays with its old owner. a person or chat is shown under the
// oldest (t, id) that belongs to them; learning that two addresses are one
// person picks between their ids, it never makes a new one.
type IDs struct {
	db    *core.DB
	newID func() string

	mu     sync.RWMutex
	byAddr map[string]assigned
	byID   map[string]given
}

type assigned struct {
	id string
	t  int64
}

type given struct {
	addr string
	t    int64
}

func (a assigned) older(b assigned) bool {
	return a.t < b.t || a.t == b.t && a.id < b.id
}

// LoadIDs reads every id in the log.
func LoadIDs(ctx context.Context, db *core.DB) (*IDs, error) {
	ids := &IDs{db: db, newID: randomID, byAddr: map[string]assigned{}, byID: map[string]given{}}
	rows, err := db.Read().QueryContext(ctx, `SELECT at, head FROM inputs WHERE kind = 'person_id' ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at int64
		var head sql.NullString
		if err := rows.Scan(&at, &head); err != nil {
			return nil, err
		}
		var h core.PersonIDHead
		if json.Unmarshal([]byte(head.String), &h) != nil || h.Addr == "" || h.ID == "" {
			continue
		}
		ids.add(h.Addr, assigned{id: h.ID, t: at})
	}
	return ids, rows.Err()
}

// add keeps the older of two ids one address got, which only happens when
// two daemons wrote the same log, or a crash lost the first answer.
func (ids *IDs) add(addr string, a assigned) {
	ids.byID[a.id] = given{addr: addr, t: a.t}
	if prev, ok := ids.byAddr[addr]; ok && !a.older(prev) {
		return
	}
	ids.byAddr[addr] = a
}

func randomID() string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("model: no randomness for an id: %v", err))
	}
	return strings.ToLower(base32.StdEncoding.EncodeToString(b[:]))
}

// Ensure gives every key that has no id one, in one append. a reader calls
// it before showing keys: the first time costs one durable commit, after that
// nothing.
func (ids *IDs) Ensure(ctx context.Context, w *World, keys ...string) error {
	ids.mu.RLock()
	missing := false
	for _, k := range keys {
		if k != "" && ids.of(w, k) == "" {
			missing = true
			break
		}
	}
	ids.mu.RUnlock()
	if !missing {
		return nil
	}
	// held across the append, so two readers never give one address two ids
	ids.mu.Lock()
	defer ids.mu.Unlock()
	var ins []core.Input
	var fresh []string
	seen := map[string]bool{}
	for _, a := range keys {
		if a == "" || seen[a] || ids.of(w, a) != "" {
			continue
		}
		seen[a] = true
		head, _ := json.Marshal(core.PersonIDHead{Addr: a, ID: ids.newID()})
		ins = append(ins, core.Input{Kind: core.KindPersonID, V: 1, Head: head})
		fresh = append(fresh, a)
	}
	if len(ins) == 0 {
		return nil
	}
	seqs, err := ids.db.AppendBatch(ctx, ins)
	if err != nil {
		return err
	}
	// the stamp is the log's, read back so a restart sees the same t
	rows, err := ids.db.Read().QueryContext(ctx, `SELECT seq, at FROM inputs WHERE seq BETWEEN ? AND ?`, seqs[0], seqs[len(seqs)-1])
	if err != nil {
		return err
	}
	defer rows.Close()
	at := map[int64]int64{}
	for rows.Next() {
		var seq, t int64
		if err := rows.Scan(&seq, &t); err != nil {
			return err
		}
		at[seq] = t
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i, a := range fresh {
		var h core.PersonIDHead
		_ = json.Unmarshal(ins[i].Head, &h)
		ids.add(a, assigned{id: h.ID, t: at[seqs[i]]})
	}
	return nil
}

// Of is the id key is shown under: the oldest id that belongs to it. "" when
// none of its addresses has one yet.
func (ids *IDs) Of(w *World, key string) string {
	ids.mu.RLock()
	defer ids.mu.RUnlock()
	return ids.of(w, key)
}

func (ids *IDs) of(w *World, key string) string {
	var best assigned
	for _, a := range w.Addrs(key) {
		x, ok := ids.byAddr[a]
		// given while the number was someone else's
		if !ok || w.Key(a, x.t) != key {
			continue
		}
		if best.id == "" || x.older(best) {
			best = x
		}
	}
	return best.id
}

// Key is the person or chat an id names now, and whether it names one. an id
// that lost to an older one still names the person it folded into.
func (ids *IDs) Key(w *World, id string) (string, bool) {
	ids.mu.RLock()
	defer ids.mu.RUnlock()
	g, ok := ids.byID[id]
	if !ok {
		return "", false
	}
	return w.Key(g.addr, g.t), true
}

// Current is the id that stands for what id named: id itself, or the one it
// folded into. "" for an id this daemon never gave.
func (ids *IDs) Current(w *World, id string) string {
	ids.mu.RLock()
	defer ids.mu.RUnlock()
	g, ok := ids.byID[id]
	if !ok {
		return ""
	}
	return ids.of(w, w.Key(g.addr, g.t))
}
