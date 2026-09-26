package paint

import (
	"image/color"
	"testing"
)

func alphaAt(t *testing.T, s Spec, x, y int) uint8 {
	t.Helper()
	img := s.Render()
	w, h := s.Size()
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("rendered %dx%d, was given %dx%d", b.Dx(), b.Dy(), w, h)
	}
	return img.NRGBAAt(x, y).A
}

func TestABubbleRoundsEveryCornerButItsOwn(t *testing.T) {
	b := Bubble{W: 60, H: 40, Fill: color.NRGBA{255, 255, 255, 255}, Radius: 8, Anchor: Left}
	if a := alphaAt(t, b, 0, 0); a != 0 {
		t.Errorf("top left corner is %d, want transparent", a)
	}
	if a := alphaAt(t, b, 59, 39); a != 0 {
		t.Errorf("bottom right corner is %d, want transparent", a)
	}
	// The sender's own corner is square: that is the whole signal.
	if a := alphaAt(t, b, 0, 39); a != 255 {
		t.Errorf("bottom left corner is %d, want opaque", a)
	}
}

func TestABubbleIsPaintedInTheColourItWasGiven(t *testing.T) {
	fill := color.NRGBA{0x4c, 0x9a, 0xff, 0x40}
	img := Bubble{W: 40, H: 20, Fill: fill, Radius: 4}.Render()
	if got := img.NRGBAAt(20, 10); got != fill {
		t.Errorf("middle is %v, want %v", got, fill)
	}
}

func TestSpecsKeyOnEverythingThatChangesThePixels(t *testing.T) {
	base := Bubble{W: 40, H: 20, Fill: color.NRGBA{1, 2, 3, 4}, Radius: 4, Anchor: Left}
	same := base
	if base.Key() != same.Key() {
		t.Fatal("the same spec has two keys")
	}
	for name, other := range map[string]Bubble{
		"width":  {W: 41, H: 20, Fill: base.Fill, Radius: 4, Anchor: Left},
		"fill":   {W: 40, H: 20, Fill: color.NRGBA{9, 9, 9, 9}, Radius: 4, Anchor: Left},
		"radius": {W: 40, H: 20, Fill: base.Fill, Radius: 6, Anchor: Left},
		"anchor": {W: 40, H: 20, Fill: base.Fill, Radius: 4, Anchor: Right},
	} {
		if other.Key() == base.Key() {
			t.Errorf("a different %s keys the same", name)
		}
	}
}

func TestADiscStaysInsideItsBox(t *testing.T) {
	d := Disc{W: 20, H: 20, Fill: color.NRGBA{255, 255, 255, 255}}
	if a := alphaAt(t, d, 0, 0); a != 0 {
		t.Errorf("corner is %d, want transparent", a)
	}
	if a := alphaAt(t, d, 10, 10); a != 255 {
		t.Errorf("centre is %d, want opaque", a)
	}
	// Wider than it is tall, which is what a pair of cells is: the circle
	// takes the short axis and stays a circle.
	wide := Disc{W: 20, H: 10, Fill: color.NRGBA{255, 255, 255, 255}}
	if a := alphaAt(t, wide, 1, 5); a != 0 {
		t.Errorf("left edge is %d, want transparent", a)
	}
}

func TestTheEdgesAreSmooth(t *testing.T) {
	// A hard edge is a jagged edge. Somewhere along a rounded corner there has
	// to be a pixel that is neither in nor out.
	img := Bubble{W: 60, H: 40, Fill: color.NRGBA{255, 255, 255, 255}, Radius: 10}.Render()
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if a := img.NRGBAAt(x, y).A; a > 0 && a < 255 {
				return
			}
		}
	}
	t.Fatal("every pixel in the corner is fully in or fully out")
}
