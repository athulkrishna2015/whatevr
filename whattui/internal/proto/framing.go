package proto

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"google.golang.org/protobuf/proto"
)

// maxFrameBytes is the daemon's own limit on a frame
const maxFrameBytes = 16 << 20

var errFrameTooBig = errors.New("a frame over 16 MiB")

// frameReader splits the socket into frames, and can say whether another
// whole one is already in hand: that is what decides when to redraw.
type frameReader struct {
	r   *bufio.Reader
	buf []byte
}

func newFrameReader(r io.Reader) *frameReader {
	return &frameReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// next is the next frame, nil with no error for one that does not decode:
// that is the daemon's bug, not a reason to drop the connection.
func (fr *frameReader) next() (*v2.Frame, error) {
	n, err := binary.ReadUvarint(fr.r)
	if err != nil {
		return nil, err
	}
	if n > maxFrameBytes {
		return nil, errFrameTooBig
	}
	if uint64(cap(fr.buf)) < n {
		fr.buf = make([]byte, n)
	}
	b := fr.buf[:n]
	if _, err := io.ReadFull(fr.r, b); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	// one big frame is no reason to hold 16 MiB for good
	if cap(fr.buf) > 1<<20 {
		fr.buf = nil
	}
	f := &v2.Frame{}
	if proto.Unmarshal(b, f) != nil {
		return nil, nil
	}
	return f, nil
}

// moreBuffered reports whether a whole further frame is already read. A
// partial one does not count: the rest of it is a network wait.
func (fr *frameReader) moreBuffered() bool {
	n := fr.r.Buffered()
	if n == 0 {
		return false
	}
	head, _ := fr.r.Peek(min(n, binary.MaxVarintLen64))
	size, k := binary.Uvarint(head)
	return k > 0 && uint64(n-k) >= size
}

// encode is f as it goes on the wire.
func encode(f *v2.Frame) ([]byte, error) {
	size := proto.Size(f)
	b := binary.AppendUvarint(make([]byte, 0, binary.MaxVarintLen64+size), uint64(size))
	return proto.MarshalOptions{}.MarshalAppend(b, f)
}
