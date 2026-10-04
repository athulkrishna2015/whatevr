// package turbojpeg decodes jpegs through libjpeg-turbo, shrunk while they
// decode: a thumbnail of a 50 megapixel photo never holds 50 megapixels.
//
// it builds on the TurboJPEG 3 api where the header has it and on the 2.x
// one otherwise (debian, ubuntu). 3 caps libjpeg-turbo's own buffers; under 2
// the caps here are all there is. -DWHATEVR_TURBOJPEG2 in CGO_CFLAGS takes
// the 2.x path against a 3 header, to test it.
package turbojpeg

/*
#cgo pkg-config: libturbojpeg
#include <turbojpeg.h>

#if defined(TJ_NUMINIT) && !defined(WHATEVR_TURBOJPEG2)
#define TJ3 1
#else
#define TJ3 0
#endif

enum { tj3 = TJ3 };

static tjhandle decompressor(int maxMemoryMB, int maxPixels, int maxScans) {
#if TJ3
	tjhandle h = tj3Init(TJINIT_DECOMPRESS);
	if (h && (tj3Set(h, TJPARAM_MAXMEMORY, maxMemoryMB) || tj3Set(h, TJPARAM_MAXPIXELS, maxPixels) ||
		tj3Set(h, TJPARAM_SCANLIMIT, maxScans))) {
		tj3Destroy(h);
		return NULL;
	}
	return h;
#else
	return tjInitDecompress();
#endif
}

static void destroy(tjhandle h) {
#if TJ3
	tj3Destroy(h);
#else
	tjDestroy(h);
#endif
}

static int header(tjhandle h, const unsigned char *buf, size_t n, int *w, int *ht) {
#if TJ3
	if (tj3DecompressHeader(h, buf, n)) return -1;
	*w = tj3Get(h, TJPARAM_JPEGWIDTH);
	*ht = tj3Get(h, TJPARAM_JPEGHEIGHT);
	return 0;
#else
	int subsamp, colorspace;
	return tjDecompressHeader3(h, buf, (unsigned long)n, w, ht, &subsamp, &colorspace);
#endif
}

static tjscalingfactor *factors(int *n) {
#if TJ3
	return tj3GetScalingFactors(n);
#else
	return tjGetScalingFactors(n);
#endif
}

// w and ht are sf applied: 2.x picks the factor from them
static int decompress(tjhandle h, const unsigned char *buf, size_t n, tjscalingfactor sf, unsigned char *dst, int w, int pitch, int ht) {
#if TJ3
	if (tj3SetScalingFactor(h, sf)) return -1;
	return tj3Decompress8(h, buf, n, dst, pitch, TJPF_RGBA);
#else
	int flags = 0;
#ifdef TJFLAG_LIMITSCANS
	flags |= TJFLAG_LIMITSCANS;
#endif
	return tjDecompress2(h, buf, (unsigned long)n, dst, w, pitch, ht, TJPF_RGBA, flags);
#endif
}

static int warned(tjhandle h) {
#if TJ3
	return tj3GetErrorCode(h) == TJERR_WARNING;
#else
	return tjGetErrorCode(h) == TJERR_WARNING;
#endif
}

static char *message(tjhandle h) {
#if TJ3
	return tj3GetErrorStr(h);
#else
	return tjGetErrorStr2(h);
#endif
}
*/
import "C"

import (
	"errors"
	"fmt"
	"image"
	"os"
	"unsafe"
)

// what one decode may take: libjpeg-turbo's buffers for a progressive jpeg,
// the source's pixels and its scans. 256 megapixels at an eighth is 16 MiB
// of rgba.
const (
	maxMemoryMB = 32
	maxPixels   = 256 << 20
	maxScans    = 500
	// a progressive jpeg's coefficients are held whole whatever the scale,
	// at most 6 bytes a pixel (4:4:4). 2.x cannot cap them, so this does
	maxProgressive = maxMemoryMB << 20 / 6
)

// Decode is the jpeg in buf at the smallest scale libjpeg-turbo has that
// keeps its longer side at least atLeast, or whole when it is smaller.
func Decode(buf []byte, atLeast int) (*image.RGBA, error) {
	if len(buf) == 0 {
		return nil, errors.New("turbojpeg: no data")
	}
	h := C.decompressor(maxMemoryMB, maxPixels, maxScans)
	if h == nil {
		return nil, errors.New("turbojpeg: no decompressor")
	}
	defer C.destroy(h)
	src, n := (*C.uchar)(unsafe.Pointer(&buf[0])), C.size_t(len(buf))
	var cw, ch C.int
	if C.header(h, src, n, &cw, &ch) != 0 {
		return nil, failed(h)
	}
	w, ht := int(cw), int(ch)
	if err := fits(w, ht, progressive(buf), C.tj3 == 1); err != nil {
		return nil, err
	}
	sf := smallest(w, ht, atLeast)
	img := image.NewRGBA(image.Rect(0, 0, scaled(w, sf), scaled(ht, sf)))
	// alpha comes out 0xff
	if C.decompress(h, src, n, sf, (*C.uchar)(unsafe.Pointer(&img.Pix[0])), C.int(img.Rect.Dx()), C.int(img.Stride), C.int(img.Rect.Dy())) != 0 &&
		C.warned(h) == 0 {
		return nil, failed(h)
	}
	return img, nil
}

// DecodeFile is Decode of the file at path, mapped rather than read: the
// compressed bytes stay out of the heap.
func DecodeFile(path string, atLeast int) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 || info.Size() != int64(int(info.Size())) {
		return nil, fmt.Errorf("turbojpeg: %s is %d bytes", path, info.Size())
	}
	buf, unmap, err := mapFile(f, int(info.Size()))
	if err != nil {
		return nil, err
	}
	defer unmap()
	return Decode(buf, atLeast)
}

// fits says whether a w by h jpeg is one to decode. with libjpeg-turbo's own
// memory cap a progressive one needs no cap of its own.
func fits(w, h int, progressive, capped bool) error {
	switch {
	case w <= 0 || h <= 0 || w*h > maxPixels:
		return fmt.Errorf("turbojpeg: %dx%d is past %d pixels", w, h, maxPixels)
	case progressive && !capped && w*h > maxProgressive:
		return fmt.Errorf("turbojpeg: %dx%d progressive is past %d pixels", w, h, maxProgressive)
	}
	return nil
}

// progressive is whether the first frame header in buf is a progressive
// one. it walks the markers up to it; anything it cannot follow is not.
func progressive(buf []byte) bool {
	for i := 2; i+4 <= len(buf); {
		if buf[i] != 0xff {
			return false
		}
		m := buf[i+1]
		switch {
		case m == 0xff:
			i++
			continue
		case m == 0xc2 || m == 0xc6 || m == 0xca || m == 0xce:
			return true
		case m >= 0xc0 && m <= 0xcf && m != 0xc4 && m != 0xc8 && m != 0xcc, m == 0xda:
			return false
		}
		i += 2 + (int(buf[i+2])<<8 | int(buf[i+3]))
	}
	return false
}

// smallest is the smallest scaling factor at most 1 that keeps the longer
// side at least atLeast.
func smallest(w, h, atLeast int) C.tjscalingfactor {
	var n C.int
	sfs := unsafe.Slice(C.factors(&n), int(n))
	best := C.tjscalingfactor{num: 1, denom: 1}
	for _, sf := range sfs {
		if sf.num > sf.denom || max(scaled(w, sf), scaled(h, sf)) < atLeast {
			continue
		}
		if int(sf.num)*int(best.denom) < int(best.num)*int(sf.denom) {
			best = sf
		}
	}
	return best
}

// scaled is TJSCALED, which cgo cannot call
func scaled(d int, sf C.tjscalingfactor) int {
	return (d*int(sf.num) + int(sf.denom) - 1) / int(sf.denom)
}

func failed(h C.tjhandle) error {
	return errors.New("turbojpeg: " + C.GoString(C.message(h)))
}
