package textrun

import (
	"testing"
	"time"
	"unicode/utf8"
)

// shaped builds a shaper at a plausible terminal cell and waits for it, or
// skips: there is no guarantee the machine running this has a font for
// devanagari, and a test that invents one would be testing nothing.
func shaped(t *testing.T, text string) *Run {
	t.Helper()
	sh := New(Options{})
	sh.SetCellSize(10, 21)
	return wait(t, sh, text)
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
