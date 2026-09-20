package ui

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
)

// testApp is an app with just enough wiring to measure and wrap text.
func testApp(t *testing.T) *App {
	t.Helper()
	return &App{vx: vaxis.NewOffscreenWindow(80, 24).Vx}
}

// The point of the whole span machinery: a url cut in half is still openable,
// because both halves carry the whole thing.
func TestEveryPieceOfASplitUrlCarriesTheWholeUrl(t *testing.T) {
	a := testApp(t)
	const url = "https://music.youtube.com/watch?v=S2csSWo7Rvc&si=W6H7DMYyVNe6Qtd6"

	lines := a.wrapSpans(url, 30, true)
	if len(lines) < 2 {
		t.Fatalf("wrapped to %d lines, want a split", len(lines))
	}
	joined := ""
	for _, l := range lines {
		for _, s := range l {
			if s.link != url {
				t.Fatalf("fragment %q carries link %q", s.text, s.link)
			}
			joined += s.text
		}
	}
	if joined != url {
		t.Fatalf("fragments rejoin to %q", joined)
	}
}

func TestWrappingNeverExceedsTheWidth(t *testing.T) {
	a := testApp(t)
	body := "the daemon owns all state and the frontend owns none of it, " +
		"see https://example.com/a/very/long/path/that/will/not/fit/anywhere"
	for _, width := range []int{8, 13, 24, 40} {
		for _, l := range a.wrapSpans(body, width, true) {
			if got := a.lineWidth(l); got > width {
				t.Fatalf("width %d produced a line of %d: %q", width, got, lineText(l))
			}
		}
	}
}

func TestWrapKeepsBlankLinesBetweenParagraphs(t *testing.T) {
	a := testApp(t)
	got := a.wrap("one\n\ntwo", 20)
	if strings.Join(got, "|") != "one||two" {
		t.Fatalf("wrap = %q", got)
	}
}
