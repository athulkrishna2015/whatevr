package whatsapp

import (
	"context"
	"testing"
	"time"
	appstore "whatevrd/internal/store"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

const testGroupJID = "120363000000000001@g.us"

func TestGroupInviteIngestKeepsWhatTheSenderClaimed(t *testing.T) {
	d := newTestDecoder(t)

	expiry := time.Now().Add(48 * time.Hour).Unix()
	input, ok := d.mediaMessageInput(context.Background(), mediaIngestEvent("inv1", &waE2E.Message{
		GroupInviteMessage: &waE2E.GroupInviteMessage{
			GroupJID:         proto.String(testGroupJID),
			InviteCode:       proto.String("  CODE123  "),
			InviteExpiration: proto.Int64(expiry),
			GroupName:        proto.String("  Wow3  "),
			Caption:          proto.String(" join us "),
		},
	}), ingestOptions{source: sourceLive})
	if !ok {
		t.Fatal("a group invite must ingest as its own kind, not a tombstone")
	}
	if input.MediaKind != appstore.MediaKindGroupInvite {
		t.Fatalf("kind = %q, want %q", input.MediaKind, appstore.MediaKindGroupInvite)
	}
	// The caption is the sender's own words and belongs in the body, the same
	// way a photo's caption does.
	if input.Text != " join us " {
		t.Fatalf("text = %q", input.Text)
	}
	if input.PayloadSummary != "Wow3" {
		t.Fatalf("summary = %q, want the group name", input.PayloadSummary)
	}

	payload := appstore.DecodePayload(input.PayloadJSON).GroupInvite
	if payload == nil {
		t.Fatal("no invite payload was stored")
	}
	if payload.GroupJID != testGroupJID {
		t.Fatalf("group jid = %q", payload.GroupJID)
	}
	// Senders pad these, and a padded code is a code that will not resolve.
	if payload.Code != "CODE123" || payload.Name != "Wow3" || payload.Caption != "join us" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.ExpiresAt != expiry {
		t.Fatalf("expires_at = %d, want %d", payload.ExpiresAt, expiry)
	}
	// Nothing has been resolved yet, and the card must be able to tell.
	if payload.ResolvedAt != 0 || payload.Subject != "" || payload.Joined {
		t.Fatalf("an unresolved invite claimed resolved facts: %+v", payload)
	}
}

// A group invite quoted in a reply has to say which group, the same way a
// quoted location says where.
func TestQuotedGroupInvitePreviewNamesTheGroup(t *testing.T) {
	text, kind, mime := quotedReplyPreview(&waE2E.Message{
		GroupInviteMessage: &waE2E.GroupInviteMessage{
			GroupJID:  proto.String(testGroupJID),
			GroupName: proto.String("Wow3"),
		},
	})
	if text != "Wow3" || kind != appstore.MediaKindGroupInvite || mime != "" {
		t.Fatalf("quotedReplyPreview() = %q, %q, %q", text, kind, mime)
	}

	// With no name the jid is the only handle there is, and it beats an empty
	// quote strip.
	text, _, _ = quotedReplyPreview(&waE2E.Message{
		GroupInviteMessage: &waE2E.GroupInviteMessage{GroupJID: proto.String(testGroupJID)},
	})
	if text != testGroupJID {
		t.Fatalf("unnamed invite preview = %q", text)
	}
}
