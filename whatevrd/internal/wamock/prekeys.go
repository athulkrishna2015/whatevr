//go:build whatevr_mock

package wamock

import (
	"encoding/binary"
	"sync"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/util/keys"
)

// clientKeys is what the client uploaded about itself. The server needs it to
// build Signal sessions in the other direction, so every bundle the client
// publishes is kept rather than counted.
type clientKeys struct {
	mu sync.Mutex

	registrationID uint32
	identityKey    *[32]byte
	signedPreKey   *keys.PreKey
	preKeys        []*keys.PreKey
	consumed       int
}

func (s *Server) preKeyCount() int {
	s.keys.mu.Lock()
	defer s.keys.mu.Unlock()
	return len(s.keys.preKeys) - s.keys.consumed
}

// capturePreKeys reads an <iq set xmlns=encrypt> upload. Anything malformed is
// skipped rather than fatal: a mock that refuses to boot because one prekey
// parsed oddly is worse than one that has fewer prekeys than it thought.
func (s *Server) capturePreKeys(node *waBinary.Node) {
	s.keys.mu.Lock()
	defer s.keys.mu.Unlock()

	if reg, ok := node.GetChildByTag("registration").Content.([]byte); ok && len(reg) == 4 {
		s.keys.registrationID = binary.BigEndian.Uint32(reg)
	}
	if identity, ok := node.GetChildByTag("identity").Content.([]byte); ok && len(identity) == 32 {
		s.keys.identityKey = (*[32]byte)(identity)
	}
	if skey, ok := node.GetOptionalChildByTag("skey"); ok {
		if parsed := parsePreKeyNode(skey); parsed != nil {
			s.keys.signedPreKey = parsed
		}
	}
	list, ok := node.GetOptionalChildByTag("list")
	if !ok {
		return
	}
	for _, child := range list.GetChildren() {
		if child.Tag != "key" {
			continue
		}
		if parsed := parsePreKeyNode(child); parsed != nil {
			s.keys.preKeys = append(s.keys.preKeys, parsed)
		}
	}
	s.log.Printf("captured %d prekeys, registration %d", len(s.keys.preKeys), s.keys.registrationID)
	if s.keys.identityKey != nil && s.keys.signedPreKey != nil {
		s.keysOnce.Do(func() { close(s.keysReady) })
	}
}

// parsePreKeyNode mirrors whatsmeow's nodeToPreKey: a three-byte big-endian id,
// a 32-byte public value, and for a signed prekey a 64-byte signature.
func parsePreKeyNode(node waBinary.Node) *keys.PreKey {
	idBytes, ok := node.GetChildByTag("id").Content.([]byte)
	if !ok || len(idBytes) != 3 {
		return nil
	}
	pub, ok := node.GetChildByTag("value").Content.([]byte)
	if !ok || len(pub) != 32 {
		return nil
	}
	key := &keys.PreKey{
		KeyPair: keys.KeyPair{Pub: (*[32]byte)(pub)},
		KeyID:   binary.BigEndian.Uint32([]byte{0, idBytes[0], idBytes[1], idBytes[2]}),
	}
	if sig, ok := node.GetChildByTag("signature").Content.([]byte); ok && len(sig) == 64 {
		key.Signature = (*[64]byte)(sig)
	}
	return key
}
