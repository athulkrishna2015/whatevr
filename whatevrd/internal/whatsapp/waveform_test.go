package whatsapp

import (
	"context"
	"math"
	"math/rand/v2"
	"os/exec"
	"path/filepath"
	"testing"
)

// exact is the envelope from all the pcm at once, sample by sample
func exact(pcm []byte) []byte {
	samples := len(pcm) / 2
	if samples < waveformBuckets {
		return nil
	}
	sums := make([]float64, waveformBuckets)
	counts := make([]int, waveformBuckets)
	for i := range samples {
		b := i * waveformBuckets / samples
		v := float64(int16(uint16(pcm[i*2]) | uint16(pcm[i*2+1])<<8))
		sums[b] += v * v
		counts[b]++
	}
	peak := 0.0
	rms := make([]float64, waveformBuckets)
	for i := range rms {
		rms[i] = math.Sqrt(sums[i] / float64(counts[i]))
		peak = max(peak, rms[i])
	}
	out := make([]byte, waveformBuckets)
	for i, v := range rms {
		out[i] = byte(min(math.Round(v/peak*100), 100))
	}
	return out
}

func TestTheEnvelopeMatchesTheWholePCM(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, samples := range []int{63, 64, 100, 3 * 8000, envelopeChunks, envelopeChunks + 1, 10 * 60 * 8000} {
		pcm := make([]byte, samples*2)
		for i := range samples {
			// a voice rising and falling, so the buckets differ
			amp := 2000 + 12000*math.Abs(math.Sin(float64(i)/float64(samples)*7))
			v := int16(amp * (r.Float64()*2 - 1))
			pcm[2*i], pcm[2*i+1] = byte(v), byte(uint16(v)>>8)
		}
		var e envelope
		// odd writes, so a sample splits across them
		for b := pcm; len(b) > 0; {
			k := min(len(b), 1+r.IntN(4097))
			e.Write(b[:k])
			b = b[k:]
		}
		got, want := e.buckets(), exact(pcm)
		if len(got) != len(want) {
			t.Fatalf("%d samples: %d buckets, want %d", samples, len(got), len(want))
		}
		// the two chunks at a bucket's edges are at most 1/16 of it
		for i := range got {
			if d := int(got[i]) - int(want[i]); d < -3 || d > 3 {
				t.Fatalf("%d samples: bucket %d is %d, want %d", samples, i, got[i], want[i])
			}
		}
		if len(e.sums) > envelopeChunks {
			t.Fatalf("%d samples kept %d chunks", samples, len(e.sums))
		}
	}
}

func TestAVoiceNoteIsReadAsItStreams(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	path := filepath.Join(t.TempDir(), "note.ogg")
	// quiet for a second, then loud
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=300:duration=3",
		"-af", "volume='if(lt(t,1),0.05,1)':eval=frame", "-c:a", "libopus", path).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	wf, err := waveform(context.Background(), path)
	if err != nil || len(wf) != waveformBuckets {
		t.Fatalf("%v %v", wf, err)
	}
	if wf[5] > 20 || wf[60] < 80 {
		t.Fatalf("quiet %d loud %d", wf[5], wf[60])
	}
}
