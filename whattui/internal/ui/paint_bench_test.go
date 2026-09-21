package ui

import (
	"testing"
	"time"

	"whattui/internal/term"
	"whattui/internal/textrun"
)

// The paint benchmarks draw an account a real daemon computed, because the
// numbers only mean anything if the rows, the sort keys and the window paging
// are the ones production produces. flood is the account nobody has: four
// hundred chats and a conversation three thousand messages deep.

func BenchmarkPaintFullFrame(b *testing.B) {
	a := mockApp(b, floodScenario, floodDeepChat, 120, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.paint()
	}
}

// The pointer moving across the chat list changes one row's background and
// nothing else, and is the frame that has to be quick: motion arrives for
// every pixel the pointer crosses.
func BenchmarkPaintHoverMove(b *testing.B) {
	a := mockApp(b, floodScenario, floodDeepChat, 120, 40)
	a.paint()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.hovered = i % 18
		a.paint()
	}
}

// The same frame with the scripts that have to be shaped and rasterised. This
// is the expensive case and the one a pointer drag has to keep up with, so it
// is taken against the chat that is nothing but those scripts.
func BenchmarkPaintComplexScript(b *testing.B) {
	a := mockApp(b, tortureScenario, tortureChat, 120, 40)
	a.caps = term.Caps{Tier: term.TierShm, RGB: true}
	a.shaper = textrun.New(textrun.Options{})
	a.shaper.SetCellSize(10, 21)

	deadline := time.Now().Add(30 * time.Second)
	for !a.shaper.Begin() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.shaper.Begin() {
		b.Skip("no usable font index on this machine")
	}

	a.paint()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.hovered = i % 18
		a.paint()
	}
}

func BenchmarkPaintNarrow(b *testing.B) {
	a := mockApp(b, floodScenario, floodDeepChat, 60, 24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.paint()
	}
}
