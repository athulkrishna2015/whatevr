// Lifted from pawbar's internal/textrun, which is where this pipeline was
// built and proven. Kept close to that copy on purpose: the two should stay
// diffable.

package textrun

import (
	"image"
	"image/color"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// mask rasterises a line into an 8 bit coverage mask the width of cells
// columns. A mask rather than a bitmap for three reasons: kitty wants
// straight alpha and Go's RGBA is premultiplied, recolouring a mask is a
// memcpy so a theme change costs nothing, and coverage is the only thing
// worth caching since it is what the colour is applied to.
//
// Each glyph gets its own rasterizer pass composited over the mask. Piling
// every outline into one pass would be faster and wrong: Devanagari marks
// overlap their bases, and a counter in one glyph would cancel ink in
// another.
func (s *fontset) mask(line shaping.Line, cells int) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, cells*s.cellW, s.cellH))
	var z vector.Rasterizer
	x := float32(0)
	y := float32(s.met.baseline)
	for _, out := range line {
		scale := float32(s.met.em) / float32(out.Face.Upem())
		for _, g := range out.Glyphs {
			gx := x + fixedToFloat(g.XOffset)
			gy := y - fixedToFloat(g.YOffset)
			if outline, ok := out.Face.GlyphData(g.GlyphID).(font.GlyphOutline); ok {
				drawOutline(&z, m, outline, scale, gx, gy)
			}
			x += fixedToFloat(g.XAdvance)
		}
	}
	return m
}

// drawOutline fills one glyph into the mask. The rasterizer is sized to the
// glyph's own box, so a run costs about as much as its ink and not as much as
// its bounding row.
func drawOutline(z *vector.Rasterizer, dst *image.Alpha, o font.GlyphOutline, scale, x, y float32) {
	if len(o.Segments) == 0 {
		return
	}
	minX, minY := float32(1<<30), float32(1<<30)
	maxX, maxY := float32(-(1 << 30)), float32(-(1 << 30))
	for i := range o.Segments {
		for _, pt := range o.Segments[i].ArgsSlice() {
			px := pt.X*scale + x
			py := -pt.Y*scale + y
			minX, maxX = min(minX, px), max(maxX, px)
			minY, maxY = min(minY, py), max(maxY, py)
		}
	}

	// A pixel of slack each way so antialiased edges are not clipped.
	box := image.Rect(int(minX)-1, int(minY)-1, int(maxX)+2, int(maxY)+2).Intersect(dst.Bounds())
	if box.Empty() {
		return
	}
	ox, oy := float32(box.Min.X), float32(box.Min.Y)

	z.Reset(box.Dx(), box.Dy())
	for _, seg := range o.Segments {
		p := func(i int) (float32, float32) {
			return seg.Args[i].X*scale + x - ox, -seg.Args[i].Y*scale + y - oy
		}
		switch seg.Op {
		case opentype.SegmentOpMoveTo:
			z.MoveTo(p(0))
		case opentype.SegmentOpLineTo:
			z.LineTo(p(0))
		case opentype.SegmentOpQuadTo:
			x1, y1 := p(0)
			x2, y2 := p(1)
			z.QuadTo(x1, y1, x2, y2)
		case opentype.SegmentOpCubeTo:
			x1, y1 := p(0)
			x2, y2 := p(1)
			x3, y3 := p(2)
			z.CubeTo(x1, y1, x2, y2, x3, y3)
		}
	}
	z.Draw(dst, box, image.Opaque, image.Point{})
}

// colourise expands a coverage mask into the straight rgba kitty's f=32
// wants, over a transparent ground so the cell's own background, and the
// panel's transparency with it, show through untouched.
//
// No gamma curve: kitty's text_composition_strategy on linux is gamma 1.0 and
// contrast 0, which is plain alpha blending. Inventing a curve here is what
// would make the ink look pasted in.
func colourise(m *image.Alpha, fg color.NRGBA) *image.NRGBA {
	b := m.Bounds()
	out := image.NewNRGBA(b)
	for y := 0; y < b.Dy(); y++ {
		src := m.Pix[y*m.Stride : y*m.Stride+b.Dx()]
		dst := out.Pix[y*out.Stride:]
		for x, a := range src {
			if a == 0 {
				continue
			}
			i := x * 4
			dst[i+0] = fg.R
			dst[i+1] = fg.G
			dst[i+2] = fg.B
			dst[i+3] = uint8(uint32(a) * uint32(fg.A) / 255)
		}
	}
	return out
}

func fixedToFloat(i fixed.Int26_6) float32 { return float32(i) / 64 }

// maskScaled rasterises a shaped line at a size of its own rather than at the
// terminal's cell. Same pipeline as mask, one multiplier through it: the
// outlines are real outlines, so a letter twice the size of a cell is drawn
// twice the size rather than drawn once and stretched.
func (s *fontset) maskScaled(line shaping.Line, k float64) *image.Alpha {
	advance := 0.0
	for _, out := range line {
		for _, g := range out.Glyphs {
			advance += float64(fixedToFloat(g.XAdvance))
		}
	}
	w := int(math.Ceil(advance*k)) + 2
	h := int(math.Ceil(float64(s.cellH)*k)) + 2
	if w < 1 || h < 1 {
		return nil
	}

	m := image.NewAlpha(image.Rect(0, 0, w, h))
	var z vector.Rasterizer
	x := float32(0)
	y := float32(float64(s.met.baseline) * k)
	for _, out := range line {
		scale := float32(s.met.em * k / float64(out.Face.Upem()))
		for _, g := range out.Glyphs {
			gx := x + fixedToFloat(g.XOffset)*float32(k)
			gy := y - fixedToFloat(g.YOffset)*float32(k)
			if outline, ok := out.Face.GlyphData(g.GlyphID).(font.GlyphOutline); ok {
				drawOutline(&z, m, outline, scale, gx, gy)
			}
			x += fixedToFloat(g.XAdvance) * float32(k)
		}
	}
	return m
}

// inkOf is the part of a mask that has ink in it. What a letter is worth
// centring on is its ink, not the line box it was drawn in.
func inkOf(m *image.Alpha) image.Rectangle {
	b := m.Bounds()
	box := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := m.Pix[(y-b.Min.Y)*m.Stride:]
		for x := b.Min.X; x < b.Max.X; x++ {
			if row[x-b.Min.X] == 0 {
				continue
			}
			box.Min.X = min(box.Min.X, x)
			box.Min.Y = min(box.Min.Y, y)
			box.Max.X = max(box.Max.X, x+1)
			box.Max.Y = max(box.Max.Y, y+1)
		}
	}
	if box.Empty() {
		return image.Rectangle{}
	}
	return box
}

// crop copies the part of a mask worth keeping.
func crop(m *image.Alpha, box image.Rectangle) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, box.Dx(), box.Dy()))
	for y := 0; y < box.Dy(); y++ {
		copy(out.Pix[y*out.Stride:(y+1)*out.Stride],
			m.Pix[(box.Min.Y+y)*m.Stride+box.Min.X:])
	}
	return out
}
