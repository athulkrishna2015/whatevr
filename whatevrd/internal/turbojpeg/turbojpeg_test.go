package turbojpeg

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// flat is one colour over r, without the pixels
type flat struct {
	*image.Uniform
	r image.Rectangle
}

func (f flat) Bounds() image.Rectangle { return f.r }

func encoded(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := flat{&image.Uniform{C: color.RGBA{R: 200, G: 40, B: 90, A: 0xff}}, image.Rect(0, 0, w, h)}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestABigPhotoDecodesAtAnEighth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jpg")
	if err := os.WriteFile(path, encoded(t, 4096, 3072), 0o600); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	img, err := DecodeFile(path, 100)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 384 {
		t.Fatalf("decoded at %v", b)
	}
	// the eighth and little else: the whole would be 48 MiB
	if n := after.TotalAlloc - before.TotalAlloc; n > 2<<20 {
		t.Fatalf("decode took %d bytes of heap", n)
	}
	r, g, b, _ := img.At(100, 100).RGBA()
	if r>>8 < 190 || g>>8 > 50 || b>>8 < 80 || img.Pix[3] != 0xff {
		t.Fatalf("colour %d %d %d alpha %d", r>>8, g>>8, b>>8, img.Pix[3])
	}
}

func TestASmallOneComesWhole(t *testing.T) {
	img, err := Decode(encoded(t, 60, 40), 100)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 60 || b.Dy() != 40 {
		t.Fatalf("decoded at %v", b)
	}
}

func TestNotAJpeg(t *testing.T) {
	if _, err := Decode([]byte("not a jpeg at all"), 100); err == nil {
		t.Fatal("decoded junk")
	}
	if _, err := Decode(nil, 100); err == nil {
		t.Fatal("decoded nothing")
	}
}

func TestProgressiveIsReadOffTheFrameHeader(t *testing.T) {
	app0 := []byte{0xff, 0xe0, 0x00, 0x04, 0x00, 0x00}
	frame := func(m byte) []byte {
		return append(append([]byte{0xff, 0xd8}, app0...), 0xff, m, 0x00, 0x02)
	}
	if !progressive(frame(0xc2)) || progressive(frame(0xc0)) {
		t.Fatal("misread the frame header")
	}
	if progressive(encoded(t, 16, 16)) || progressive([]byte{0xff, 0xd8, 0x00}) {
		t.Fatal("a baseline or broken jpeg read as progressive")
	}
}

func TestOnlyTheOldAPICapsAProgressiveJpeg(t *testing.T) {
	big := maxProgressive + 1
	if fits(big, 1, true, false) == nil || fits(big, 1, true, true) != nil || fits(big, 1, false, false) != nil {
		t.Fatal("progressive cap misapplied")
	}
	if fits(maxPixels+1, 1, false, true) == nil || fits(0, 10, false, true) == nil {
		t.Fatal("pixel cap misapplied")
	}
}
