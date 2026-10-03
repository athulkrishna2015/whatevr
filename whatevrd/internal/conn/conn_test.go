package conn

import (
	"testing"
	"time"
)

func TestBackoffDoublesToTheCapWithJitterInTheUpperHalf(t *testing.T) {
	low := func() float64 { return 0 }
	high := func() float64 { return 0.999999 }
	for n, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 6: 32 * time.Second, 7: 60 * time.Second, 50: 60 * time.Second} {
		if got := Backoff(n, time.Second, time.Minute, low); got != want/2 {
			t.Errorf("attempt %d low %v, want %v", n, got, want/2)
		}
		if got := Backoff(n, time.Second, time.Minute, high); got < want-time.Millisecond || got > want {
			t.Errorf("attempt %d high %v, want about %v", n, got, want)
		}
	}
}
