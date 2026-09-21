//go:build whatevr_mock

package wamock

import (
	"bytes"
	"image/gif"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Everything the mock attaches to a message claims to be a real file. These
// tests are what keeps that true: a synthetic blob that only looks like a jpeg
// fails inside the daemon, where it reads as a daemon bug.
func TestSynthJPEGDecodes(t *testing.T) {
	data, err := synthJPEG(320, 240, "seed")
	if err != nil {
		t.Fatalf("synthJPEG: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 240 {
		t.Fatalf("bounds = %v, want 320x240", b)
	}
}

func TestSynthGIFDecodes(t *testing.T) {
	data, err := synthGIF(160, 120, 6, "seed")
	if err != nil {
		t.Fatalf("synthGIF: %v", err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Image) != 6 {
		t.Fatalf("frames = %d, want 6", len(decoded.Image))
	}
}

func TestSynthWAVIsPCM(t *testing.T) {
	data := synthWAV(2*time.Second, "seed")
	if !bytes.HasPrefix(data, []byte("RIFF")) || !bytes.Contains(data[:16], []byte("WAVE")) {
		t.Fatal("not a riff wave")
	}
	// 16 bit mono at 16 kHz for two seconds, plus a 44 byte header.
	if want := 44 + 2*16000*2; len(data) != want {
		t.Fatalf("length = %d, want %d", len(data), want)
	}
}

func TestSynthPDFIsAPDF(t *testing.T) {
	data := synthPDF("Quarterly (report)")
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatal("no pdf header")
	}
	if !bytes.Contains(data, []byte("startxref")) || !bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF")) {
		t.Fatal("no trailer")
	}
	if bytes.Contains(data, []byte("(Quarterly (report))")) {
		t.Fatal("unescaped parentheses in the title would break the content stream")
	}
}

// ffmpeg is the daemon's own decoder for posters and waveforms, so it is the
// right judge of whether the container writers work. Skipped when it is not
// installed rather than failing: it is optional for the daemon too.
func TestSynthContainersDecodeWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	mov, err := synthMOV(320, 240, 10, 5, "seed")
	if err != nil {
		t.Fatalf("synthMOV: %v", err)
	}
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"clip.mov", mov},
		{"tone.wav", synthWAV(time.Second, "seed")},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, tc.data, 0o600); err != nil {
			t.Fatalf("write %s: %v", tc.name, err)
		}
		out := filepath.Join(dir, tc.name+".png")
		cmd := exec.Command(ffmpeg, "-v", "error", "-y", "-i", path, "-frames:v", "1", out)
		if tc.name == "tone.wav" {
			cmd = exec.Command(ffmpeg, "-v", "error", "-y", "-i", path, "-f", "s16le", filepath.Join(dir, "pcm.raw"))
		}
		if combined, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg rejected %s: %v: %s", tc.name, err, combined)
		}
	}
}

// The webp encoder is hand written, so it gets checked against a decoder that
// is not. Pillow is not a project dependency; skip when it is absent.
func TestSynthWebPDecodes(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	data, err := synthWebP(200, 200, "seed")
	if err != nil {
		t.Fatalf("synthWebP: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sticker.webp")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	script := `
import sys
try:
    from PIL import Image
except ImportError:
    sys.exit(77)
img = Image.open(sys.argv[1])
img.load()
print(img.format, img.size, len(img.convert("RGB").getcolors(16)))
`
	out, err := exec.Command(python, "-c", script, path).CombinedOutput()
	if cmd, ok := err.(*exec.ExitError); ok && cmd.ExitCode() == 77 {
		t.Skip("pillow not installed")
	}
	if err != nil {
		t.Fatalf("pillow rejected the webp: %v: %s", err, out)
	}
	if got := string(bytes.TrimSpace(out)); got != "WEBP (200, 200) 2" {
		t.Fatalf("decoded as %q, want a 200x200 two colour webp", got)
	}
}
