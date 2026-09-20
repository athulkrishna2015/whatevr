package proto

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

var errFrameTooLong = errors.New("frame exceeds the maximum line length")

// lineReader splits the socket into NDJSON frames with a hard cap on any one
// of them, and can say whether another whole frame is already in hand. That
// second question is what closes a batch: a window fill arrives as one read,
// and settling it once instead of per item is the difference between one
// layout pass and eighty.
type lineReader struct {
	r   *bufio.Reader
	max int
	buf []byte
}

func newLineReader(r io.Reader, max int) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 64<<10), max: max}
}

// next returns the next frame, without its terminator. The slice is only valid
// until the following call.
func (lr *lineReader) next() ([]byte, error) {
	lr.buf = lr.buf[:0]
	for {
		chunk, err := lr.r.ReadSlice('\n')
		if len(lr.buf)+len(chunk) > lr.max {
			return nil, errFrameTooLong
		}
		lr.buf = append(lr.buf, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			// The read buffer filled with no terminator in it. Banking the
			// whole allowance and still having no frame means the terminator
			// cannot arrive in time, and waiting for it would block forever on
			// a peer that is never going to send one.
			if len(lr.buf) >= lr.max {
				return nil, errFrameTooLong
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		return bytes.TrimRight(lr.buf, "\r\n"), nil
	}
}

// moreBuffered reports whether a whole further frame is already read. A
// partial one does not count: waiting on the rest of it is a network wait, and
// a batch that stays open across one is a frame the user does not see.
func (lr *lineReader) moreBuffered() bool {
	n := lr.r.Buffered()
	if n == 0 {
		return false
	}
	peeked, err := lr.r.Peek(n)
	if err != nil {
		return false
	}
	return bytes.IndexByte(peeked, '\n') >= 0
}
