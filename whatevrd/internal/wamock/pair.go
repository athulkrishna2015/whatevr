//go:build whatevr_mock

package wamock

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/libsignal/ecc"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// qrRefreshInterval is how long each ref is offered before the next one. The
// real server hands whatsmeow a batch of refs up front and the client rotates
// through them, so a scenario that wants to watch the QR change just has to
// wait.
const qrRefreshInterval = 20 * time.Second

// pairedDevice is what a completed pairing produced, so a scenario can report
// which account it ended up as.
type pairedDevice struct {
	JID types.JID
	LID types.JID
}

// startPairing drives the QR half of login. The client turns each ref into a
// QR containing its own noise key, identity key and adv secret; the mock plays
// the phone that scans it, which is why it waits for the daemon to publish the
// code rather than inventing one.
func (s *session) startPairing(ctx context.Context) error {
	refs := make([]waBinary.Node, 0, 5)
	for i := 0; i < 5; i++ {
		refs = append(refs, waBinary.Node{
			Tag: "ref",
			// No commas: the client pastes this ref into a comma separated QR
			// string, so one here would shift every field after it.
			Content: []byte(fmt.Sprintf("mockref%02d%s", i, base64.RawURLEncoding.EncodeToString(s.srv.rng.bytes(16)))),
		})
	}
	err := s.sendNode(ctx, waBinary.Node{
		Tag: "iq",
		Attrs: waBinary.Attrs{
			"id":    s.srv.rng.stanzaID(),
			"type":  "set",
			"from":  types.ServerJID,
			"xmlns": "md",
		},
		Content: []waBinary.Node{{
			Tag:     "pair-device",
			Content: refs,
		}},
	})
	if err != nil {
		return fmt.Errorf("offer pair-device: %w", err)
	}
	go s.awaitScan(ctx)
	return nil
}

// awaitScan blocks until the daemon publishes a QR, then completes the pairing
// after the scenario's delay. Reading the code from the daemon's login events
// is how the mock learns the adv secret: it only ever exists in the QR.
func (s *session) awaitScan(ctx context.Context) {
	code, err := s.srv.waitForQR(ctx)
	if err != nil {
		s.srv.log.Printf("pairing: %v", err)
		return
	}
	select {
	case <-time.After(s.srv.opts.ScanDelay):
	case <-ctx.Done():
		return
	}
	if err := s.completePairing(ctx, code); err != nil {
		s.srv.log.Printf("pairing: %v", err)
	}
}

// qrParts are the five comma-separated fields of the linked-devices URL that
// whatsmeow's makeQRData builds.
type qrParts struct {
	ref      string
	noise    [32]byte
	identity [32]byte
	adv      []byte
}

func parseQR(code string) (*qrParts, error) {
	_, fragment, ok := strings.Cut(code, "#")
	if !ok {
		return nil, fmt.Errorf("qr %q has no fragment", code)
	}
	fields := strings.Split(fragment, ",")
	if len(fields) < 4 {
		return nil, fmt.Errorf("qr has %d fields, want at least 4", len(fields))
	}
	noise, err := decodeKey(fields[1])
	if err != nil {
		return nil, fmt.Errorf("qr noise key: %w", err)
	}
	identity, err := decodeKey(fields[2])
	if err != nil {
		return nil, fmt.Errorf("qr identity key: %w", err)
	}
	adv, err := base64.StdEncoding.DecodeString(fields[3])
	if err != nil {
		return nil, fmt.Errorf("qr adv secret: %w", err)
	}
	return &qrParts{ref: fields[0], noise: noise, identity: identity, adv: adv}, nil
}

func decodeKey(field string) ([32]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(field)
	if err != nil {
		return [32]byte{}, err
	}
	if len(raw) != 32 {
		return [32]byte{}, fmt.Errorf("got %d bytes, want 32", len(raw))
	}
	return *(*[32]byte)(raw), nil
}

// completePairing signs the device identity as the primary device would and
// sends the pair-success the client verifies against the adv secret it put in
// the QR.
func (s *session) completePairing(ctx context.Context, code string) error {
	qr, err := parseQR(code)
	if err != nil {
		return err
	}

	s.jid = s.srv.accountJID()
	s.jid.Device = 1
	s.lid = lidFor(s.jid)

	details, err := proto.Marshal(&waAdv.ADVDeviceIdentity{
		RawID:     proto.Uint32(1),
		Timestamp: proto.Uint64(uint64(time.Now().Unix())),
		KeyIndex:  proto.Uint32(1),
	})
	if err != nil {
		return fmt.Errorf("marshal device identity: %w", err)
	}

	// The client checks this against the account key we publish alongside it,
	// over prefix || details || its own identity key.
	message := append(append(append([]byte{}, whatsmeow.AdvAccountSignaturePrefix...), details...), qr.identity[:]...)
	accountSig := ecc.CalculateSignature(ecc.NewDjbECPrivateKey(*s.srv.account.Priv), message)

	signed, err := proto.Marshal(&waAdv.ADVSignedDeviceIdentity{
		Details:             details,
		AccountSignatureKey: s.srv.account.Pub[:],
		AccountSignature:    accountSig[:],
	})
	if err != nil {
		return fmt.Errorf("marshal signed identity: %w", err)
	}

	mac := hmac.New(sha256.New, qr.adv)
	mac.Write(signed)
	container, err := proto.Marshal(&waAdv.ADVSignedDeviceIdentityHMAC{
		Details: signed,
		HMAC:    mac.Sum(nil),
	})
	if err != nil {
		return fmt.Errorf("marshal identity container: %w", err)
	}

	id := s.srv.rng.stanzaID()
	s.pairRequestID = id
	return s.sendNode(ctx, waBinary.Node{
		Tag: "iq",
		Attrs: waBinary.Attrs{
			"id":    id,
			"type":  "set",
			"from":  types.ServerJID,
			"xmlns": "md",
			"t":     fmt.Sprintf("%d", time.Now().Unix()),
		},
		Content: []waBinary.Node{{
			Tag: "pair-success",
			Content: []waBinary.Node{
				{Tag: "device", Attrs: waBinary.Attrs{"jid": s.jid, "lid": s.lid}},
				{Tag: "platform", Attrs: waBinary.Attrs{"name": "android"}},
				{Tag: "biz", Attrs: waBinary.Attrs{"name": s.srv.opts.AccountName}},
				{Tag: "device-identity", Content: container},
			},
		}},
	})
}

// handleIQResponse catches the client's answer to pair-success. whatsmeow
// expects the connection to drop after it, then reconnects as a logged-in
// device, which is exactly what a real pairing does.
func (s *session) handleIQResponse(ctx context.Context, node *waBinary.Node) error {
	id := node.AttrGetter().OptionalString("id")
	if s.pairRequestID == "" || id != s.pairRequestID {
		return nil
	}
	s.pairRequestID = ""
	if node.AttrGetter().OptionalString("type") == "error" {
		return fmt.Errorf("client rejected pair-success: %s", node.String())
	}
	s.srv.notePaired(pairedDevice{JID: s.jid, LID: s.lid})
	s.srv.log.Printf("paired %s", s.jid)

	// A freshly paired device has to start a new stream, and the real server
	// says so with stream:error 515 rather than by hanging up. whatsmeow has a
	// dedicated branch for that code which reconnects itself; closing the
	// socket instead leaves the daemon parked forever waiting for a signal that
	// never comes.
	go func() {
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return
		}
		if err := s.sendNode(ctx, waBinary.Node{
			Tag:   "stream:error",
			Attrs: waBinary.Attrs{"code": "515"},
		}); err != nil {
			s.srv.log.Printf("send 515: %v", err)
		}
	}()
	return nil
}

var errNoQR = errors.New("daemon never published a qr code")
