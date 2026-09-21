//go:build whatevr_mock

package wamock

import (
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"

	"go.mau.fi/whatsmeow/util/gcmutil"
)

// The responder half of Noise_XX_25519_AESGCM_SHA256, mirroring
// whatsmeow/socket.NoiseHandshake step for step. We cannot reuse that type
// directly: its salt is unexported and its Finish hands the derived keys to a
// client-side FrameSocket, so the server needs its own copy of the state
// machine. Every operation here has a counterpart in whatsmeow's doHandshake,
// and the two must stay in the same order or the transcript hashes diverge.
type noiseState struct {
	hash    []byte
	salt    []byte
	key     cipher.AEAD
	counter uint32
}

func newNoiseState(pattern string, header []byte) *noiseState {
	ns := &noiseState{}
	data := []byte(pattern)
	if len(data) == 32 {
		ns.hash = data
	} else {
		sum := sha256.Sum256(data)
		ns.hash = sum[:]
	}
	ns.salt = ns.hash
	key, err := gcmutil.Prepare(ns.hash)
	if err != nil {
		// The pattern is a compile-time constant, so this cannot fail.
		panic(err)
	}
	ns.key = key
	ns.authenticate(header)
	return ns
}

func (ns *noiseState) authenticate(data []byte) {
	sum := sha256.Sum256(append(ns.hash, data...))
	ns.hash = sum[:]
}

func (ns *noiseState) nextIV() []byte {
	iv := make([]byte, 12)
	binary.BigEndian.PutUint32(iv[8:], ns.counter)
	ns.counter++
	return iv
}

func (ns *noiseState) encrypt(plaintext []byte) []byte {
	ciphertext := ns.key.Seal(nil, ns.nextIV(), plaintext, ns.hash)
	ns.authenticate(ciphertext)
	return ciphertext
}

func (ns *noiseState) decrypt(ciphertext []byte) ([]byte, error) {
	plaintext, err := ns.key.Open(nil, ns.nextIV(), ciphertext, ns.hash)
	if err != nil {
		return nil, err
	}
	ns.authenticate(ciphertext)
	return plaintext, nil
}

func (ns *noiseState) mixSharedSecret(priv, pub [32]byte) error {
	secret, err := curve25519.X25519(priv[:], pub[:])
	if err != nil {
		return fmt.Errorf("x25519: %w", err)
	}
	return ns.mixIntoKey(secret)
}

func (ns *noiseState) mixIntoKey(data []byte) error {
	ns.counter = 0
	write, read, err := expand(ns.salt, data)
	if err != nil {
		return err
	}
	ns.salt = write
	ns.key, err = gcmutil.Prepare(read)
	if err != nil {
		return fmt.Errorf("prepare cipher: %w", err)
	}
	return nil
}

// finish derives the transport keys. The client calls the same expansion and
// takes write first, read second, so the server takes them the other way round.
func (ns *noiseState) finish() (write, read cipher.AEAD, err error) {
	clientWrite, clientRead, err := expand(ns.salt, nil)
	if err != nil {
		return nil, nil, err
	}
	if write, err = gcmutil.Prepare(clientRead); err != nil {
		return nil, nil, fmt.Errorf("prepare write cipher: %w", err)
	}
	if read, err = gcmutil.Prepare(clientWrite); err != nil {
		return nil, nil, fmt.Errorf("prepare read cipher: %w", err)
	}
	return write, read, nil
}

func expand(salt, data []byte) (write, read []byte, err error) {
	h := hkdf.New(sha256.New, data, salt, nil)
	write = make([]byte, 32)
	read = make([]byte, 32)
	if _, err = io.ReadFull(h, write); err != nil {
		return nil, nil, fmt.Errorf("read write key: %w", err)
	}
	if _, err = io.ReadFull(h, read); err != nil {
		return nil, nil, fmt.Errorf("read read key: %w", err)
	}
	return write, read, nil
}
