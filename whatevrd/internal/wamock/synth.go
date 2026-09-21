//go:build whatevr_mock

package wamock

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"time"
)

// Everything in here makes a real file rather than a plausible-looking blob.
// The daemon runs ffmpeg over video and audio for posters and waveforms, the
// frontends decode images for real, and a file that only pretends to be one
// fails in ways that look like daemon bugs.
//
// The one thing the mock does not have is a video encoder. Its video is motion
// JPEG in a QuickTime container, which ffmpeg and mpv both decode, rather than
// h264 in mp4.

// synthPalette turns a seed into two colours that contrast. Same seed, same
// picture, every run.
func synthPalette(seed string) (base, accent color.RGBA) {
	sum := sha256.Sum256([]byte(seed))
	base = color.RGBA{R: 40 + sum[0]/3, G: 40 + sum[1]/3, B: 40 + sum[2]/3, A: 255}
	accent = color.RGBA{R: 200 - sum[3]/4, G: 200 - sum[4]/4, B: 200 - sum[5]/4, A: 255}
	return base, accent
}

// synthFrame draws one picture: diagonal bands, plus a block that moves with
// the frame number so a video is visibly moving.
func synthFrame(width, height, frame int, seed string) *image.RGBA {
	base, accent := synthPalette(seed)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	band := height / 6
	if band < 1 {
		band = 1
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := base
			if ((x+y)/band+frame)%2 == 1 {
				pixel = accent
			}
			img.Set(x, y, pixel)
		}
	}
	// A marker that walks left to right, so a poster taken from frame 0 is
	// distinguishable from one taken later.
	markerW, markerH := width/8, height/8
	x0 := (frame * width / 12) % (width - markerW)
	y0 := height/2 - markerH/2
	for y := y0; y < y0+markerH; y++ {
		for x := x0; x < x0+markerW; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	return img
}

func synthJPEG(width, height int, seed string) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, synthFrame(width, height, 0, seed), &jpeg.Options{Quality: 82}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// synthThumbnail is the inline preview a media message carries. Real ones are
// a few kilobytes of jpeg, and the frontends draw them before the download
// finishes, so this has to be a real image too.
func synthThumbnail(seed string) []byte {
	data, err := synthJPEG(96, 96, seed)
	if err != nil {
		return nil
	}
	return data
}

func synthPNG(width, height int, seed string) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, synthFrame(width, height, 0, seed)); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

func synthGIF(width, height, frames int, seed string) ([]byte, error) {
	base, accent := synthPalette(seed)
	palette := color.Palette{base, accent, color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	out := &gif.GIF{}
	for i := 0; i < frames; i++ {
		src := synthFrame(width, height, i, seed)
		framed := image.NewPaletted(src.Bounds(), palette)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				framed.Set(x, y, src.At(x, y))
			}
		}
		out.Image = append(out.Image, framed)
		out.Delay = append(out.Delay, 12)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, out); err != nil {
		return nil, fmt.Errorf("encode gif: %w", err)
	}
	return buf.Bytes(), nil
}

// synthWAV is 16 bit mono PCM: a tone that slides, so a waveform drawn from it
// has shape rather than being a flat bar.
func synthWAV(d time.Duration, seed string) []byte {
	const rate = 16000
	samples := int(d.Seconds() * rate)
	if samples < rate/10 {
		samples = rate / 10
	}
	sum := sha256.Sum256([]byte(seed))
	startHz := 180.0 + float64(sum[0])
	endHz := 320.0 + float64(sum[1])

	body := new(bytes.Buffer)
	phase := 0.0
	for i := 0; i < samples; i++ {
		progress := float64(i) / float64(samples)
		hz := startHz + (endHz-startHz)*progress
		phase += 2 * math.Pi * hz / rate
		// An envelope that swells and fades, so the waveform is not a rectangle.
		envelope := math.Sin(math.Pi * progress)
		value := int16(envelope * 12000 * math.Sin(phase))
		_ = binary.Write(body, binary.LittleEndian, value)
	}

	out := new(bytes.Buffer)
	out.WriteString("RIFF")
	_ = binary.Write(out, binary.LittleEndian, uint32(36+body.Len()))
	out.WriteString("WAVEfmt ")
	_ = binary.Write(out, binary.LittleEndian, uint32(16))
	_ = binary.Write(out, binary.LittleEndian, uint16(1))      // pcm
	_ = binary.Write(out, binary.LittleEndian, uint16(1))      // mono
	_ = binary.Write(out, binary.LittleEndian, uint32(rate))   //
	_ = binary.Write(out, binary.LittleEndian, uint32(rate*2)) // byte rate
	_ = binary.Write(out, binary.LittleEndian, uint16(2))      // block align
	_ = binary.Write(out, binary.LittleEndian, uint16(16))     // bits
	out.WriteString("data")
	_ = binary.Write(out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

// synthPDF writes a one page document by hand. A pdf is simple enough to emit
// literally, and a real one means a document message opens in whatever the
// desktop hands it to rather than erroring.
func synthPDF(title string) []byte {
	content := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET\n", pdfEscape(title))
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

func pdfEscape(s string) string {
	var out bytes.Buffer
	for _, r := range s {
		switch r {
		case '(', ')', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		default:
			if r < 32 || r > 126 {
				continue
			}
			out.WriteRune(r)
		}
	}
	return out.String()
}

// synthMOV writes a QuickTime file whose video track is motion JPEG: every
// sample is one of the frames synthFrame draws. ffmpeg and mpv both read it,
// which is what makes the daemon's poster extraction and the frontends' video
// playback run for real against the mock.
//
// The alternative would be h264 in mp4, which needs an encoder the mock has no
// business carrying. video/quicktime is a mimetype real accounts see anyway.
func synthMOV(width, height, frames, fps int, seed string) ([]byte, error) {
	if frames < 1 {
		frames = 1
	}
	if fps < 1 {
		fps = 1
	}
	samples := make([][]byte, 0, frames)
	var payload bytes.Buffer
	for i := 0; i < frames; i++ {
		var frame bytes.Buffer
		if err := jpeg.Encode(&frame, synthFrame(width, height, i, seed), &jpeg.Options{Quality: 70}); err != nil {
			return nil, fmt.Errorf("encode frame %d: %w", i, err)
		}
		samples = append(samples, frame.Bytes())
		payload.Write(frame.Bytes())
	}

	timescale := uint32(fps * 100)
	delta := uint32(100)
	duration := uint32(frames) * delta

	ftyp := movBox("ftyp", []byte("qt  \x00\x00\x02\x00qt  "))
	// Two passes: the chunk offset table holds an absolute file offset, and it
	// cannot be known until the header it sits inside has a length.
	moov := movMoov(width, height, timescale, duration, delta, samples, 0)
	mdatStart := len(ftyp) + len(moov) + 8
	moov = movMoov(width, height, timescale, duration, delta, samples, uint32(mdatStart))

	var out bytes.Buffer
	out.Write(ftyp)
	out.Write(moov)
	out.Write(movBox("mdat", payload.Bytes()))
	return out.Bytes(), nil
}

func movBox(tag string, payload ...[]byte) []byte {
	size := 8
	for _, part := range payload {
		size += len(part)
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint32(size))
	out.WriteString(tag)
	for _, part := range payload {
		out.Write(part)
	}
	return out.Bytes()
}

// movMatrix is the identity transform every track carries.
var movMatrix = []uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000}

func movPack(values ...any) []byte {
	var out bytes.Buffer
	for _, value := range values {
		switch v := value.(type) {
		case []byte:
			out.Write(v)
		case string:
			out.WriteString(v)
		case []uint32:
			for _, n := range v {
				_ = binary.Write(&out, binary.BigEndian, n)
			}
		default:
			_ = binary.Write(&out, binary.BigEndian, v)
		}
	}
	return out.Bytes()
}

func movMoov(width, height int, timescale, duration, delta uint32, samples [][]byte, chunkOffset uint32) []byte {
	mvhd := movBox("mvhd", movPack(
		uint32(0), uint32(0), uint32(0), timescale, duration,
		uint32(0x00010000), uint16(0x0100), uint16(0), uint32(0), uint32(0),
		movMatrix, uint32(0), uint32(0), uint32(0), uint32(0), uint32(0), uint32(0), uint32(2),
	))

	tkhd := movBox("tkhd", movPack(
		uint32(0x00000007), uint32(0), uint32(0), uint32(1), uint32(0), duration,
		uint32(0), uint32(0), uint16(0), uint16(0), uint16(0), uint16(0),
		movMatrix, uint32(width)<<16, uint32(height)<<16,
	))

	mdhd := movBox("mdhd", movPack(uint32(0), uint32(0), uint32(0), timescale, duration, uint16(0x55c4), uint16(0)))
	hdlr := movBox("hdlr", movPack(uint32(0), "mhlrvide", uint32(0), uint32(0), uint32(0), byte(0)))
	vmhd := movBox("vmhd", movPack(uint32(0x00000001), uint16(0), uint16(0), uint16(0), uint16(0)))
	dref := movBox("dref", movPack(uint32(0), uint32(1), movBox("url ", movPack(uint32(1)))))
	dinf := movBox("dinf", dref)

	// A QuickTime visual sample entry. The compressor name is a pascal string
	// padded to 32 bytes, and the trailing 0xffff is "no colour table".
	compressor := make([]byte, 32)
	copy(compressor, append([]byte{6}, "Photo-"...))
	jpegEntry := movBox("jpeg", movPack(
		make([]byte, 6), uint16(1),
		uint16(0), uint16(0), uint32(0),
		uint32(512), uint32(512),
		uint16(width), uint16(height),
		uint32(0x00480000), uint32(0x00480000), uint32(0),
		uint16(1), compressor, uint16(24), uint16(0xffff),
	))
	stsd := movBox("stsd", movPack(uint32(0), uint32(1), jpegEntry))
	stts := movBox("stts", movPack(uint32(0), uint32(1), uint32(len(samples)), delta))
	stsc := movBox("stsc", movPack(uint32(0), uint32(1), uint32(1), uint32(len(samples)), uint32(1)))

	sizes := movPack(uint32(0), uint32(0), uint32(len(samples)))
	for _, sample := range samples {
		sizes = append(sizes, movPack(uint32(len(sample)))...)
	}
	stsz := movBox("stsz", sizes)
	stco := movBox("stco", movPack(uint32(0), uint32(1), chunkOffset))

	stbl := movBox("stbl", stsd, stts, stsc, stsz, stco)
	minf := movBox("minf", vmhd, dinf, stbl)
	mdia := movBox("mdia", mdhd, hdlr, minf)
	trak := movBox("trak", tkhd, mdia)
	return movBox("moov", mvhd, trak)
}

// bitWriter packs bits least significant first, which is the order VP8L reads
// them in.
type bitWriter struct {
	buf  bytes.Buffer
	acc  uint32
	bits uint
}

func (b *bitWriter) write(value uint32, count uint) {
	for i := uint(0); i < count; i++ {
		b.acc |= ((value >> i) & 1) << b.bits
		b.bits++
		if b.bits == 8 {
			b.buf.WriteByte(byte(b.acc))
			b.acc, b.bits = 0, 0
		}
	}
}

func (b *bitWriter) flush() []byte {
	if b.bits > 0 {
		b.buf.WriteByte(byte(b.acc))
		b.acc, b.bits = 0, 0
	}
	return b.buf.Bytes()
}

// writeSimpleCode emits a VP8L prefix code with one or two symbols. One symbol
// costs nothing per pixel; two cost a bit each. That is the whole reason the
// mock's stickers are two colours: it keeps the encoder to an afternoon rather
// than a huffman implementation.
func (b *bitWriter) writeSimpleCode(symbols ...uint32) {
	b.write(1, 1)                      // simple code
	b.write(uint32(len(symbols)-1), 1) // symbol count
	b.write(1, 1)                      // first symbol is 8 bits
	b.write(symbols[0], 8)
	if len(symbols) == 2 {
		b.write(symbols[1], 8)
	}
}

// synthWebP writes a real lossless WebP. Stickers are the one thing WhatsApp
// will only ever send as WebP, and a sticker that is secretly a png renders as
// a broken image in every frontend, so the mock encodes one properly.
func synthWebP(width, height int, seed string) ([]byte, error) {
	if width < 1 || height < 1 || width > 1<<14 || height > 1<<14 {
		return nil, fmt.Errorf("webp size %dx%d out of range", width, height)
	}
	base, accent := synthPalette(seed)

	var b bitWriter
	b.write(uint32(width-1), 14)
	b.write(uint32(height-1), 14)
	b.write(0, 1) // no alpha
	b.write(0, 3) // version
	b.write(0, 1) // no transform
	b.write(0, 1) // no colour cache
	b.write(0, 1) // no meta huffman image

	// Five prefix codes in the order the decoder reads them: green, red, blue,
	// alpha, distance. Two symbols each for the colour channels, one for alpha
	// and distance, which are constant.
	b.writeSimpleCode(uint32(base.G), uint32(accent.G))
	b.writeSimpleCode(uint32(base.R), uint32(accent.R))
	b.writeSimpleCode(uint32(base.B), uint32(accent.B))
	b.writeSimpleCode(255)
	b.writeSimpleCode(0)

	// One bit per channel per pixel, all three the same, so every pixel is one
	// of the two colours rather than a mix of their channels.
	band := height / 5
	if band < 1 {
		band = 1
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			bit := uint32(0)
			if (x+y)/band%2 == 1 {
				bit = 1
			}
			b.write(bit, 1)
			b.write(bit, 1)
			b.write(bit, 1)
		}
	}

	stream := append([]byte{0x2f}, b.flush()...)
	var chunk bytes.Buffer
	chunk.WriteString("VP8L")
	_ = binary.Write(&chunk, binary.LittleEndian, uint32(len(stream)))
	chunk.Write(stream)
	if len(stream)%2 == 1 {
		chunk.WriteByte(0)
	}

	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(4+chunk.Len()))
	out.WriteString("WEBP")
	out.Write(chunk.Bytes())
	return out.Bytes(), nil
}
