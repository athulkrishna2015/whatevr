package capture

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// flushEvery bounds what a crash loses.
const flushEvery = 500 * time.Millisecond

// Writer appends one segment. every hook calls it inline, so a write is an
// encode into a buffer under a lock and nothing more.
type Writer struct {
	dir     string
	segment int

	mu  sync.Mutex
	f   *os.File
	buf *bufio.Writer
	enc *json.Encoder
	seq uint64
	err error

	stop chan struct{}
	wg   sync.WaitGroup
}

// Open makes dir a capture if it is not one yet and starts its next segment
// with a start record.
func Open(dir, name string, start Start) (*Writer, error) {
	for _, sub := range []string{segmentsDir, blobsDir} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	if _, err := readMeta(dir); errors.Is(err, fs.ErrNotExist) {
		raw, _ := json.MarshalIndent(Meta{Format: Format, Name: name, Created: time.Now()}, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, metaFile), append(raw, '\n'), 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	segs, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	n := 1
	if len(segs) > 0 {
		n = segs[len(segs)-1] + 1
	}
	f, err := os.OpenFile(segmentPath(dir, n), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, segment: n, f: f, buf: bufio.NewWriterSize(f, 256<<10), stop: make(chan struct{})}
	w.enc = json.NewEncoder(w.buf)
	w.Write(Record{Kind: KindStart, Start: &start})
	w.wg.Add(1)
	go w.flushLoop()
	return w, nil
}

func (w *Writer) Dir() string  { return w.dir }
func (w *Writer) Segment() int { return w.segment }

// Write stamps r with the next seq (and now, if T is unset) and appends it.
func (w *Writer) Write(r Record) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0
	}
	w.seq++
	r.Seq = w.seq
	if r.T.IsZero() {
		r.T = time.Now()
	}
	if err := w.enc.Encode(&r); err != nil && w.err == nil {
		w.err = err
	}
	return r.Seq
}

// Err is the first write error, the hooks have nowhere to return one.
func (w *Writer) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *Writer) flushLoop() {
	defer w.wg.Done()
	t := time.NewTicker(flushEvery)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
			w.mu.Lock()
			if w.f != nil {
				if err := w.buf.Flush(); err != nil && w.err == nil {
					w.err = err
				}
			}
			w.mu.Unlock()
		}
	}
}

// Close writes the end record and syncs.
func (w *Writer) Close() error {
	w.Write(Record{Kind: KindEnd})
	close(w.stop)
	w.wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return w.err
	}
	err := errors.Join(w.buf.Flush(), w.f.Sync(), w.f.Close())
	w.f = nil
	if w.err == nil {
		w.err = err
	}
	return w.err
}

// Blob stores data by its sha256 and returns the name, "" when over BlobMax.
func (w *Writer) Blob(data []byte) (string, error) {
	b, err := w.NewBlob()
	if err != nil {
		return "", err
	}
	if _, err := b.Write(data); err != nil {
		b.Abort()
		return "", err
	}
	name, _, err := b.Finish()
	return name, err
}

// NewBlob streams a body into the blob store.
func (w *Writer) NewBlob() (*BlobWriter, error) {
	f, err := os.CreateTemp(filepath.Join(w.dir, blobsDir), ".tmp-*")
	if err != nil {
		return nil, err
	}
	return &BlobWriter{dir: filepath.Join(w.dir, blobsDir), f: f, h: sha256.New()}, nil
}

// BlobWriter keeps hashing and counting past BlobMax but stops storing.
type BlobWriter struct {
	dir  string
	f    *os.File
	h    hash.Hash
	size int64
	err  error
}

func (b *BlobWriter) Write(p []byte) (int, error) {
	b.h.Write(p)
	if b.f != nil && b.err == nil {
		if b.size+int64(len(p)) > BlobMax {
			b.drop()
		} else if _, err := b.f.Write(p); err != nil {
			b.err = err
		}
	}
	b.size += int64(len(p))
	return len(p), nil
}

func (b *BlobWriter) Size() int64 { return b.size }

func (b *BlobWriter) drop() {
	name := b.f.Name()
	b.f.Close()
	os.Remove(name)
	b.f = nil
}

// Finish returns the blob name ("" past BlobMax) and the body's sha256.
func (b *BlobWriter) Finish() (name, sum string, err error) {
	sum = hex.EncodeToString(b.h.Sum(nil))
	if b.f == nil {
		return "", sum, nil
	}
	if b.err != nil {
		b.drop()
		return "", sum, b.err
	}
	tmp := b.f.Name()
	if err := b.f.Close(); err != nil {
		os.Remove(tmp)
		b.f = nil
		return "", sum, err
	}
	b.f = nil
	final := filepath.Join(b.dir, sum)
	if _, err := os.Stat(final); err == nil {
		os.Remove(tmp)
		return sum, sum, nil
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", sum, fmt.Errorf("store blob: %w", err)
	}
	return sum, sum, nil
}

func (b *BlobWriter) Abort() {
	if b.f != nil {
		b.drop()
	}
}
