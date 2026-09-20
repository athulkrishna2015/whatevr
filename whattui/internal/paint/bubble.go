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
