package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// probed is what ffprobe says of a file we send.
type probed struct {
	w, h, seconds uint32
	// frame is a jpeg of an early frame, for a video's thumbnail
	frame []byte
}

const probeTimeout = 10 * time.Second

// probe reads a video's or audio's size and length, and a video's first
// good frame. false is no ffprobe, or a file it can't read.
func probe(ctx context.Context, path string) (probed, bool) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return probed{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-print_format", "json",
		"-show_entries", "format=duration:stream=codec_type,width,height", path).Output()
	if err != nil {
		return probed{}, false
	}
	var r struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &r) != nil {
		return probed{}, false
	}
	var p probed
	if d, err := strconv.ParseFloat(r.Format.Duration, 64); err == nil && d > 0 {
		p.seconds = uint32(math.Round(d))
	}
	video := false
	for _, s := range r.Streams {
		if s.Type == "video" && s.Width > 0 && s.Height > 0 {
			p.w, p.h, video = uint32(s.Width), uint32(s.Height), true
			break
		}
	}
	if video {
		if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
			var frame bytes.Buffer
			cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-i", path,
				"-vf", "thumbnail=30,"+posterScale, "-frames:v", "1", "-f", "image2", "-c:v", "mjpeg", "-")
			cmd.Stdout = &frame
			if cmd.Run() == nil && frame.Len() > 0 {
				p.frame = frame.Bytes()
			}
		}
	}
	return p, true
}

// readChunk caps a media_read
const readChunk = 1 << 20

// ReadFile is a chunk of a file the daemon put on a row, for a frontend that
// can't open the path itself. size is the whole file's.
func (c *Client) ReadFile(path string, offset uint64, limit uint32) (data []byte, size uint64, eof bool, err error) {
	if !filepath.IsAbs(path) {
		return nil, 0, false, Errorf(ErrInvalid, "the path must be absolute")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, 0, false, Errorf(ErrNotFound, "no such file")
	}
	if !c.ours(real) {
		return nil, 0, false, Errorf(ErrRejected, "not a file the daemon gave out")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, 0, false, Errorf(ErrNotFound, "no such file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, 0, false, Errorf(ErrNotFound, "no such file")
	}
	size = uint64(info.Size())
	if offset >= size {
		return nil, size, true, nil
	}
	n := uint64(readChunk)
	if limit > 0 && uint64(limit) < n {
		n = uint64(limit)
	}
	n = min(n, size-offset)
	data = make([]byte, n)
	got, err := f.ReadAt(data, int64(offset))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, false, err
	}
	data = data[:got]
	return data, size, offset+uint64(got) >= size, nil
}

// ours says path is under a directory the daemon writes its files to.
func (c *Client) ours(path string) bool {
	for _, dir := range []string{c.o.Paths.MediaCacheDir, c.o.Paths.CacheDir} {
		if dir == "" {
			continue
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(real, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}
