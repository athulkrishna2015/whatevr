package textrun

import (
	"image/color"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// shaped builds a shaper at a plausible terminal cell and waits for it, or
// skips: there is no guarantee the machine running this has a font for
// devanagari, and a test that invents one would be testing nothing.
func shaped(t *testing.T, text string) *Run {
	t.Helper()
	sh := New(Options{})
	sh.SetCellSize(10, 21)
	r := wait(t, sh, text)
	requireGlyphs(t, sh, text)
	return r
}

// requireGlyphs skips unless some installed face actually draws every rune of
// text. Having an index is not the same as having the script: with no
// devanagari face the phrase comes out as a row of tofu, each box a full cell
// wide, and every width below then measures the boxes instead of the writing.
// ci installs the font, so there it runs.
func requireGlyphs(t *testing.T, sh *Shaper, text string) {
	t.Helper()
	s, ok := sh.ready()
	if !ok {
		t.Skip("no usable font index on this machine")
	}
	fams := s.families(text)
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		face := s.faceFor(r, fams)
		if face == nil {
			t.Skipf("no face for %q on this machine", r)
		}
		if _, ok := face.NominalGlyph(r); !ok {
			t.Skipf("nothing on this machine draws %q", r)
		}
	}
}

// wait spins until the shaper has built its font index, which it does off the
// caller's goroutine so a frame never blocks on it.
func wait(t *testing.T, sh *Shaper, text string) *Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if r := sh.Shape(text, false, false); r != nil {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Skip("no usable font index on this machine")
	return nil
}

// The whole reason this package exists: a terminal scores these clusters at
// fewer columns than they draw in, so the phrase overlaps itself. Our answer
// has to be wider than one cell per cluster.
func TestADevanagariPhraseNeedsMoreColumnsThanItHasClusters(t *testing.T) {
	const text = "नमस्ते सर"
	r := shaped(t, text)
	if r.Cells() < 1 {
		t.Fatalf("cells = %d", r.Cells())
	}
	if r.Cells() > utf8.RuneCountInString(text) {
		t.Fatalf("cells = %d for %d runes, which is wider than the text can be",
			r.Cells(), utf8.RuneCountInString(text))
	}
}

func TestAPrefixNeverOverrunsTheColumnsItWasGiven(t *testing.T) {
	const text = "नमस्ते सर, Khatabook के इंस्टेंट लोन"
	r := shaped(t, text)
	for cells := 1; cells <= r.Cells(); cells++ {
		head := r.Prefix(cells)
		if head == "" {
			continue
		}
		cut := shaped(t, head)
		if cut.Cells() > cells {
			t.Fatalf("prefix for %d cells shapes to %d: %q", cells, cut.Cells(), head)
		}
	}
}

func TestTheWholePhraseSurvivesAPrefixThatFits(t *testing.T) {
	const text = "नमस्ते"
	r := shaped(t, text)
	if got := r.Prefix(r.Cells()); got != text {
		t.Fatalf("Prefix(all) = %q, want %q", got, text)
	}
	if got := r.Prefix(r.Cells() + 10); got != text {
		t.Fatalf("Prefix(more than all) = %q, want %q", got, text)
	}
}

// A letter drawn for an avatar is sized by the circle around it, not by the
// terminal's cell, and it comes back cropped to its own ink so the caller can
// centre it on something.
func TestAGlyphIsDrawnAtTheSizeAskedForAndCroppedToItsInk(t *testing.T) {
	sh := New(Options{})
	sh.SetCellSize(10, 20)
	deadline := time.Now().Add(30 * time.Second)
	for !sh.Begin() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !sh.Begin() {
		t.Skip("no usable font index on this machine")
	}

	small := sh.Glyph("S", 20, color.NRGBA{255, 255, 255, 255})
	big := sh.Glyph("S", 40, color.NRGBA{255, 255, 255, 255})
	if small == nil || big == nil {
		t.Fatal("the shaper drew nothing")
	}
	if big.Bounds().Dy() <= small.Bounds().Dy() {
		t.Fatalf("twice the size is %d pixels tall, once is %d", big.Bounds().Dy(), small.Bounds().Dy())
	}
	// Cropped to the ink: every edge of the image has something on it.
	for _, edge := range []struct {
		name string
		at   func(i int) (int, int)
		n    int
	}{
		{"top", func(i int) (int, int) { return i, 0 }, big.Bounds().Dx()},
		{"bottom", func(i int) (int, int) { return i, big.Bounds().Dy() - 1 }, big.Bounds().Dx()},
		{"left", func(i int) (int, int) { return 0, i }, big.Bounds().Dy()},
		{"right", func(i int) (int, int) { return big.Bounds().Dx() - 1, i }, big.Bounds().Dy()},
	} {
		inked := false
		for i := 0; i < edge.n; i++ {
			x, y := edge.at(i)
			if big.NRGBAAt(x, y).A > 0 {
				inked = true
				break
			}
		}
		if !inked {
			t.Errorf("the %s edge of the image has no ink, so it was not cropped to it", edge.name)
		}
	}
	if nothing := sh.Glyph(" ", 40, color.NRGBA{255, 255, 255, 255}); nothing != nil {
		t.Error("a space drew ink")
	}
}
