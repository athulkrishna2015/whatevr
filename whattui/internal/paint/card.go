package paint

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// Card is a surface that floats above the page rather than being part of it: a
// rounded panel with a bar of colour down its leading edge and a soft shadow
// underneath.
//
// The shadow is the whole point. A terminal application that wants to say "this
// is on top" has a box-drawing frame and nothing else, and a frame says "this is
// a region of the page". A gradient falling out from under an edge says the
// thing above it is not on the page at all, and nothing made of cells can draw
// one: the shadow is thinner than a cell, it is a ramp rather than a colour, and
// it lands on top of whatever text is already underneath.
//
// The image is taller than the card. What is left below it is the room the
// shadow falls into, which is why a caller reserves a row it does not write in.
type Card struct {
	W, H   int // pixels, the whole image
	Fill   color.NRGBA
	Edge   color.NRGBA
	Accent color.NRGBA
	Radius int
	// Bar is how many pixels of the leading edge the accent takes. It follows
	// the rounded corner rather than cutting across it, because a straight bar
	// against a round edge is the one detail that gives a drawn shape away.
	Bar int
	// Shadow is how far the shadow spreads and Drop is how far it falls. Both
	// are pixels, and both come out of the image's own height.
	Shadow int
	Drop   int
}

func (c Card) Size() (int, int) { return c.W, c.H }

func (c Card) Key() string {
	return fmt.Sprintf("card/%dx%d/%v/%v/%v/%d/%d/%d/%d",
		c.W, c.H, c.Fill, c.Edge, c.Accent, c.Radius, c.Bar, c.Shadow, c.Drop)
}

func (c Card) Render() *image.NRGBA {
	cv := newCanvas(c.W, c.H)
	// The card keeps half the spread clear either side, so the shadow has
	// somewhere to go horizontally as well as down.
	inset := float64(c.Shadow) / 2
	x0, x1 := inset, float64(c.W)-inset
	y1 := float64(c.H - c.Shadow - c.Drop)
	if x1 <= x0 || y1 <= 0 {
		return cv.img
	}
	r := math.Min(float64(c.Radius), math.Min((x1-x0)/2, y1/2))
	radii := [4]float64{r, r, r, r}
	body := roundRect(x0, 0, x1, y1, radii)

	if c.Shadow > 0 {
		cv.blurShape(roundRect(x0, float64(c.Drop), x1, y1+float64(c.Drop), radii),
			color.NRGBA{0, 0, 0, 0x66}, float64(c.Shadow))
	}
	cv.fillShape(body, c.Fill)
	if c.Bar > 0 && c.Accent.A > 0 {
		// The intersection of the card with a strip of its leading edge, so the
		// bar is the shape of the corner it sits in.
		strip := roundRect(x0, 0, x0+float64(c.Bar), y1, [4]float64{})
		cv.fillShape(func(x, y float64) float64 {
			return math.Max(body(x, y), strip(x, y))
		}, c.Accent)
	}
	cv.strokeShape(body, c.Edge, 1)
	return cv.img
}
