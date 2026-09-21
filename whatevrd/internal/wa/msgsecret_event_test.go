package wa

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"golang.org/x/crypto/hkdf"
	"google.golang.org/protobuf/proto"
)

// The event-response derivation is ours to maintain (see msgsecret_event.go),
// so it needs the tests upstream's own copy would have had.
//
// The failure mode being guarded against is silent: get the join order, the
// jid form or the additional data wrong and nothing panics, nothing logs, the
// key is simply a different key. Every RSVP then fails to authenticate and
// every RSVP we send is unreadable to everyone else.

// The use-case string is the one thing here taken from whatsmeow rather than
// written out, so that an upstream rename breaks the build instead of quietly
// changing every key we derive. This pins what the constant is *worth*, which
// a rename alone would not catch: WhatsApp's wire format is what it is, and if
// upstream ever changed this text the two clients would stop understanding
// each other with no error to explain it.
func TestEventResponseUseCaseStringIsWhatTheWireExpects(t *testing.T) {
	if got := string(whatsmeow.EncSecretEventResponse); got != "Event Response" {
		t.Fatalf("whatsmeow.EncSecretEventResponse = %q, want %q; if upstream really "+
			"changed this, every RSVP we send becomes unreadable", got, "Event Response")
	}
}

// A known vector, derived independently. The expected key is computed here
// straight from the documented scheme with x/crypto/hkdf rather than through
// the same helper under test, so a transposed join or a missing ToNonAD shows
// up as a mismatch instead of agreeing with itself.
func TestEventResponseSecretKeyMatchesTheDocumentedScheme(t *testing.T) {
	secret, err := hex.DecodeString(
		"0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	if err != nil {
		t.Fatal(err)
	}
	const origMsgID = "3EB0ABCDEF"
	// Deliberately carrying device parts: the scheme uses the non-AD form of
	// both jids, and dropping the ToNonAD is the easiest mistake to make here
	// because it works fine until somebody answers from a second device.
	origSender := types.JID{User: "911111", Server: types.DefaultUserServer, Device: 3}
	responder := types.JID{User: "922222", Server: types.DefaultUserServer, Device: 7}

	key, additionalData := eventResponseSecretKey(responder, origMsgID, origSender, secret)

	var info bytes.Buffer
	info.WriteString(origMsgID)
	info.WriteString("911111@s.whatsapp.net")
	info.WriteString("922222@s.whatsapp.net")
	info.WriteString("Event Response")

	want := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, nil, info.Bytes()), want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, want) {
		t.Fatalf("key = %x, want %x", key, want)
	}

	// origMsgID, one NUL, then the responder. Not the original sender: the
	// additional data names who is answering, and swapping the two authenticates
	// against the wrong party.
	wantData := []byte(origMsgID + "\x00" + "922222@s.whatsapp.net")
	if !bytes.Equal(additionalData, wantData) {
		t.Fatalf("additional data = %q, want %q", additionalData, wantData)
	}
}

// A key that depends on the responder is what makes two people's answers to the
// same event different ciphertexts. If it did not, one member's key would open
// another's RSVP.
func TestEventResponseKeyIsPerResponder(t *testing.T) {
	secret := bytes.Repeat([]byte{0xAB}, 32)
	origSender := types.JID{User: "911111", Server: types.DefaultUserServer}
	ana := types.JID{User: "922222", Server: types.DefaultUserServer}
	bo := types.JID{User: "933333", Server: types.DefaultUserServer}

	anaKey, anaData := eventResponseSecretKey(ana, "3EB0", origSender, secret)
	boKey, _ := eventResponseSecretKey(bo, "3EB0", origSender, secret)
	if bytes.Equal(anaKey, boKey) {
		t.Fatal("two responders derived the same key")
	}

	// And the derived key really does round trip through the cipher the scheme
	// names, with the additional data bound in.
	iv := bytes.Repeat([]byte{0x11}, 12)
	ciphertext, err := gcmutil.Encrypt(anaKey, iv, []byte("going"), anaData)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plaintext, err := gcmutil.Decrypt(anaKey, iv, ciphertext, anaData)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(plaintext) != "going" {
		t.Fatalf("round trip = %q", plaintext)
	}
	// Bo cannot open Ana's answer.
	if _, err := gcmutil.Decrypt(boKey, iv, ciphertext, anaData); err == nil {
		t.Fatal("another member's key opened this response")
	}
}

// Who created the event decides the key, and the answer comes from three
// different places depending on the chat and who sent what.
func TestEventOrigSenderFromKeyPicksTheEventsAuthor(t *testing.T) {
	group := types.JID{User: "120363", Server: types.GroupServer}
	dm := types.JID{User: "911111", Server: types.DefaultUserServer}
	responder := types.JID{User: "922222", Server: types.DefaultUserServer}

	// Our own event: the response and the event share an author.
	own, err := eventOrigSenderFromKey(
		&events.Message{Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: responder},
		}},
		&waCommon.MessageKey{FromMe: proto.Bool(true)},
	)
	if err != nil || own != responder {
		t.Fatalf("fromMe sender = %s, %v", own, err)
	}

	// In a group the author is the participant on the key.
	got, err := eventOrigSenderFromKey(
		&events.Message{Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: responder},
		}},
		&waCommon.MessageKey{Participant: proto.String("911111@s.whatsapp.net")},
	)
	if err != nil || got.User != "911111" {
		t.Fatalf("group sender = %s, %v", got, err)
	}

	// In a one-to-one chat there is no participant, only the remote jid.
	got, err = eventOrigSenderFromKey(
		&events.Message{Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: dm, Sender: responder},
		}},
		&waCommon.MessageKey{RemoteJID: proto.String("911111@s.whatsapp.net")},
	)
	if err != nil || got.User != "911111" {
		t.Fatalf("dm sender = %s, %v", got, err)
	}

	// A group key naming a group as the author is nonsense and must not be
	// used to derive anything.
	if _, err := eventOrigSenderFromKey(
		&events.Message{Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: responder},
		}},
		&waCommon.MessageKey{Participant: proto.String("120363@g.us")},
	); err == nil {
		t.Fatal("a group was accepted as the event's author")
	}
}
