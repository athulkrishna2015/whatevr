package whatsapp

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// flat is one colour over r, without the pixels
type flat struct {
	*image.Uniform
	r image.Rectangle
}

func (f flat) Bounds() image.Rectangle { return f.r }

func flatFile(t *testing.T, name string, w, h int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img := flat{image.NewUniform(color.RGBA{R: 30, G: 120, B: 200, A: 0xff}), image.Rect(0, 0, w, h)}
	if filepath.Ext(name) == ".png" {
		err = png.Encode(f, img)
	} else {
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 80})
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// heap is what fn allocated
func heap(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestABigPhotoGetsAThumbnailInLittleMemory(t *testing.T) {
	path := flatFile(t, "big.jpg", 8192, 6144)
	var c Client
	var img *waE2E.ImageMessage
	n := heap(func() {
		img = c.mediaBody(context.Background(), sendImage, path, "image/jpeg", "big.jpg", "", 1, false, nil).GetImageMessage()
	})
	if img.GetWidth() != 8192 || img.GetHeight() != 6144 {
		t.Fatalf("%dx%d", img.GetWidth(), img.GetHeight())
	}
	thumb, err := jpeg.DecodeConfig(bytes.NewReader(img.GetJPEGThumbnail()))
	if err != nil || thumb.Width != 100 || thumb.Height != 75 {
		t.Fatalf("thumbnail %v %v", thumb, err)
	}
	// 50 megapixels whole is 200 MiB of rgba
	if n > 16<<20 {
		t.Fatalf("took %d bytes of heap", n)
	}
}

func TestAPNGPastTheCapGoesWithoutAThumbnail(t *testing.T) {
	path := flatFile(t, "big.png", 5000, 4000)
	var c Client
	var w, th uint32
	var thumb []byte
	n := heap(func() {
		img := c.mediaBody(context.Background(), sendImage, path, "image/png", "big.png", "", 1, false, nil).GetImageMessage()
		w, th, thumb = img.GetWidth(), img.GetHeight(), img.GetJPEGThumbnail()
	})
	if w != 5000 || th != 4000 || thumb != nil {
		t.Fatalf("%dx%d, %d byte thumbnail", w, th, len(thumb))
	}
	if n > 1<<20 {
		t.Fatalf("took %d bytes of heap", n)
	}
}
