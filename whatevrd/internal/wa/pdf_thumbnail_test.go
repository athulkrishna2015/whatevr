package wa

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
)

// minimalPDF is a valid one-page PDF pdftoppm can rasterize.
const minimalPDF = `%PDF-1.4
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]/Contents 4 0 R/Resources<<>>>>endobj
4 0 obj<</Length 44>>stream
BT /F1 12 Tf 10 10 Td (hi) Tj ET
endstream
endobj
trailer<</Root 1 0 R>>
`

func TestRenderPDFThumbnail(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not on PATH")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(pdfPath, []byte(minimalPDF), 0o600); err != nil {
		t.Fatalf("write test pdf: %v", err)
	}
	c := &Client{paths: app.Paths{MediaCacheDir: dir}, log: waLog.Noop}
	thumb := c.renderPDFThumbnail(context.Background(), "chat", "msg:1", pdfPath)
	if thumb == "" {
		t.Fatal("expected a thumbnail path, got empty")
	}
	info, err := os.Stat(thumb)
	if err != nil || info.Size() == 0 {
		t.Fatalf("thumbnail missing or empty: %v", info)
	}
}

func TestIsPDFMime(t *testing.T) {
	if !isPDFMime("application/pdf") || !isPDFMime("Application/PDF; charset=binary") {
		t.Fatal("expected PDF mime to match")
	}
	if isPDFMime("application/ogg") || isPDFMime("text/plain") || isPDFMime("") {
		t.Fatal("expected non-PDF mime to be rejected")
	}
}
