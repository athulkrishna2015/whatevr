// Package paint rasterises the chrome a cell grid cannot draw: bubbles with
// real rounded corners and a tail, discs, and the surfaces that come after
// them. What it produces is placed under the terminal's own text, so the
// glyphs stay the user's font and only the shapes are ours.
//
// Everything here is a spec and a renderer. A spec is data: it says what to
// draw and it names itself, and naming itself is what lets the caller cache
// the pixels without knowing what is in them. New chrome is a new spec, never
// a new pipeline.
package paint

import (
	"image"
	"image/color"
	"math"
)

// Spec is one thing to draw. Key identifies the pixels, so two specs with the
// same key must render the same image, and Size is what the caller reserved
// for it.
type Spec interface {
	Key() string
	Size() (w, h int)
	Render() *image.NRGBA
}

// canvas is an un-premultiplied RGBA buffer. Straight alpha because that is
// what the kitty f=32 transmit wants and what compositing over the terminal's
// own background needs: the ground is not ours to multiply into.
type canvas struct {
	img  *image.NRGBA
	w, h int
}

func newCanvas(w, h int) *canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &canvas{img: image.NewNRGBA(image.Rect(0, 0, w, h)), w: w, h: h}
}

// blend puts one colour over whatever is already at x,y, with coverage
// scaling its alpha. Straight alpha over straight alpha, done by hand: the
// stdlib compositor works in premultiplied colour and this buffer is not.
func (c *canvas) blend(x, y int, col color.NRGBA, coverage float64) {
	if coverage <= 0 || col.A == 0 || x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	if coverage > 1 {
		coverage = 1
	}
	sa := float64(col.A) / 255 * coverage
	if sa <= 0 {
		return
	}
	i := c.img.PixOffset(x, y)
	dst := c.img.Pix[i : i+4 : i+4]
	da := float64(dst[3]) / 255
	out := sa + da*(1-sa)
	if out <= 0 {
		dst[0], dst[1], dst[2], dst[3] = 0, 0, 0, 0
		return
	}
	mix := func(s, d uint8) uint8 {
		v := (float64(s)*sa + float64(d)*da*(1-sa)) / out
		return uint8(math.Round(math.Max(0, math.Min(255, v))))
	}
	dst[0] = mix(col.R, dst[0])
	dst[1] = mix(col.G, dst[1])
	dst[2] = mix(col.B, dst[2])
	dst[3] = uint8(math.Round(out * 255))
}

// fillShape paints every pixel a distance field says is inside, with the edge
// pixels weighted by how much of them is. One pixel of coverage either side of
// the boundary is the whole of the antialiasing, and it is why this draws
// curves a box-drawing glyph cannot.
func (c *canvas) fillShape(sdf func(x, y float64) float64, col color.NRGBA) {
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			c.blend(x, y, col, coverage(sdf(float64(x)+0.5, float64(y)+0.5)))
		}
	}
}

// strokeShape paints a band of width w just inside the boundary. Inside
// rather than centred on it, so a bubble's edge never lands half a pixel
// outside the cells the layout gave it.
func (c *canvas) strokeShape(sdf func(x, y float64) float64, col color.NRGBA, w float64) {
	if w <= 0 {
		return
	}
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			d := sdf(float64(x)+0.5, float64(y)+0.5)
			c.blend(x, y, col, coverage(d)-coverage(d+w))
		}
	}
}

// stamp puts an image in the middle of the canvas. Blended a pixel at a time
// rather than composited by the standard library, for the same reason
// everything else here is: this buffer is straight alpha and that compositor
// is not.
func (c *canvas) stamp(src *image.NRGBA) {
	if src == nil {
		return
	}
	b := src.Bounds()
	x0, y0 := (c.w-b.Dx())/2, (c.h-b.Dy())/2
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c.blend(x0+x, y0+y, src.NRGBAAt(b.Min.X+x, b.Min.Y+y), 1)
		}
	}
}

// coverage turns a signed distance into how much of a pixel is inside.
func coverage(d float64) float64 {
	switch {
	case d <= -0.5:
		return 1
	case d >= 0.5:
		return 0
	default:
		return 0.5 - d
	}
}

// roundRect is the distance to a rounded rectangle from x0,y0 to x1,y1, with
// a radius per corner in the order top left, top right, bottom right, bottom
// left. Per corner because a bubble with a tail squares off the corner the
// tail grows out of.
func roundRect(x0, y0, x1, y1 float64, r [4]float64) func(x, y float64) float64 {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	halfW, halfH := (x1-x0)/2, (y1-y0)/2
	return func(x, y float64) float64 {
		px, py := x-cx, y-cy
		var rad float64
		switch {
		case px <= 0 && py <= 0:
			rad = r[0]
		case px > 0 && py <= 0:
			rad = r[1]
		case px > 0 && py > 0:
			rad = r[2]
		default:
			rad = r[3]
		}
		rad = math.Min(rad, math.Min(halfW, halfH))
		qx := math.Abs(px) - (halfW - rad)
		qy := math.Abs(py) - (halfH - rad)
		return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - rad
	}
}

// disc is the distance to a circle.
func disc(cx, cy, r float64) func(x, y float64) float64 {
	return func(x, y float64) float64 { return math.Hypot(x-cx, y-cy) - r }
}
