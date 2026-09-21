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
	"go.mau.fi/libsignal/groups"
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

	signed  *keys.PreKey
	preKeys map[uint32]*keys.PreKey
}

var _ signalStore.SignalProtocol = (*memSignalStore)(nil)

// newSignalStore builds one device's Signal state. Pass an identity to pin the
// device's key, or nil to draw a fresh one from the seed; the pair that ended
// up being used comes back, because prekeys have to be signed with it.
func newSignalStore(r io.Reader, pair *keys.KeyPair) (*memSignalStore, *keys.KeyPair, error) {
	if pair == nil {
		var err error
		if pair, err = genKeyPair(r); err != nil {
			return nil, nil, fmt.Errorf("identity key: %w", err)
		}
	}
	var regBytes [2]byte
	if _, err := io.ReadFull(r, regBytes[:]); err != nil {
		return nil, nil, fmt.Errorf("registration id: %w", err)
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
		preKeys:        map[uint32]*keys.PreKey{},
	}, pair, nil
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

// setPreKeys publishes the device's own key material. It is what makes the
// mock answerable as well as able to initiate: a client opening a session with
// one of these devices sends a pkmsg that only these keys can open.
func (m *memSignalStore) setPreKeys(signed *keys.PreKey, oneTime []*keys.PreKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.signed = signed
	m.preKeys = make(map[uint32]*keys.PreKey, len(oneTime))
	for _, key := range oneTime {
		m.preKeys[key.KeyID] = key
	}
}

func eccPair(pair keys.KeyPair) *ecc.ECKeyPair {
	return ecc.NewECKeyPair(ecc.NewDjbECPublicKey(*pair.Pub), ecc.NewDjbECPrivateKey(*pair.Priv))
}

func (m *memSignalStore) LoadPreKey(_ context.Context, id uint32) (*record.PreKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.preKeys[id]
	if !ok {
		return nil, nil
	}
	return record.NewPreKey(key.KeyID, eccPair(key.KeyPair), nil), nil
}

func (m *memSignalStore) StorePreKey(context.Context, uint32, *record.PreKey) error { return nil }

func (m *memSignalStore) ContainsPreKey(_ context.Context, id uint32) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.preKeys[id]
	return ok, nil
}

// RemovePreKey is deliberately a no-op. A real device burns a prekey on use;
// a mock that did would fail a reconnect, which is a state scenarios rely on.
func (m *memSignalStore) RemovePreKey(context.Context, uint32) error { return nil }

func (m *memSignalStore) LoadSignedPreKey(_ context.Context, id uint32) (*record.SignedPreKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.signed == nil || m.signed.KeyID != id {
		return nil, nil
	}
	return record.NewSignedPreKey(id, 0, eccPair(m.signed.KeyPair), *m.signed.Signature, nil), nil
}

func (m *memSignalStore) LoadSignedPreKeys(context.Context) ([]*record.SignedPreKey, error) {
	return nil, nil
}
func (m *memSignalStore) StoreSignedPreKey(context.Context, uint32, *record.SignedPreKey) error {
	return nil
}
func (m *memSignalStore) ContainsSignedPreKey(_ context.Context, id uint32) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.signed != nil && m.signed.KeyID == id, nil
}
func (m *memSignalStore) RemoveSignedPreKey(context.Context, uint32) error { return nil }

// senderKeyKey names a group sender key. SenderKeyName has no String of its
// own, so the two halves get joined here.
func senderKeyKey(name *protocol.SenderKeyName) string {
	return name.GroupID() + "::" + name.Sender().String()
}

// peerPreKeyCount is how many one-time prekeys a mock device publishes. The
// client burns one per session it opens, and it only ever opens one.
const peerPreKeyCount = 8

// peer is one mock device's end of the conversation with the paired client:
// its addressing identity, the Signal state it encrypts under, and the key
// material it hands out when the client asks for a session.
type peer struct {
	// jid is what goes in the stanza's from or participant attribute.
	jid types.JID
	// encJID is the address the client will decrypt under, which is not always
	// the same thing. whatsmeow prefers a LID whenever it knows one, and the
	// one LID it always knows is the account's own.
	encJID types.JID

	signal   *memSignalStore
	identity *keys.KeyPair
	signed   *keys.PreKey
	oneTime  []*keys.PreKey

	mu       sync.Mutex
	started  bool
	consumed int
}

// peerFor returns the mock device that speaks as jid, creating it on first use.
func (s *Server) peerFor(jid, encJID types.JID) (*peer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peerForLocked(jid, encJID)
}

func (s *Server) peerForLocked(jid, encJID types.JID) (*peer, error) {
	key := jid.String()
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
	sig, identityPair, err := newSignalStore(s.rng, identityPair)
	if err != nil {
		return nil, err
	}
	signed, err := newSignedPreKey(s.rng, identityPair, 1)
	if err != nil {
		return nil, err
	}
	oneTime := make([]*keys.PreKey, 0, peerPreKeyCount)
	for i := 0; i < peerPreKeyCount; i++ {
		pre, err := newPreKey(s.rng, uint32(i+1))
		if err != nil {
			return nil, err
		}
		oneTime = append(oneTime, pre)
	}
	sig.setPreKeys(signed, oneTime)
	p := &peer{
		jid:      jid,
		encJID:   encJID,
		signal:   sig,
		identity: identityPair,
		signed:   signed,
		oneTime:  oneTime,
	}
	s.peers[key] = p
	return p, nil
}

func newPreKey(r io.Reader, id uint32) (*keys.PreKey, error) {
	pair, err := genKeyPair(r)
	if err != nil {
		return nil, fmt.Errorf("prekey %d: %w", id, err)
	}
	return &keys.PreKey{KeyPair: *pair, KeyID: id}, nil
}

// newSignedPreKey mirrors keys.CreateSignedPreKey, which draws from crypto/rand
// and so cannot be used by anything that has to replay.
func newSignedPreKey(r io.Reader, identityPair *keys.KeyPair, id uint32) (*keys.PreKey, error) {
	pre, err := newPreKey(r, id)
	if err != nil {
		return nil, err
	}
	pre.Signature = identityPair.Sign(&pre.KeyPair)
	return pre, nil
}

// takeOneTime hands out one of the peer's one-time prekeys. Running out is not
// worth failing over: reusing the last one still opens a session the client
// accepts.
func (p *peer) takeOneTime() *keys.PreKey {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.oneTime) == 0 {
		return nil
	}
	if p.consumed >= len(p.oneTime) {
		return p.oneTime[len(p.oneTime)-1]
	}
	key := p.oneTime[p.consumed]
	p.consumed++
	return key
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

// decryptDM opens one <enc> the client addressed to this device. from is the
// address the client is known by, which is the same one we encrypt to, so both
// directions share a session.
func (p *peer) decryptDM(ctx context.Context, from types.JID, isPreKey bool, content []byte) ([]byte, error) {
	addr := from.SignalAddress()

	p.mu.Lock()
	defer p.mu.Unlock()

	builder := signalSession.NewBuilderFromSignal(p.signal, addr, pbSerializer)
	cipher := signalSession.NewCipher(builder, addr)
	var plaintext []byte
	var err error
	if isPreKey {
		var msg *protocol.PreKeySignalMessage
		msg, err = protocol.NewPreKeySignalMessageFromBytes(content, pbSerializer.PreKeySignalMessage, pbSerializer.SignalMessage)
		if err != nil {
			return nil, fmt.Errorf("parse prekey message: %w", err)
		}
		plaintext, err = cipher.DecryptMessage(ctx, msg)
	} else {
		var msg *protocol.SignalMessage
		msg, err = protocol.NewSignalMessageFromBytes(content, pbSerializer.SignalMessage)
		if err != nil {
			return nil, fmt.Errorf("parse message: %w", err)
		}
		plaintext, err = cipher.Decrypt(ctx, msg)
	}
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	// The client has a session with us now, so our own first message to it no
	// longer has to be a pkmsg.
	p.started = true
	return unpadMessage(plaintext)
}

// processSKDM takes the sender key the client distributed for a group, which is
// what makes its skmsg readable.
func (p *peer) processSKDM(ctx context.Context, chat, from types.JID, raw []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	name := protocol.NewSenderKeyName(chat.String(), from.SignalAddress())
	msg, err := protocol.NewSenderKeyDistributionMessageFromBytes(raw, pbSerializer.SenderKeyDistributionMessage)
	if err != nil {
		return fmt.Errorf("parse sender key distribution: %w", err)
	}
	return groups.NewGroupSessionBuilder(p.signal, pbSerializer).Process(ctx, name, msg)
}

// decryptGroup opens the skmsg body of a group message the client sent.
func (p *peer) decryptGroup(ctx context.Context, chat, from types.JID, content []byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	name := protocol.NewSenderKeyName(chat.String(), from.SignalAddress())
	builder := groups.NewGroupSessionBuilder(p.signal, pbSerializer)
	msg, err := protocol.NewSenderKeyMessageFromBytes(content, pbSerializer.SenderKeyMessage)
	if err != nil {
		return nil, fmt.Errorf("parse group message: %w", err)
	}
	plaintext, err := groups.NewGroupCipher(builder, name, p.signal).Decrypt(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("decrypt group message: %w", err)
	}
	return unpadMessage(plaintext)
}

// unpadMessage is the other half of padMessage, unexported in whatsmeow for the
// same reason.
func unpadMessage(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("plaintext is empty")
	}
	pad := int(plaintext[len(plaintext)-1])
	if pad == 0 || pad > len(plaintext) {
		return nil, fmt.Errorf("plaintext has invalid padding length %d", pad)
	}
	if !bytes.HasSuffix(plaintext, bytes.Repeat(plaintext[len(plaintext)-1:], pad)) {
		return nil, errors.New("plaintext has invalid padding")
	}
	return plaintext[:len(plaintext)-pad], nil
}

// preKeyBundleNode is the <user> element fetchPreKeys reads: the device's
// registration id, its identity key, one one-time prekey and the signed one.
func (p *peer) preKeyBundleNode() waBinary.Node {
	var registration [4]byte
	binary.BigEndian.PutUint32(registration[:], p.signal.GetLocalRegistrationID())
	content := []waBinary.Node{
		{Tag: "registration", Content: registration[:]},
		{Tag: "type", Content: []byte{ecc.DjbType}},
		{Tag: "identity", Content: p.identity.Pub[:]},
	}
	if oneTime := p.takeOneTime(); oneTime != nil {
		content = append(content, preKeyNode("key", oneTime))
	}
	content = append(content, preKeyNode("skey", p.signed))
	return waBinary.Node{
		Tag:     "user",
		Attrs:   waBinary.Attrs{"jid": p.jid},
		Content: content,
	}
}

// preKeyNode mirrors whatsmeow's preKeyToNode: a three-byte big-endian id and a
// raw public value, with a signature when the key is a signed one.
func preKeyNode(tag string, key *keys.PreKey) waBinary.Node {
	var id [4]byte
	binary.BigEndian.PutUint32(id[:], key.KeyID)
	node := waBinary.Node{
		Tag: tag,
		Content: []waBinary.Node{
			{Tag: "id", Content: id[1:]},
			{Tag: "value", Content: key.Pub[:]},
		},
	}
	if key.Signature != nil {
		node.Content = append(node.GetChildren(), waBinary.Node{Tag: "signature", Content: key.Signature[:]})
	}
	return node
}

// peerForRecipient resolves a jid the client addressed to the mock device that
// answers for it. The client may write either the phone number or the LID, and
// for the account's own device it always writes the LID, so both spellings have
// to land on the same peer.
func (s *Server) peerForRecipient(jid types.JID) (*peer, error) {
	if jid.IsEmpty() {
		return nil, fmt.Errorf("empty recipient")
	}
	wire := jid
	if wire.Server == types.HiddenUserServer {
		wire.Server = types.DefaultUserServer
	}
	world := s.world
	if world == nil {
		return nil, fmt.Errorf("no world")
	}
	world.mu.Lock()
	_, known := world.contacts[wire.ToNonAD().String()]
	world.mu.Unlock()
	if !known {
		return nil, fmt.Errorf("%s is not in this world", jid)
	}
	return s.peerFor(wire, s.encryptionJID(wire))
}
