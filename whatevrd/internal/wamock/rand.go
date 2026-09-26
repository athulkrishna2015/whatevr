//go:build whatevr_mock

package wamock

import (
	"encoding/hex"
	"math/rand"
	"strings"
	"sync"
)

// seededRand is the only source of randomness in the mock. Keys, message ids
// and pairing refs all come from here so a scenario replays identically, which
// is what lets whattui compare golden frames at all.
type seededRand struct {
	mu sync.Mutex
	r  *rand.Rand
}

func newSeededRand(seed int64) *seededRand {
	return &seededRand{r: rand.New(rand.NewSource(seed))}
}

func (s *seededRand) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.Read(p)
}

func (s *seededRand) bytes(n int) []byte {
	buf := make([]byte, n)
	_, _ = s.Read(buf)
	return buf
}

// messageID matches the shape WhatsApp hands out for server-originated
// messages: uppercase hex, which is what whatsmeow's own generator produces.
func (s *seededRand) messageID() string {
	return strings.ToUpper(hex.EncodeToString(s.bytes(8)))
}

func (s *seededRand) stanzaID() string {
	return hex.EncodeToString(s.bytes(8))
}
