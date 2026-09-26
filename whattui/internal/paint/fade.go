package paint

import (
	"fmt"
	"image"
	"image/color"
)

// Fade is any spec drawn toward a ground. Chrome behind a panel has to fall
// back with everything else: a bubble is an image, and an image ignores the
// dim attribute a cell would carry.
//
// The alpha is kept as it is, so the shape and its antialiased edge survive
// and only the colour moves.
type Fade struct {
	Spec
	Toward color.NRGBA
	// Percent of the spec's own colour that survives.
	Percent int
}

func (f Fade) Key() string {
	return fmt.Sprintf("%s/fade/%v/%d", f.Spec.Key(), f.Toward, f.Percent)
}

func (f Fade) Render() *image.NRGBA {
	img := f.Spec.Render()
	mix := func(c, ground uint8) uint8 {
		return uint8((uint32(c)*uint32(f.Percent) + uint32(ground)*uint32(100-f.Percent)) / 100)
	}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = mix(img.Pix[i], f.Toward.R)
		img.Pix[i+1] = mix(img.Pix[i+1], f.Toward.G)
		img.Pix[i+2] = mix(img.Pix[i+2], f.Toward.B)
	}
	return img
}
