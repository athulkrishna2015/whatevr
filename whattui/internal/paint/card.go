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
	W, H int // pixels, the whole image
	// Top is where the card starts inside the image and Body is how tall it is.
	// The two exist so a card can be taller than the row whose cells hold its
	// words and still be centred on it: half a row of air above the words, half
	// below, and the rest of the image is the room the shadow falls into. A card
	// that simply filled the image would be a rim hard against the tops and
	// bottoms of the letters.
	Top    int
	Body   int
	Fill   color.NRGBA
	Edge   color.NRGBA
	Radius int
	// Shadow is how far the shadow spreads and Drop is how far it falls, in
	// pixels, both into the room under the body.
	Shadow int
	Drop   int
}

func (c Card) Size() (int, int) { return c.W, c.H }

func (c Card) Key() string {
	return fmt.Sprintf("card/%dx%d/%d+%d/%v/%v/%d/%d/%d",
		c.W, c.H, c.Top, c.Body, c.Fill, c.Edge, c.Radius, c.Shadow, c.Drop)
}

func (c Card) Render() *image.NRGBA {
	cv := newCanvas(c.W, c.H)
	// The card keeps half the spread clear either side, so the shadow has
	// somewhere to go horizontally as well as down.
	inset := float64(c.Shadow) / 2
	x0, x1 := inset, float64(c.W)-inset
	y0 := float64(c.Top)
	y1 := y0 + float64(c.Body)
	if c.Body <= 0 || y1 > float64(c.H) {
		y0, y1 = 0, float64(c.H)
	}
	if x1 <= x0 || y1 <= y0 {
		return cv.img
	}
	r := math.Min(float64(c.Radius), math.Min((x1-x0)/2, (y1-y0)/2))
	radii := [4]float64{r, r, r, r}
	body := roundRect(x0, y0, x1, y1, radii)

	if c.Shadow > 0 {
		cv.blurShape(roundRect(x0, y0+float64(c.Drop), x1, y1+float64(c.Drop), radii),
			color.NRGBA{0, 0, 0, 0x66}, float64(c.Shadow))
	}
	cv.fillShape(body, c.Fill)
	cv.strokeShape(body, c.Edge, 1)
	return cv.img
}
