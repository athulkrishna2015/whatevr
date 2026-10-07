package core

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResetEmptiesTheLogAndKeepsABackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "core.db")
	db := open(t, path, Options{})
	appendAll(t, db, []Input{noteInput(note{"x", 1, "a"}), noteInput(note{"y", 1, "a"})})
	settle(t, db)

	backup := filepath.Join(dir, "before.db")
	if err := db.Reset(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	if got := dump(t, db)["notes"]; len(got) != 0 {
		t.Fatalf("notes after reset %v", got)
	}
	if ins, err := db.Inputs(context.Background(), 0, 10); err != nil || len(ins) != 0 {
		t.Fatalf("inputs after reset %v %v", ins, err)
	}
	// seqs go on from where they were, never handed out twice
	if seq := appendAll(t, db, []Input{noteInput(note{"z", 1, "c"})}); seq != 3 {
		t.Fatalf("first seq after reset %d", seq)
	}
	settle(t, db)
	if got := dump(t, db)["notes"]; !reflect.DeepEqual(got, []string{"id=z t=1 text=c"}) {
		t.Fatalf("notes %v", got)
	}
	db.Close()

	old := open(t, backup, Options{})
	defer old.Close()
	settle(t, old)
	if got := dump(t, old)["notes"]; len(got) != 2 {
		t.Fatalf("backup notes %v", got)
	}
	again := open(t, path, Options{})
	defer again.Close()
	settle(t, again)
	if got := dump(t, again)["notes"]; !reflect.DeepEqual(got, []string{"id=z t=1 text=c"}) {
		t.Fatalf("reopened notes %v", got)
	}
}

func TestHealthKeepsTheFirstFailureUntilItWorks(t *testing.T) {
	var h health
	h.set(&h.fold, errors.New("disk I/O error"))
	first := h.fold.At
	h.set(&h.fold, errors.New("database is locked"))
	if h.fold.At != first || h.fold.Error != "database is locked" {
		t.Fatalf("fold %+v", h.fold)
	}
	h.set(&h.fold, nil)
	if h.fold != nil {
		t.Fatalf("fold still failing after it worked: %+v", h.fold)
	}
}
