package paint

import (
	"fmt"
	"image"
	"image/color"
)

// Side is a bubble's own side: which end of the conversation it came from.
type Side uint8

const (
	NoSide Side = iota
	Left
	Right
)

// Bubble is a message's chrome: a rounded rectangle with a hairline edge and
// one square corner at the bottom on the sender's side.
//
// The square corner is the tail. A real tail is a curved nub about a third of
// a cell across, which at ten by twenty pixels is a smudge rather than a
// shape, so the corner carries the same meaning at a size the grid can
// actually resolve.
type Bubble struct {
	W, H   int // pixels
	Fill   color.NRGBA
	Edge   color.NRGBA
	Radius int
	Anchor Side
}

func (b Bubble) Size() (int, int) { return b.W, b.H }

func (b Bubble) Key() string {
	return fmt.Sprintf("bubble/%dx%d/%v/%v/%d/%d", b.W, b.H, b.Fill, b.Edge, b.Radius, b.Anchor)
}

func (b Bubble) Render() *image.NRGBA {
	c := newCanvas(b.W, b.H)
	r := float64(b.Radius)
	// Top left, top right, bottom right, bottom left.
	radii := [4]float64{r, r, r, r}
	switch b.Anchor {
	case Left:
		radii[3] = 0
	case Right:
		radii[2] = 0
	}
	shape := roundRect(0, 0, float64(b.W), float64(b.H), radii)
	c.fillShape(shape, b.Fill)
	c.strokeShape(shape, b.Edge, 1)
	return c.img
}

// Rule is the hairline down the side of a run of messages. Drawn rather than
// typed: a quarter block glyph is a quarter of a cell of solid colour, and
// this is two pixels with round ends, which no cell can hold.
type Rule struct {
	W, H  int // pixels
	Fill  color.NRGBA
	Thick int
}

func (r Rule) Size() (int, int) { return r.W, r.H }

func (r Rule) Key() string {
	return fmt.Sprintf("rule/%dx%d/%v/%d", r.W, r.H, r.Fill, r.Thick)
}

func (r Rule) Render() *image.NRGBA {
	c := newCanvas(r.W, r.H)
	t := float64(r.Thick)
	if t < 1 {
		t = 1
	}
	x := (float64(r.W) - t) / 2
	c.fillShape(roundRect(x, 0, x+t, float64(r.H), [4]float64{t / 2, t / 2, t / 2, t / 2}), r.Fill)
	return c.img
}

// Hairline is a rule across the page, a pixel or two thick. A row of box
// glyphs is a fence; this is a line.
type Hairline struct {
	W, H  int // pixels
	Fill  color.NRGBA
	Thick int
}

func (h Hairline) Size() (int, int) { return h.W, h.H }

func (h Hairline) Key() string {
	return fmt.Sprintf("hairline/%dx%d/%v/%d", h.W, h.H, h.Fill, h.Thick)
}

func (h Hairline) Render() *image.NRGBA {
	c := newCanvas(h.W, h.H)
	t := float64(h.Thick)
	if t < 1 {
		t = 1
	}
	y := (float64(h.H) - t) / 2
	c.fillShape(roundRect(0, y, float64(h.W), y+t, [4]float64{}), h.Fill)
	return c.img
}
