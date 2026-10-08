package whatsapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyToDestinationGuardsAndCopies(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(src, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "sub", "out.bin")
	if err := copyToDestination(src, dest); err == nil {
		t.Fatal("missing parent dir copied")
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyToDestination(src, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "bytes" {
		t.Fatalf("copied %q %v", got, err)
	}
	link := filepath.Join(dir, "sub", "link.bin")
	if err := os.Symlink(dest, link); err != nil {
		t.Fatal(err)
	}
	if err := copyToDestination(src, link); err == nil {
		t.Fatal("symlink destination copied")
	}
	if err := copyToDestination(filepath.Join(dir, "missing.bin"), dest); err == nil {
		t.Fatal("missing source copied")
	}
}
