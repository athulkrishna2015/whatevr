package proto

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestLineReaderSplitsFrames(t *testing.T) {
	r := newLineReader(strings.NewReader("{\"a\":1}\n{\"b\":2}\n"), 1024)
	for _, want := range []string{`{"a":1}`, `{"b":2}`} {
		got, err := r.next()
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if string(got) != want {
			t.Errorf("frame = %q, want %q", got, want)
		}
	}
	if _, err := r.next(); !errors.Is(err, io.EOF) {
		t.Errorf("want EOF at the end, got %v", err)
	}
}

// endlessReader is a peer that keeps sending and never terminates a frame.
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestLineReaderRefusesAFrameOverTheCap(t *testing.T) {
	r := newLineReader(strings.NewReader(strings.Repeat("x", 1000)+"\n"), 100)
	if _, err := r.next(); !errors.Is(err, errFrameTooLong) {
		t.Errorf("err = %v, want errFrameTooLong", err)
	}
}

func TestLineReaderRefusesAFrameSittingExactlyOnTheCap(t *testing.T) {
	// The cap is a whole number of read buffers, so the allowance is banked
	// exactly and the terminator has nowhere left to go. This is the case that
	// hangs if the only check is on the next chunk: the peer keeps sending,
	// every chunk fits, and the frame never ends.
	r := newLineReader(endlessReader{}, 2*(64<<10))
	done := make(chan error, 1)
	go func() { _, err := r.next(); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, errFrameTooLong) {
			t.Errorf("err = %v, want errFrameTooLong", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("next blocked on a frame that can never be delivered")
	}
}

func TestLineReaderReportsEOFOnATruncatedFinalFrame(t *testing.T) {
	// Short of the cap but with no terminator and nothing more coming. The
	// connection ended mid-frame, which is not the same thing as a peer
	// flooding us, and it should not be reported as one.
	r := newLineReader(strings.NewReader(strings.Repeat("x", 50)), 100)
	if _, err := r.next(); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want EOF", err)
	}
}

func TestLineReaderSeesAWholeFrameAlreadyInHand(t *testing.T) {
	r := newLineReader(strings.NewReader("{\"a\":1}\n{\"b\":2}\n"), 1024)
	if _, err := r.next(); err != nil {
		t.Fatal(err)
	}
	if !r.moreBuffered() {
		t.Error("moreBuffered = false with a whole frame still buffered")
	}
	if _, err := r.next(); err != nil {
		t.Fatal(err)
	}
	if r.moreBuffered() {
		t.Error("moreBuffered = true with nothing left")
	}
}

func TestLineReaderDoesNotCountAPartialFrame(t *testing.T) {
	r := newLineReader(strings.NewReader("{\"a\":1}\n{\"b\":"), 1024)
	if _, err := r.next(); err != nil {
		t.Fatal(err)
	}
	if r.moreBuffered() {
		t.Error("a partial frame counted as buffered; a batch would stay open across a network wait")
	}
}
