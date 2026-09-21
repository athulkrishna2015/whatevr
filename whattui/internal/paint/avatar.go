package paint

import (
	"fmt"
	"image"
	"image/color"
)

// Disc is the ground a participant's initials sit on, and the shape a photo
// is cropped to once there are photos. It is centred in the box it was given
// and as large as the box allows, which on a terminal cell means the cell's
// short axis: an avatar that filled the box would be an egg.
type Disc struct {
	W, H int // pixels
	Fill color.NRGBA
	Edge color.NRGBA
	// Glyph is the initial, already rasterised and cropped to its own ink, to
	// be centred on the circle. Drawn here rather than typed into the cells
	// under it because a cell is the wrong size and in the wrong place: the
	// middle of a circle is not the corner of a cell, and asking the terminal
	// to scale a letter only works on the terminals that can, which is a
	// different set from the ones that can draw a circle at all.
	//
	// Key identifies it, since the pixels themselves cannot.
	Glyph    *image.NRGBA
	GlyphKey string
}

func (d Disc) Size() (int, int) { return d.W, d.H }

func (d Disc) Key() string {
	return fmt.Sprintf("disc/%dx%d/%v/%v/%s", d.W, d.H, d.Fill, d.Edge, d.GlyphKey)
}

func (d Disc) Render() *image.NRGBA {
	c := newCanvas(d.W, d.H)
	w, h := float64(d.W), float64(d.H)
	r := w / 2
	if h/2 < r {
		r = h / 2
	}
	shape := disc(w/2, h/2, r)
	c.fillShape(shape, d.Fill)
	c.strokeShape(shape, d.Edge, 1)
	c.stamp(d.Glyph)
	return c.img
}
