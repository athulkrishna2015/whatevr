package paint

import (
	"fmt"
	"image"
	"image/color"
)

// QR is a pairing code drawn module for module. no smoothing and no theme: a
// phone reads hard dark squares on light, and an antialiased edge is noise.
type QR struct {
	W, H int
	// N is modules on a side, quiet zone included, and Dark is N*N row major.
	N    int
	Dark []bool
	// Code is what was encoded, which is what names the pixels.
	Code string
}

func (q QR) Size() (int, int) { return q.W, q.H }

func (q QR) Key() string { return fmt.Sprintf("qr/%dx%d/%s", q.W, q.H, q.Code) }

// Render centres a square of whole-pixel modules in the rect, since a module
// that is not a whole number of pixels blurs. only the square is light: the
// cells are taller than they are wide, and a light rect would give the code a
// thicker quiet zone above and below than at its sides.
func (q QR) Render() *image.NRGBA {
	c := newCanvas(q.W, q.H)
	if q.N <= 0 || len(q.Dark) != q.N*q.N {
		return c.img
	}
	m := min(c.w, c.h) / q.N
	if m < 1 {
		return c.img
	}
	light := color.NRGBA{0xff, 0xff, 0xff, 0xff}
	dark := color.NRGBA{0x00, 0x00, 0x00, 0xff}
	ox, oy := (c.w-m*q.N)/2, (c.h-m*q.N)/2
	for my := 0; my < q.N; my++ {
		for mx := 0; mx < q.N; mx++ {
			ink := light
			if q.Dark[my*q.N+mx] {
				ink = dark
			}
			for y := oy + my*m; y < oy+(my+1)*m; y++ {
				for x := ox + mx*m; x < ox+(mx+1)*m; x++ {
					c.img.SetNRGBA(x, y, ink)
				}
			}
		}
	}
	return c.img
}
