package paint

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// Chip is the chrome behind one reaction: a rounded rectangle drawn inside the
// cells it was given rather than filling them.
//
// The inset is the whole of it. A shape that fills its row touches the rows
// above and below, which in a transcript are the words it belongs to and the
// message after them, so what should read as a small thing under a message
// reads as a band across it. Horizontally it is the same argument from the
// other side: the cells a chip claims are whole cells, and a whole cell of air
// each side of a glyph that is two cells wide is a chip mostly made of nothing.
// Insetting in pixels gives it the air a chip actually wants, which is less
// than a column and more than none.
// The two vertical insets are not one number because a glyph does not sit in
// the middle of its cell: it stands on a baseline, which is below the middle, so
// a shape centred on the cell holds the glyph low. Taking more off the top than
// the bottom puts the shape where the glyph actually is.
type Chip struct {
	W, H                  int // pixels, the cells it was given
	InsetX                int
	InsetTop, InsetBottom int
	Fill                  color.NRGBA
	Edge                  color.NRGBA
	Radius                int
}

func (c Chip) Size() (int, int) { return c.W, c.H }

func (c Chip) Key() string {
	return fmt.Sprintf("chip/%dx%d/%d+%d+%d/%v/%v/%d",
		c.W, c.H, c.InsetX, c.InsetTop, c.InsetBottom, c.Fill, c.Edge, c.Radius)
}

func (c Chip) Render() *image.NRGBA {
	cv := newCanvas(c.W, c.H)
	x0, y0 := float64(c.InsetX), float64(c.InsetTop)
	x1, y1 := float64(c.W-c.InsetX), float64(c.H-c.InsetBottom)
	if x1 <= x0 || y1 <= y0 {
		return cv.img
	}
	r := math.Min(float64(c.Radius), math.Min((x1-x0)/2, (y1-y0)/2))
	shape := roundRect(x0, y0, x1, y1, [4]float64{r, r, r, r})
	cv.fillShape(shape, c.Fill)
	cv.strokeShape(shape, c.Edge, 1)
	return cv.img
}
