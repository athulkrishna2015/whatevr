//go:build whatevr_mock

package wamock

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"go.mau.fi/libsignal/ecc"
	groupRecord "go.mau.fi/libsignal/groups/state/record"
	"go.mau.fi/libsignal/keys/identity"
	"go.mau.fi/libsignal/keys/prekey"
	"go.mau.fi/libsignal/protocol"
	signalSession "go.mau.fi/libsignal/session"
	"go.mau.fi/libsignal/state/record"
	signalStore "go.mau.fi/libsignal/state/store"
	"go.mau.fi/libsignal/util/optional"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/util/keys"
)

// pbSerializer is the same serializer whatsmeow uses. Both sides of a session
// have to agree on the record encoding, and there is only one.
var pbSerializer = store.SignalProtobufSerializer

var errNoClientKeys = errors.New("client has not uploaded its prekeys yet")

// memSignalStore is one mock device's Signal state. Every peer in the world is
// a separate device with its own identity key, so every peer gets one of these.
// It lives in memory only: a mock run that survives a restart would be a mock
// run whose state we cannot describe.
type memSignalStore struct {
	mu sync.Mutex

	identity       *identity.KeyPair
	registrationID uint32

	sessions   map[string]*record.Session
	identities map[string][32]byte
	senderKeys map[string]*groupRecord.SenderKey
}

var _ signalStore.SignalProtocol = (*memSignalStore)(nil)

// newSignalStore builds one device's Signal state. Pass an identity to pin the
// device's key, or nil to draw a fresh one from the seed.
func newSignalStore(r io.Reader, pair *keys.KeyPair) (*memSignalStore, error) {
	if pair == nil {
		var err error
		if pair, err = genKeyPair(r); err != nil {
			return nil, fmt.Errorf("identity key: %w", err)
		}
	}
	var regBytes [2]byte
	if _, err := io.ReadFull(r, regBytes[:]); err != nil {
		return nil, fmt.Errorf("registration id: %w", err)
	}
	return &memSignalStore{
		identity: identity.NewKeyPair(
			identity.NewKey(ecc.NewDjbECPublicKey(*pair.Pub)),
			ecc.NewDjbECPrivateKey(*pair.Priv),
		),
		// The valid range is 1 to 16380, same as a real install picks once.
		registrationID: uint32(binary.BigEndian.Uint16(regBytes[:])%16380) + 1,
		sessions:       map[string]*record.Session{},
		identities:     map[string][32]byte{},
		senderKeys:     map[string]*groupRecord.SenderKey{},
	}, nil
}

func (m *memSignalStore) GetIdentityKeyPair() *identity.KeyPair { return m.identity }
func (m *memSignalStore) GetLocalRegistrationID() uint32        { return m.registrationID }

func (m *memSignalStore) SaveIdentity(_ context.Context, address *protocol.SignalAddress, key *identity.Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.identities[address.String()] = key.PublicKey().PublicKey()
	return nil
}

// IsTrustedIdentity is trust on first use, like the real one, except that the
// mock only ever talks to the one client it paired with.
func (m *memSignalStore) IsTrustedIdentity(_ context.Context, address *protocol.SignalAddress, key *identity.Key) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	known, ok := m.identities[address.String()]
	if !ok {
		return true, nil
	}
	return known == key.PublicKey().PublicKey(), nil
}

func (m *memSignalStore) LoadSession(_ context.Context, address *protocol.SignalAddress) (*record.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[address.String()]; ok {
		return sess, nil
	}
	return record.NewSession(pbSerializer.Session, pbSerializer.State), nil
}

func (m *memSignalStore) StoreSession(_ context.Context, address *protocol.SignalAddress, rec *record.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[address.String()] = rec
	return nil
}

func (m *memSignalStore) ContainsSession(_ context.Context, address *protocol.SignalAddress) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[address.String()]
	return ok, nil
}

func (m *memSignalStore) DeleteSession(_ context.Context, address *protocol.SignalAddress) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, address.String())
	return nil
}

func (m *memSignalStore) DeleteAllSessions(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = map[string]*record.Session{}
	return nil
}

func (m *memSignalStore) GetSubDeviceSessions(context.Context, string) ([]uint32, error) {
	return nil, nil
}

func (m *memSignalStore) StoreSenderKey(_ context.Context, name *protocol.SenderKeyName, rec *groupRecord.SenderKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.senderKeys[senderKeyKey(name)] = rec
	return nil
}

func (m *memSignalStore) LoadSenderKey(_ context.Context, name *protocol.SenderKeyName) (*groupRecord.SenderKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.senderKeys[senderKeyKey(name)]; ok {
		return rec, nil
	}
	return groupRecord.NewSenderKey(pbSerializer.SenderKeyRecord, pbSerializer.SenderKeyState), nil
}

// The mock is always the initiator, so nothing ever asks it for its own
// prekeys. They exist to satisfy the interface.
func (m *memSignalStore) LoadPreKey(context.Context, uint32) (*record.PreKey, error) { return nil, nil }
func (m *memSignalStore) StorePreKey(context.Context, uint32, *record.PreKey) error  { return nil }
func (m *memSignalStore) ContainsPreKey(context.Context, uint32) (bool, error)       { return false, nil }
func (m *memSignalStore) RemovePreKey(context.Context, uint32) error                 { return nil }

func (m *memSignalStore) LoadSignedPreKey(context.Context, uint32) (*record.SignedPreKey, error) {
	return nil, nil
}
func (m *memSignalStore) LoadSignedPreKeys(context.Context) ([]*record.SignedPreKey, error) {
	return nil, nil
}
func (m *memSignalStore) StoreSignedPreKey(context.Context, uint32, *record.SignedPreKey) error {
	return nil
}
func (m *memSignalStore) ContainsSignedPreKey(context.Context, uint32) (bool, error) {
	return false, nil
}
func (m *memSignalStore) RemoveSignedPreKey(context.Context, uint32) error { return nil }

// senderKeyKey names a group sender key. SenderKeyName has no String of its
// own, so the two halves get joined here.
func senderKeyKey(name *protocol.SenderKeyName) string {
	return name.GroupID() + "::" + name.Sender().String()
}

// peer is one mock device's end of the conversation with the paired client:
// its addressing identity plus the Signal state it encrypts under.
type peer struct {
	// jid is what goes in the stanza's from or participant attribute.
	jid types.JID
	// encJID is the address the client will decrypt under, which is not always
	// the same thing. whatsmeow prefers a LID whenever it knows one, and the
	// one LID it always knows is the account's own.
	encJID types.JID

	signal *memSignalStore

	mu      sync.Mutex
	started bool
}

// peerFor returns the mock device that speaks as jid, creating it on first use.
func (s *Server) peerFor(jid, encJID types.JID) (*peer, error) {
	key := jid.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.peers[key]; ok {
		return p, nil
	}
	// The account's own device does not get to pick its identity key: the
	// client wrote the account signature key down as that address's identity
	// when it paired, and a different one reads as the account having been
	// taken over.
	var identityPair *keys.KeyPair
	if encJID.Server == types.HiddenUserServer && encJID.User == s.opts.AccountPhone {
		identityPair = s.account
	}
	sig, err := newSignalStore(s.rng, identityPair)
	if err != nil {
		return nil, err
	}
	p := &peer{jid: jid, encJID: encJID, signal: sig}
	s.peers[key] = p
	return p, nil
}

// clientBundle turns the prekeys the client uploaded into the bundle an X3DH
// needs. One one-time prekey is burned per new peer, exactly as a real server
// hands one out per requester.
func (s *Server) clientBundle(deviceID uint32) (*prekey.Bundle, error) {
	s.keys.mu.Lock()
	defer s.keys.mu.Unlock()
	if s.keys.identityKey == nil || s.keys.signedPreKey == nil || s.keys.signedPreKey.Signature == nil {
		return nil, errNoClientKeys
	}
	signed := s.keys.signedPreKey
	identityKey := identity.NewKey(ecc.NewDjbECPublicKey(*s.keys.identityKey))

	var oneTime *keys.PreKey
	if len(s.keys.preKeys) > 0 {
		if s.keys.consumed < len(s.keys.preKeys) {
			oneTime = s.keys.preKeys[s.keys.consumed]
			s.keys.consumed++
		} else {
			// Running out is not fatal: reusing the last one still produces a
			// session the client accepts, and a scenario with more peers than
			// uploaded prekeys is not a case worth failing over.
			oneTime = s.keys.preKeys[len(s.keys.preKeys)-1]
		}
	}
	if oneTime == nil {
		return prekey.NewBundle(s.keys.registrationID, deviceID,
			optional.NewEmptyUint32(), signed.KeyID,
			nil, ecc.NewDjbECPublicKey(*signed.Pub), *signed.Signature, identityKey), nil
	}
	return prekey.NewBundle(s.keys.registrationID, deviceID,
		optional.NewOptionalUint32(oneTime.KeyID), signed.KeyID,
		ecc.NewDjbECPublicKey(*oneTime.Pub), ecc.NewDjbECPublicKey(*signed.Pub),
		*signed.Signature, identityKey), nil
}

// encrypt builds the <enc> node for one plaintext. The first message to a
// client establishes the session from its prekey bundle and comes out as a
// pkmsg; everything after it rides the ratchet as a msg.
func (p *peer) encrypt(ctx context.Context, srv *Server, to types.JID, plaintext []byte) (waBinary.Node, error) {
	addr := to.SignalAddress()

	p.mu.Lock()
	defer p.mu.Unlock()

	builder := signalSession.NewBuilderFromSignal(p.signal, addr, pbSerializer)
	if !p.started {
		bundle, err := srv.clientBundle(uint32(to.Device))
		if err != nil {
			return waBinary.Node{}, err
		}
		if err := builder.ProcessBundle(ctx, bundle); err != nil {
			return waBinary.Node{}, fmt.Errorf("process prekey bundle: %w", err)
		}
		p.started = true
	}

	ciphertext, err := signalSession.NewCipher(builder, addr).Encrypt(ctx, padMessage(plaintext, srv.rng))
	if err != nil {
		return waBinary.Node{}, fmt.Errorf("encrypt: %w", err)
	}
	encType := "msg"
	if ciphertext.Type() == protocol.PREKEY_TYPE {
		encType = "pkmsg"
	}
	return waBinary.Node{
		Tag:     "enc",
		Attrs:   waBinary.Attrs{"v": "2", "type": encType},
		Content: ciphertext.Serialize(),
	}, nil
}

// padMessage is whatsmeow's v2 padding, which is unexported there: between one
// and fifteen trailing bytes, each holding the pad length. The client's
// unpadMessage rejects anything else.
func padMessage(plaintext []byte, r *seededRand) []byte {
	pad := r.bytes(1)
	pad[0] &= 0xf
	if pad[0] == 0 {
		pad[0] = 0xf
	}
	return append(plaintext, bytes.Repeat(pad, int(pad[0]))...)
}
