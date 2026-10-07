package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOldDaemonClearsOnlyItsOwnFiles(t *testing.T) {
	dir := t.TempDir()
	if oldDaemon(dir) != nil {
		t.Fatal("a data dir without the old daemon's core asks for a relink")
	}
	old := []string{"whatevrd.db", "whatevrd.db-wal", "whatevrd.db-shm", "whatevrd-before-logout-20260101.db", "whatevrd-before-logout-20260101.db-wal"}
	kept := []string{"whatevr.db", "whatevr-before-logout-20260101.db", "session/whatsmeow.db"}
	for _, f := range append(old, kept...) {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clear := oldDaemon(dir)
	if clear == nil {
		t.Fatal("the old daemon's core asks for no relink")
	}
	if err := clear(); err != nil {
		t.Fatal(err)
	}
	for _, f := range old {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s is still there", f)
		}
	}
	for _, f := range kept {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s went: %v", f, err)
		}
	}
	if err := clear(); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
}
