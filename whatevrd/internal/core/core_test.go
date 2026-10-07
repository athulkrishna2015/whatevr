package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// note is a stand-in domain until real folds exist: per id, the highest
// (t, text) wins, the same rule the real facts use.
type note struct {
	ID   string `json:"id"`
	T    int    `json:"t"`
	Text string `json:"text"`
}

func noteDomain(version int, table string) Domain {
	return Domain{
		Name:    "note",
		Version: version,
		Tables:  []string{table},
		Schema:  []string{`CREATE TABLE ` + table + ` (id TEXT PRIMARY KEY, t INTEGER NOT NULL, text TEXT NOT NULL)`},
		Folds: map[string]FoldFunc{"note": func(tx *Tx, in Input) error {
			var n note
			if err := json.Unmarshal(in.Head, &n); err != nil {
				return err
			}
			switch n.Text {
			case "boom":
				return errors.New("boom")
			case "panic":
				panic("fold panicked")
			}
			if _, err := tx.Exec(`INSERT INTO `+table+` (id, t, text) VALUES (?, ?, ?)
				ON CONFLICT (id) DO UPDATE SET t = excluded.t, text = excluded.text
				WHERE (excluded.t, excluded.text) > (`+table+`.t, `+table+`.text)`, n.ID, n.T, n.Text); err != nil {
				return err
			}
			tx.Touch("note", n.ID)
			return nil
		}},
	}
}

func noteInput(n note) Input {
	head, _ := json.Marshal(n)
	return Input{Kind: "note", V: 1, Head: head}
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func open(t *testing.T, path string, opts Options) *DB {
	t.Helper()
	if opts.Domains == nil {
		opts.Domains = []Domain{noteDomain(1, "notes")}
	}
	opts.Log = zerolog.Nop()
	db, err := Open(context.Background(), path, opts)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func appendAll(t *testing.T, db *DB, ins []Input) int64 {
	t.Helper()
	var last int64
	for _, in := range ins {
		seq, err := db.Append(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		last = seq
	}
	return last
}

func settle(t *testing.T, db *DB) {
	t.Helper()
	_, appended := db.Progress()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.WaitFolded(ctx, appended); err != nil {
		t.Fatal(err)
	}
}

func dump(t *testing.T, db *DB) map[string][]string {
	t.Helper()
	d, err := db.DumpDerived(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func randomNotes(r *rand.Rand, n int) []Input {
	texts := []string{"a", "b", "c", "d"}
	ins := make([]Input, n)
	for i := range ins {
		ins[i] = noteInput(note{ID: fmt.Sprint("n", r.IntN(20)), T: r.IntN(10), Text: texts[r.IntN(len(texts))]})
	}
	return ins
}

func TestAppendIsReadableAndFolds(t *testing.T) {
	var mu sync.Mutex
	var changes []Change
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{
		Clock:    fixedClock{at},
		OnChange: func(c Change) { mu.Lock(); changes = append(changes, c); mu.Unlock() },
	})
	defer db.Close()

	seq := appendAll(t, db, []Input{noteInput(note{"x", 1, "a"}), noteInput(note{"y", 1, "a"}), noteInput(note{"x", 2, "b"})})
	if seq != 3 {
		t.Fatalf("seq %d", seq)
	}
	ins, err := db.Inputs(context.Background(), 0, 10)
	if err != nil || len(ins) != 3 || !ins[0].At.Equal(at) || ins[2].Kind != "note" {
		t.Fatalf("inputs %v %v", ins, err)
	}
	settle(t, db)
	want := []string{"id=x t=2 text=b", "id=y t=1 text=a"}
	if got := dump(t, db)["notes"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[string]bool{}
	for _, c := range changes {
		for _, id := range c.Keys["note"] {
			seen[id] = true
		}
	}
	if !seen["x"] || !seen["y"] || changes[len(changes)-1].Through != 3 {
		t.Fatalf("changes %+v", changes)
	}
}

func TestAnyOrderFoldsTheSame(t *testing.T) {
	for seed := range uint64(20) {
		r := rand.New(rand.NewPCG(seed, 1))
		ins := randomNotes(r, 200)
		shuffled := append([]Input(nil), ins...)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		// and every input twice, as a redelivery would
		doubled := append(append([]Input(nil), shuffled...), ins...)

		dir := t.TempDir()
		var dumps []map[string][]string
		for i, list := range [][]Input{ins, shuffled, doubled} {
			db := open(t, filepath.Join(dir, fmt.Sprint(i, ".db")), Options{})
			appendAll(t, db, list)
			settle(t, db)
			dumps = append(dumps, dump(t, db))
			db.Close()
		}
		if !reflect.DeepEqual(dumps[0], dumps[1]) || !reflect.DeepEqual(dumps[0], dumps[2]) {
			t.Fatalf("seed %d: order or duplicates changed the result:\n%v\n%v\n%v", seed, dumps[0], dumps[1], dumps[2])
		}
	}
}

func TestRebuildEqualsIncremental(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 7))
	ins := randomNotes(r, 400)
	path := filepath.Join(t.TempDir(), "core.db")
	for chunk := range 4 {
		db := open(t, path, Options{})
		appendAll(t, db, ins[chunk*100:(chunk+1)*100])
		db.Close() // folding left over resumes at the next open
	}
	db := open(t, path, Options{})
	settle(t, db)
	incremental := dump(t, db)
	db.Close()

	// a new domain version drops the old table and folds the log again
	db = open(t, path, Options{Domains: []Domain{noteDomain(2, "notes_v2")}})
	defer db.Close()
	settle(t, db)
	rebuilt := dump(t, db)
	if !reflect.DeepEqual(incremental["notes"], rebuilt["notes_v2"]) {
		t.Fatalf("rebuild differs:\n%v\n%v", incremental["notes"], rebuilt["notes_v2"])
	}
	var n int
	db.Read().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'notes'`).Scan(&n)
	if n != 0 {
		t.Fatal("the old domain's table survived the rebuild")
	}
}

func TestRebuildOnAskEqualsIncremental(t *testing.T) {
	r := rand.New(rand.NewPCG(8, 8))
	path := filepath.Join(t.TempDir(), "core.db")
	db := open(t, path, Options{})
	appendAll(t, db, randomNotes(r, 200))
	settle(t, db)
	before := dump(t, db)
	db.Close()

	db = open(t, path, Options{Rebuild: true})
	defer db.Close()
	if folded, appended := db.Progress(); folded != 0 || appended == 0 {
		t.Fatalf("progress %d/%d at open, want the log folded from the start", folded, appended)
	}
	settle(t, db)
	if after := dump(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("rebuild differs:\n%v\n%v", before, after)
	}
}

func TestFoldResumesFromLastFoldedSeq(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.db")
	db := open(t, path, Options{})
	appendAll(t, db, []Input{noteInput(note{"x", 1, "a"})})
	settle(t, db)
	db.Close()

	// a crash after the append and before the fold: rows in the log the
	// derived tables never saw, and a folded_seq behind them
	raw, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []note{{"x", 3, "c"}, {"z", 1, "a"}} {
		head, _ := json.Marshal(n)
		if _, err := raw.Exec(`INSERT INTO inputs (kind, v, at, head) VALUES ('note', 1, 0, ?)`, string(head)); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	db = open(t, path, Options{})
	defer db.Close()
	if folded, appended := db.Progress(); folded != 1 || appended != 3 {
		t.Fatalf("progress %d %d", folded, appended)
	}
	settle(t, db)
	want := []string{"id=x t=3 text=c", "id=z t=1 text=a"}
	if got := dump(t, db)["notes"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestFailedFoldIsSkippedAndRecorded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.db")
	db := open(t, path, Options{})
	appendAll(t, db, []Input{
		noteInput(note{"x", 1, "a"}),
		noteInput(note{"y", 9, "boom"}),
		noteInput(note{"z", 9, "panic"}),
		noteInput(note{"w", 1, "a"}),
	})
	settle(t, db)
	if got := dump(t, db)["notes"]; !reflect.DeepEqual(got, []string{"id=w t=1 text=a", "id=x t=1 text=a"}) {
		t.Fatalf("got %v", got)
	}
	failures, err := db.FoldFailures(context.Background())
	if err != nil || len(failures) != 2 || failures[2] != "boom" || failures[3] == "" {
		t.Fatalf("failures %v %v", failures, err)
	}
	db.Close()

	// a rebuild tries them again
	db = open(t, path, Options{Domains: []Domain{noteDomain(2, "notes")}})
	defer db.Close()
	settle(t, db)
	if failures, _ := db.FoldFailures(context.Background()); len(failures) != 2 {
		t.Fatalf("after rebuild %v", failures)
	}
}

func TestConcurrentAppendsGetEverySeqOnce(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{})
	defer db.Close()
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int64]bool{}
	for g := range 50 {
		wg.Go(func() {
			for i := range 20 {
				seq, err := db.Append(context.Background(), noteInput(note{fmt.Sprint(g), i, "a"}))
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				if seen[seq] {
					t.Errorf("seq %d twice", seq)
				}
				seen[seq] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(seen) != 1000 {
		t.Fatalf("%d seqs", len(seen))
	}
	settle(t, db)
	if got := dump(t, db)["notes"]; len(got) != 50 {
		t.Fatalf("%d rows", len(got))
	}
}

func TestBigBatchTouchesTheWholeKind(t *testing.T) {
	var mu sync.Mutex
	all := false
	domain := Domain{Name: "wide", Version: 1, Folds: map[string]FoldFunc{"wide": func(tx *Tx, in Input) error {
		for i := range touchAllAt + 1 {
			tx.Touch("note", fmt.Sprint(i))
		}
		return nil
	}}}
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{
		Domains:  []Domain{domain},
		OnChange: func(c Change) { mu.Lock(); all = all || c.All["note"]; mu.Unlock() },
	})
	defer db.Close()
	appendAll(t, db, []Input{{Kind: "wide"}})
	settle(t, db)
	mu.Lock()
	defer mu.Unlock()
	if !all {
		t.Fatal("a commit touching more than touchAllAt ids did not touch the whole kind")
	}
}

func TestAppendAfterClose(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{})
	db.Close()
	if _, err := db.Append(context.Background(), noteInput(note{"x", 1, "a"})); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestAppendsDoNotWaitBehindAFoldBacklog(t *testing.T) {
	slow := Domain{Name: "slow", Version: 1, Folds: map[string]FoldFunc{"slow": func(tx *Tx, in Input) error {
		time.Sleep(time.Millisecond)
		return nil
	}}}
	path := filepath.Join(t.TempDir(), "core.db")
	open(t, path, Options{Domains: []Domain{slow}}).Close()
	// about two seconds of folding waiting at open, as after a rebuild
	raw, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 2000)
		INSERT INTO inputs (kind, v, at) SELECT 'slow', 1, 0 FROM n`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db := open(t, path, Options{Domains: []Domain{slow}})
	defer db.Close()
	for range 5 {
		start := time.Now()
		appendAll(t, db, []Input{{Kind: "fast"}})
		if d := time.Since(start); d > 500*time.Millisecond {
			t.Fatalf("an append waited %v behind folding", d)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if folded, appended := db.Progress(); folded >= appended {
		t.Fatal("the backlog was folded before the appends, the test proved nothing")
	}
}

func TestAppendBatchLandsWholeAndInOrder(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{})
	defer db.Close()
	seqs, err := db.AppendBatch(context.Background(), []Input{noteInput(note{"x", 1, "a"}), noteInput(note{"y", 1, "a"}), noteInput(note{"x", 2, "b"})})
	if err != nil || !reflect.DeepEqual(seqs, []int64{1, 2, 3}) {
		t.Fatalf("seqs %v %v", seqs, err)
	}
	if _, err := db.AppendBatch(context.Background(), []Input{noteInput(note{"z", 1, "a"}), {}}); err == nil {
		t.Fatal("a batch with a kindless input was taken")
	}
	if _, appended := db.Progress(); appended != 3 {
		t.Fatalf("a refused batch moved the log to %d", appended)
	}
}

// TestFinishSeesEveryTouchedID: a summary over 600 notes gets all 600 ids,
// past where a Change gives up and says "all", in the commit that made them.
func TestFinishSeesEveryTouchedID(t *testing.T) {
	notes := noteDomain(1, "notes")
	var mu sync.Mutex
	seen := map[string]bool{}
	sum := Domain{
		Name:    "sum",
		Version: 1,
		Tables:  []string{"note_count"},
		Schema:  []string{`CREATE TABLE note_count (n INTEGER NOT NULL)`, `INSERT INTO note_count VALUES (0)`},
		Watch:   []string{"note"},
		Finish: func(tx *Tx, touched map[string][]string, all map[string]bool) error {
			mu.Lock()
			for _, id := range touched["note"] {
				seen[id] = true
			}
			mu.Unlock()
			tx.Touch("sum", "")
			_, err := tx.Exec(`UPDATE note_count SET n = (SELECT COUNT(*) FROM notes)`)
			return err
		},
	}
	var changes []Change
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{Domains: []Domain{notes, sum},
		OnChange: func(c Change) { mu.Lock(); changes = append(changes, c); mu.Unlock() }})
	defer db.Close()
	ins := make([]Input, 600)
	for i := range ins {
		ins[i] = noteInput(note{ID: fmt.Sprint("n", i), T: 1, Text: "a"})
	}
	if _, err := db.AppendBatch(context.Background(), ins); err != nil {
		t.Fatal(err)
	}
	settle(t, db)
	var n int
	if err := db.Read().QueryRow(`SELECT n FROM note_count`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if n != 600 || len(seen) != 600 {
		t.Fatalf("count %d, finish saw %d ids, want 600 and 600", n, len(seen))
	}
	for _, c := range changes {
		if len(c.Keys["sum"]) == 0 {
			t.Fatalf("a commit without the summary's touch: %+v", c.Keys)
		}
	}
}

// TestFinishFailingFailsTheCommit: nothing of a batch lands when its summary
// cannot be kept, and folding resumes once it can.
func TestFinishFailingFailsTheCommit(t *testing.T) {
	notes := noteDomain(1, "notes")
	var fail sync.Map
	fail.Store("on", true)
	sum := Domain{
		Name: "sum", Version: 1, Watch: []string{"note"},
		Finish: func(tx *Tx, touched map[string][]string, all map[string]bool) error {
			if v, _ := fail.Load("on"); v == true {
				panic("summary broke")
			}
			return nil
		},
	}
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{Domains: []Domain{notes, sum}})
	defer db.Close()
	seq := appendAll(t, db, []Input{noteInput(note{ID: "a", T: 1, Text: "x"})})
	time.Sleep(100 * time.Millisecond)
	if folded, _ := db.Progress(); folded >= seq {
		t.Fatalf("folded %d with a failing finish", folded)
	}
	if h := db.Health(); h.Fold == nil {
		t.Fatal("health says the fold works")
	}
	fail.Store("on", false)
	settle(t, db)
	var n int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("notes %d, %v", n, err)
	}
}

// TestFinishWithoutWatchRunsEveryCommit: a Finish that watches nothing runs
// once per fold commit, however little the batch touched.
func TestFinishWithoutWatchRunsEveryCommit(t *testing.T) {
	notes := noteDomain(1, "notes")
	var mu sync.Mutex
	runs := 0
	every := Domain{
		Name: "every", Version: 1,
		Finish: func(tx *Tx, touched map[string][]string, all map[string]bool) error {
			mu.Lock()
			runs++
			mu.Unlock()
			return nil
		},
	}
	db := open(t, filepath.Join(t.TempDir(), "core.db"), Options{Domains: []Domain{notes, every}})
	defer db.Close()
	for i := range 3 {
		if _, err := db.Append(context.Background(), noteInput(note{ID: fmt.Sprint("n", i), T: 1, Text: "a"})); err != nil {
			t.Fatal(err)
		}
		settle(t, db)
	}
	mu.Lock()
	defer mu.Unlock()
	if runs < 3 {
		t.Fatalf("finish ran %d times over 3 commits", runs)
	}
}

func TestOnlyOneOpenHoldsAFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	db, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, path, Options{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("a second open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatalf("open after close: %v", err)
	}
	again.Close()
}
