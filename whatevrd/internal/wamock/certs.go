//go:build whatevr_mock

package wamock

import (
	"fmt"
	"io"
	"time"

	"go.mau.fi/libsignal/ecc"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCert"
	"go.mau.fi/whatsmeow/util/keys"
	"google.golang.org/protobuf/proto"
)

// certValidity is how long the fabricated chain claims to be good for.
// whatsmeow's checkCertValidity reads notAfter as a unix timestamp and treats
// an unset field as 1970, which reads as expired, so this is not optional.
const certValidity = 10 * 365 * 24 * time.Hour

// serverIdentity is the key material the fake server presents during the Noise
// handshake: a root whose public half replaces whatsmeow.WACertPubKey, an
// intermediate it signs, and the static noise key the leaf attests to.
//
// All three are derived from the scenario seed rather than crypto/rand, so a
// mock run is byte-reproducible and a failing handshake can be replayed.
type serverIdentity struct {
	root         *keys.KeyPair
	intermediate *keys.KeyPair
	static       *keys.KeyPair

	chain []byte
}

func genKeyPair(r io.Reader) (*keys.KeyPair, error) {
	var priv [32]byte
	if _, err := io.ReadFull(r, priv[:]); err != nil {
		return nil, fmt.Errorf("read key material: %w", err)
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	return keys.NewKeyPairFromPrivateKey(priv), nil
}

func newServerIdentity(r io.Reader, now time.Time) (*serverIdentity, error) {
	root, err := genKeyPair(r)
	if err != nil {
		return nil, err
	}
	intermediate, err := genKeyPair(r)
	if err != nil {
		return nil, err
	}
	static, err := genKeyPair(r)
	if err != nil {
		return nil, err
	}
	id := &serverIdentity{root: root, intermediate: intermediate, static: static}
	if id.chain, err = id.buildChain(now); err != nil {
		return nil, err
	}
	return id, nil
}

// buildChain produces the two-level CertChain that whatsmeow's verifyServerCert
// walks: the root signs the intermediate's details, the intermediate signs the
// leaf's, and the leaf's key must equal the static key the client just
// decrypted. The serials have to line up too, with the intermediate issued by
// serial 0 and the leaf issued by the intermediate's own serial.
func (id *serverIdentity) buildChain(now time.Time) ([]byte, error) {
	const intermediateSerial = 1

	notBefore := uint64(now.Add(-certValidity).Unix())
	notAfter := uint64(now.Add(certValidity).Unix())

	intermediateDetails, err := proto.Marshal(&waCert.CertChain_NoiseCertificate_Details{
		Serial:       proto.Uint32(intermediateSerial),
		IssuerSerial: proto.Uint32(whatsmeow.WACertIssuerSerial),
		Key:          id.intermediate.Pub[:],
		NotBefore:    proto.Uint64(notBefore),
		NotAfter:     proto.Uint64(notAfter),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal intermediate details: %w", err)
	}
	leafDetails, err := proto.Marshal(&waCert.CertChain_NoiseCertificate_Details{
		Serial:       proto.Uint32(intermediateSerial + 1),
		IssuerSerial: proto.Uint32(intermediateSerial),
		Key:          id.static.Pub[:],
		NotBefore:    proto.Uint64(notBefore),
		NotAfter:     proto.Uint64(notAfter),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal leaf details: %w", err)
	}

	intermediateSig := ecc.CalculateSignature(ecc.NewDjbECPrivateKey(*id.root.Priv), intermediateDetails)
	leafSig := ecc.CalculateSignature(ecc.NewDjbECPrivateKey(*id.intermediate.Priv), leafDetails)

	chain, err := proto.Marshal(&waCert.CertChain{
		Intermediate: &waCert.CertChain_NoiseCertificate{
			Details:   intermediateDetails,
			Signature: intermediateSig[:],
		},
		Leaf: &waCert.CertChain_NoiseCertificate{
			Details:   leafDetails,
			Signature: leafSig[:],
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cert chain: %w", err)
	}
	return chain, nil
}
