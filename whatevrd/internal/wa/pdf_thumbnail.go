package wa

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	appstore "whatevrd/internal/store"
)

// pdfThumbTimeout bounds first-page rendering; a huge or pathological PDF
// must never stall a send or a download worker.
const pdfThumbTimeout = 30 * time.Second

// pdfThumbMaxSourceBytes caps the PDFs we attempt to thumbnail: rendering a
// hundreds-of-MB scan is not worth a bubble preview.
const pdfThumbMaxSourceBytes = 64 * 1024 * 1024

// isPDFMime reports whether a stored/sniffed MIME type is a PDF. Parameters
// ("application/pdf; ...") are ignored, like mediaExtension does.
func isPDFMime(mimeType string) bool {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mimeType)), ";")
	return strings.TrimSpace(base) == "application/pdf"
}

// pdfThumbnailDims decodes just the JPEG header for display dimensions.
// Failures yield 0,0 and the bubble falls back to its fixed preview height.
func pdfThumbnailDims(jpg []byte) (int32, int32) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(jpg))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0
	}
	return int32(cfg.Width), int32(cfg.Height)
}

// renderPDFThumbnail renders a PDF's first page to a cached JPEG thumbnail
// and returns its local path, or "" when pdftoppm is missing/unusable. It is
// the document analogue of the video poster pipeline: entirely optional,
// never an error that fails a send or download.
func (c *Client) renderPDFThumbnail(ctx context.Context, chatID, messageID, pdfPath string) string {
	pdftoppm, err := exec.LookPath("pdftoppm")
	if err != nil {
		return ""
	}
	info, err := os.Stat(pdfPath)
	if err != nil || info.Size() <= 0 || info.Size() > pdfThumbMaxSourceBytes {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, pdfThumbTimeout)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "whatevr-pdfthumb")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmpDir)
	prefix := filepath.Join(tmpDir, "page")
	cmd := exec.CommandContext(ctx, pdftoppm,
		"-jpeg", "-r", "72", "-f", "1", "-l", "1", "-singlefile", pdfPath, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		c.log.Debugf("PDF thumbnail render for %s failed: %v: %s", messageID, err, strings.TrimSpace(string(out)))
		return ""
	}
	jpg, err := os.ReadFile(prefix + ".jpg")
	if err != nil || len(jpg) == 0 {
		return ""
	}
	return c.saveMessageThumbnailWithExtension(chatID, messageID+"-pdf", jpg, ".thumb.jpg")
}

// maybeDerivePDFThumbnail fills in a first-page preview for a PDF document
// row that has the file locally but no thumbnail yet: received PDFs whose
// sender shipped none, and — via the same call — any other gap. Rows that
// already carry a (sender- or locally-derived) thumbnail are untouched, so no
// received data is ever replaced.
func (c *Client) maybeDerivePDFThumbnail(ctx context.Context, message appstore.Message) {
	if message.MediaKind != appstore.MediaKindDocument || !isPDFMime(message.MediaMimeType) {
		return
	}
	if strings.TrimSpace(message.MediaThumbnailLocalPath) != "" {
		return
	}
	if strings.TrimSpace(message.MediaLocalPath) == "" {
		return
	}
	thumbPath := c.renderPDFThumbnail(ctx, message.ChatID, message.ID, message.MediaLocalPath)
	if thumbPath == "" {
		return
	}
	updated, err := c.store.UpdateMessageMediaThumbnailLocalPath(ctx, message.ID, thumbPath)
	if err != nil {
		c.log.Warnf("Failed to store derived PDF thumbnail for %s: %v", message.ID, err)
		return
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
}
