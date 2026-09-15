package wa

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

// TestDownscaleImageForStandard locks in the HD contract: small photos pass
// through byte-identical, large ones come back at most 1600px on a side as
// JPEG with real dimensions reported.
func TestDownscaleImageForStandard(t *testing.T) {
	small := testJPEG(t, 800, 600)
	out, mime, ext, w, h := downscaleImageForStandard(small, "image/jpeg")
	if !bytes.Equal(out, small) || mime != "image/jpeg" || ext != ".jpg" || w != 800 || h != 600 {
		t.Fatalf("small image changed: mime=%s ext=%s %dx%d", mime, ext, w, h)
	}

	large := testJPEG(t, 3200, 2400)
	out, mime, ext, w, h = downscaleImageForStandard(large, "image/jpeg")
	if mime != "image/jpeg" || ext != ".jpg" {
		t.Fatalf("scaled mime/ext = %s/%s, want image/jpeg/.jpg", mime, ext)
	}
	if w != 1600 || h != 1200 {
		t.Fatalf("scaled dims = %dx%d, want 1600x1200", w, h)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || cfg.Width != 1600 || cfg.Height != 1200 {
		t.Fatalf("scaled bytes decode = %+v, %v; want 1600x1200", cfg, err)
	}
}
