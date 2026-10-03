package model

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"whatevrd/internal/core"
)

func openIDs(t *testing.T, path string) (*core.DB, *IDs) {
	t.Helper()
	db, err := core.Open(context.Background(), path, core.Options{Domains: Domains(), Log: zerolog.Nop(), LogIndexes: []string{IDIndex}})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := LoadIDs(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return db, ids
}

func TestIDsSurviveARestartAndARebuild(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	db, ids := openIDs(t, path)
	w, _ := NewReader(db.Read()).World(ctx)
	if err := ids.Ensure(ctx, w, "1@s.whatsapp.net", "2@g.us"); err != nil {
		t.Fatal(err)
	}
	a, g := ids.Of(w, "1@s.whatsapp.net"), ids.Of(w, "2@g.us")
	if a == "" || g == "" || a == g {
		t.Fatalf("ids %q %q", a, g)
	}
	// asking again changes nothing and appends nothing
	_, before := db.Progress()
	if err := ids.Ensure(ctx, w, "1@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if _, after := db.Progress(); after != before {
		t.Fatalf("a known address appended %d inputs", after-before)
	}
	db.Close()

	db, err := core.Open(ctx, path, core.Options{Domains: Domains(), Log: zerolog.Nop(), LogIndexes: []string{IDIndex}, Rebuild: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ids, err = LoadIDs(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if ids.Of(w, "1@s.whatsapp.net") != a || ids.Of(w, "2@g.us") != g {
		t.Fatal("ids changed over a restart and rebuild")
	}
}

func TestIDsFoldIntoTheOldest(t *testing.T) {
	ctx := context.Background()
	db, ids := openIDs(t, filepath.Join(t.TempDir(), "core.db"))
	defer db.Close()
	r := NewReader(db.Read())
	const pn, lid = "15550001@s.whatsapp.net", "900001@lid"
	w, _ := r.World(ctx)
	// shown by number first, then by lid as a stranger
	if err := ids.Ensure(ctx, w, pn); err != nil {
		t.Fatal(err)
	}
	if err := ids.Ensure(ctx, w, lid); err != nil {
		t.Fatal(err)
	}
	old, young := ids.Of(w, pn), ids.Of(w, lid)
	feed(t, db, []core.Input{in(core.KindLIDMapping, core.LIDMappingHead{LID: lid, PN: pn}, nil, at(5))})
	w, _ = r.World(ctx)
	if w.Now(pn) != lid {
		t.Fatalf("pn key %q", w.Now(pn))
	}
	if got := ids.Of(w, lid); got != old {
		t.Fatalf("merged id %q, want the older %q", got, old)
	}
	if got := ids.Current(w, young); got != old {
		t.Fatalf("the younger id points at %q, want %q", got, old)
	}
	if k, ok := ids.Key(w, young); !ok || k != lid {
		t.Fatalf("the younger id names %q %v", k, ok)
	}
}

func TestIDsNeverTwoForOneAddress(t *testing.T) {
	ctx := context.Background()
	db, ids := openIDs(t, filepath.Join(t.TempDir(), "core.db"))
	defer db.Close()
	w, _ := NewReader(db.Read()).World(ctx)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ids.Ensure(ctx, w, "7@s.whatsapp.net", "8@s.whatsapp.net"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	_, appended := db.Progress()
	if appended != 2 {
		t.Fatalf("%d inputs for two addresses", appended)
	}
}

func TestIDsStayWithANumbersOldOwner(t *testing.T) {
	ctx := context.Background()
	db, ids := openIDs(t, filepath.Join(t.TempDir(), "core.db"))
	defer db.Close()
	r := NewReader(db.Read())
	const pn, asha, bo = "15550002@s.whatsapp.net", "900002@lid", "900003@lid"
	w, _ := r.World(ctx)
	if err := ids.Ensure(ctx, w, pn); err != nil {
		t.Fatal(err)
	}
	p := ids.Of(w, pn)
	now := time.Now()
	feed(t, db, []core.Input{
		in(core.KindLIDMapping, core.LIDMappingHead{LID: asha, PN: pn}, nil, now.Add(time.Second)),
		in(core.KindLIDMapping, core.LIDMappingHead{LID: bo, PN: pn}, nil, now.Add(2*time.Second)),
	})
	w, _ = r.World(ctx)
	if w.Now(pn) != bo {
		t.Fatalf("pn key %q", w.Now(pn))
	}
	if got := ids.Of(w, asha); got != p {
		t.Fatalf("asha is %q, want the number's old id %q", got, p)
	}
	if got := ids.Of(w, bo); got != "" {
		t.Fatalf("bo took %q before being shown", got)
	}
	if err := ids.Ensure(ctx, w, bo); err != nil {
		t.Fatal(err)
	}
	b := ids.Of(w, bo)
	if b == "" || b == p {
		t.Fatalf("bo is %q, asha %q", b, p)
	}
	if k, _ := ids.Key(w, p); k != asha {
		t.Fatalf("the number's old id names %q", k)
	}
}
