//go:build whatevr_mock

package wamock

import (
	"context"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waWa6"
	"go.mau.fi/whatsmeow/socket"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

const (
	frameMaxSize    = 1 << 24
	frameLengthSize = 3
	handshakeWait   = 20 * time.Second
)

// session is one client connection: the Noise handshake, then an encrypted
// stream of binary XMPP nodes in both directions.
type session struct {
	srv  *Server
	conn *websocket.Conn

	// pending holds bytes read from the websocket that do not yet make a whole
	// frame. WhatsApp frames and websocket messages are not one to one.
	pending []byte

	writeKey     cipher.AEAD
	readKey      cipher.AEAD
	writeCounter uint32
	readCounter  uint32
	writeMu      sync.Mutex

	// headerSeen tracks the four-byte WA header the client prepends to its very
	// first frame and never repeats.
	headerSeen bool

	payload *waWa6.ClientPayload
	jid     types.JID
	lid     types.JID

	// pairRequestID is the id of the outstanding pair-success, so the client's
	// result can be told apart from any other iq it answers. It is written by
	// the pairing goroutine and read by the read loop, hence the lock.
	pairMu        sync.Mutex
	pairRequestID string

	// outbox serialises everything the server originates after login, so
	// stanzas reach the client in the order the scenario produced them.
	outbox chan func(context.Context) error
	// done closes when the connection is gone, so a scenario goroutine parked
	// on a full outbox is released rather than leaked.
	done chan struct{}

	closeOnce sync.Once
}

func newSession(srv *Server, conn *websocket.Conn) *session {
	return &session{
		srv:    srv,
		conn:   conn,
		outbox: make(chan func(context.Context) error, outboxDepth),
		done:   make(chan struct{}),
	}
}

func (s *session) close(code websocket.StatusCode, reason string) {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.conn.Close(code, reason)
	})
}

func (s *session) run(ctx context.Context) {
	defer s.close(websocket.StatusNormalClosure, "")

	hsCtx, cancel := context.WithTimeout(ctx, handshakeWait)
	err := s.handshake(hsCtx)
	cancel()
	if err != nil {
		s.srv.log.Printf("handshake failed: %v", err)
		return
	}

	if err := s.onConnected(ctx); err != nil {
		s.srv.log.Printf("post-connect: %v", err)
		return
	}

	for {
		node, err := s.readNode(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
				s.srv.log.Printf("read: %v", err)
			}
			return
		}
		if err := s.handleNode(ctx, node); err != nil {
			s.srv.log.Printf("handle <%s>: %v", node.Tag, err)
		}
	}
}

// handshake runs the responder half of Noise_XX. Every step mirrors
// whatsmeow's doHandshake in the same order; see noise.go.
func (s *session) handshake(ctx context.Context) error {
	first, err := s.readFrame(ctx)
	if err != nil {
		return fmt.Errorf("read client hello: %w", err)
	}
	var hello waWa6.HandshakeMessage
	if err := proto.Unmarshal(first, &hello); err != nil {
		return fmt.Errorf("parse client hello: %w", err)
	}
	clientEphemeral := hello.GetClientHello().GetEphemeral()
	if len(clientEphemeral) != 32 {
		return fmt.Errorf("client hello ephemeral is %d bytes, want 32", len(clientEphemeral))
	}
	clientEphemeralArr := *(*[32]byte)(clientEphemeral)

	ns := newNoiseState(socket.NoiseStartPattern, socket.WAConnHeader)
	ns.authenticate(clientEphemeral)

	ephemeral, err := genKeyPair(s.srv.rng)
	if err != nil {
		return fmt.Errorf("server ephemeral: %w", err)
	}
	ns.authenticate(ephemeral.Pub[:])
	if err := ns.mixSharedSecret(*ephemeral.Priv, clientEphemeralArr); err != nil {
		return fmt.Errorf("mix ephemeral: %w", err)
	}

	encryptedStatic := ns.encrypt(s.srv.ident.static.Pub[:])
	if err := ns.mixSharedSecret(*s.srv.ident.static.Priv, clientEphemeralArr); err != nil {
		return fmt.Errorf("mix static: %w", err)
	}
	encryptedCert := ns.encrypt(s.srv.ident.chain)

	serverHello, err := proto.Marshal(&waWa6.HandshakeMessage{
		ServerHello: &waWa6.HandshakeMessage_ServerHello{
			Ephemeral: ephemeral.Pub[:],
			Static:    encryptedStatic,
			Payload:   encryptedCert,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal server hello: %w", err)
	}
	if err := s.writeFrame(ctx, serverHello); err != nil {
		return fmt.Errorf("send server hello: %w", err)
	}

	second, err := s.readFrame(ctx)
	if err != nil {
		return fmt.Errorf("read client finish: %w", err)
	}
	var finishMsg waWa6.HandshakeMessage
	if err := proto.Unmarshal(second, &finishMsg); err != nil {
		return fmt.Errorf("parse client finish: %w", err)
	}
	finish := finishMsg.GetClientFinish()
	if finish == nil {
		return errors.New("client finish missing")
	}
	clientStatic, err := ns.decrypt(finish.GetStatic())
	if err != nil {
		return fmt.Errorf("decrypt client static: %w", err)
	}
	if len(clientStatic) != 32 {
		return fmt.Errorf("client static is %d bytes, want 32", len(clientStatic))
	}
	if err := ns.mixSharedSecret(*ephemeral.Priv, *(*[32]byte)(clientStatic)); err != nil {
		return fmt.Errorf("mix client static: %w", err)
	}
	payloadBytes, err := ns.decrypt(finish.GetPayload())
	if err != nil {
		return fmt.Errorf("decrypt client payload: %w", err)
	}
	var payload waWa6.ClientPayload
	if err := proto.Unmarshal(payloadBytes, &payload); err != nil {
		return fmt.Errorf("parse client payload: %w", err)
	}
	s.payload = &payload

	if s.writeKey, s.readKey, err = ns.finish(); err != nil {
		return fmt.Errorf("derive transport keys: %w", err)
	}
	return nil
}

// readFrame returns one WhatsApp frame, stripping the one-off WA header and
// reassembling across websocket message boundaries.
func (s *session) readFrame(ctx context.Context) ([]byte, error) {
	for {
		if !s.headerSeen && len(s.pending) >= len(socket.WAConnHeader) {
			s.pending = s.pending[len(socket.WAConnHeader):]
			s.headerSeen = true
		}
		if s.headerSeen && len(s.pending) >= frameLengthSize {
			length := int(s.pending[0])<<16 | int(s.pending[1])<<8 | int(s.pending[2])
			if length >= frameMaxSize {
				return nil, fmt.Errorf("frame of %d bytes is too large", length)
			}
			if len(s.pending) >= frameLengthSize+length {
				frame := make([]byte, length)
				copy(frame, s.pending[frameLengthSize:frameLengthSize+length])
				s.pending = s.pending[frameLengthSize+length:]
				return frame, nil
			}
		}
		typ, data, err := s.conn.Read(ctx)
		if err != nil {
			return nil, err
		}
		if typ != websocket.MessageBinary {
			return nil, fmt.Errorf("unexpected websocket message type %v", typ)
		}
		s.pending = append(s.pending, data...)
	}
}

func (s *session) writeFrame(ctx context.Context, data []byte) error {
	if len(data) >= frameMaxSize {
		return fmt.Errorf("frame of %d bytes is too large", len(data))
	}
	frame := make([]byte, frameLengthSize+len(data))
	frame[0] = byte(len(data) >> 16)
	frame[1] = byte(len(data) >> 8)
	frame[2] = byte(len(data))
	copy(frame[frameLengthSize:], data)
	return s.conn.Write(ctx, websocket.MessageBinary, frame)
}

// readNode reads one decrypted, unmarshalled stanza.
func (s *session) readNode(ctx context.Context) (*waBinary.Node, error) {
	frame, err := s.readFrame(ctx)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, 12)
	binary.BigEndian.PutUint32(iv[8:], s.readCounter)
	s.readCounter++
	plaintext, err := s.readKey.Open(nil, iv, frame, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt frame: %w", err)
	}
	// Marshal emits a leading flag byte (zero, or bit 1 for zlib) that
	// Unmarshal does not expect back. Unpack is the half that strips it, so a
	// frame has to go through both.
	unpacked, err := waBinary.Unpack(plaintext)
	if err != nil {
		return nil, fmt.Errorf("unpack frame: %w", err)
	}
	node, err := waBinary.Unmarshal(unpacked)
	if err != nil {
		return nil, fmt.Errorf("unmarshal node: %w", err)
	}
	return node, nil
}

func (s *session) sendNode(ctx context.Context, node waBinary.Node) error {
	plaintext, err := waBinary.Marshal(node)
	if err != nil {
		return fmt.Errorf("marshal <%s>: %w", node.Tag, err)
	}
	// The counter and the write have to stay together: two senders that
	// interleave would put frames on the wire in a different order than they
	// numbered them, and the client's AEAD counter would desync.
	// plaintext already carries Marshal's flag byte, which is exactly what the
	// client's Unpack wants to strip. Nothing to add here.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	iv := make([]byte, 12)
	binary.BigEndian.PutUint32(iv[8:], s.writeCounter)
	s.writeCounter++
	ciphertext := s.writeKey.Seal(nil, iv, plaintext, nil)
	return s.writeFrame(ctx, ciphertext)
}

func (s *session) setPairRequest(id string) {
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	s.pairRequestID = id
}

// takePairRequest reports whether id is the outstanding pair-success and clears
// it if so, so the answer is only ever acted on once.
func (s *session) takePairRequest(id string) bool {
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	if s.pairRequestID == "" || id != s.pairRequestID {
		return false
	}
	s.pairRequestID = ""
	return true
}
