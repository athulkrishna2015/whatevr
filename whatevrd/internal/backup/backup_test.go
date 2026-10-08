package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testPaths(t *testing.T) Sources {
	t.Helper()
	base := t.TempDir()
	paths := Sources{
		DataDir:   filepath.Join(base, "data"),
		CoreDB:    filepath.Join(base, "data", "whatevrd.db"),
		SessionDB: filepath.Join(base, "data", "session", "whatsmeow.db"),
		MediaDir:  filepath.Join(base, "cache", "media"),
	}
	for _, dir := range []string{paths.DataDir, filepath.Dir(paths.SessionDB), paths.MediaDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	seedSQLite := func(path, table string) {
		db, err := sql.Open("sqlite3", path)
		if err != nil {
			t.Fatalf("open seed db: %v", err)
		}
		defer db.Close()
		if _, err := db.Exec(`CREATE TABLE t (v TEXT)`); err != nil {
			t.Fatalf("seed schema: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO t (v) VALUES ('hello')`); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	seedSQLite(paths.CoreDB, "t")
	seedSQLite(paths.SessionDB, "t")
	if err := os.WriteFile(filepath.Join(paths.MediaDir, "pic.jpg"), []byte("fake-bytes"), 0o600); err != nil {
		t.Fatalf("seed media: %v", err)
	}
	return paths
}

// TestBackupExportRestoreRoundTrip locks in the bundle lifecycle: plain export
// restores bit-for-bit, and an encrypted export refuses the wrong passphrase
// while accepting the right one.
func TestBackupExportRestoreRoundTrip(t *testing.T) {
	ctx := t.Context()
	t.Run("plain", func(t *testing.T) {
		paths := testPaths(t)
		dest := filepath.Join(t.TempDir(), "plain.tar.gz")
		size, err := Export(ctx, paths, dest, nil)
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		if size <= 0 {
			t.Fatalf("export size = %d, want > 0", size)
		}

		// Wipe everything, then restore and compare.
		os.Remove(paths.CoreDB)
		os.Remove(paths.SessionDB)
		os.RemoveAll(paths.MediaDir)
		if err := Restore(paths, dest, nil); err != nil {
			t.Fatalf("restore: %v", err)
		}
		for _, path := range []string{paths.CoreDB, paths.SessionDB, filepath.Join(paths.MediaDir, "pic.jpg")} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("restored file missing %s: %v", path, err)
			}
		}
		media, err := os.ReadFile(filepath.Join(paths.MediaDir, "pic.jpg"))
		if err != nil || string(media) != "fake-bytes" {
			t.Fatalf("restored media = %q, %v; want fake-bytes", media, err)
		}
		db, err := sql.Open("sqlite3", "file:"+paths.CoreDB+"?mode=ro")
		if err != nil {
			t.Fatalf("open restored db: %v", err)
		}
		defer db.Close()
		var value string
		if err := db.QueryRow(`SELECT v FROM t`).Scan(&value); err != nil || value != "hello" {
			t.Fatalf("restored row = %q, %v; want hello", value, err)
		}
	})

	t.Run("encrypted", func(t *testing.T) {
		paths := testPaths(t)
		dest := filepath.Join(t.TempDir(), "enc.bin")
		if _, err := Export(ctx, paths, dest, []byte("s3cret")); err != nil {
			t.Fatalf("export: %v", err)
		}
		raw, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("read bundle: %v", err)
		}
		if !isEncryptedBundle(raw) {
			t.Fatal("encrypted bundle lacks the magic header")
		}
		if err := Restore(paths, dest, []byte("wrong")); err == nil {
			t.Fatal("restore with wrong passphrase must fail")
		}
		os.Remove(paths.CoreDB)
		if err := Restore(paths, dest, []byte("s3cret")); err != nil {
			t.Fatalf("restore with right passphrase: %v", err)
		}
		if _, err := os.Stat(paths.CoreDB); err != nil {
			t.Fatalf("restored db missing: %v", err)
		}
	})
}
