//go:build !cgo

package turbojpeg

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
)

// Decode is the non-cgo fallback used by cross-compilation. Production
// builds use libjpeg-turbo for bounded-memory, scaled decoding.
func Decode(buf []byte, _ int) (*image.RGBA, error) {
	img, err := jpeg.Decode(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	rgba, ok := img.(*image.RGBA)
	if ok {
		return rgba, nil
	}
	out := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out, nil
}

func DecodeFile(path string, atLeast int) (*image.RGBA, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(data, atLeast)
}
