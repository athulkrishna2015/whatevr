package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	appstore "whatevrd/internal/store"
)

const testGroupJID = "120363000000000001@g.us"

func TestGroupInviteIngestKeepsWhatTheSenderClaimed(t *testing.T) {
	client := newMediaIngestClient(t)

	expiry := time.Now().Add(48 * time.Hour).Unix()
	input, ok := client.mediaMessageInput(context.Background(), mediaIngestEvent("inv1", &waE2E.Message{
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

// A card with nothing resolved still has to name its group, or the bubble is a
// button with no subject.
func TestGroupInviteDisplayNamePrefersTheResolvedSubject(t *testing.T) {
	cases := []struct {
		name    string
		payload *appstore.GroupInvitePayload
		want    string
	}{
		{"resolved subject wins", &appstore.GroupInvitePayload{Name: "old name", Subject: "Wow3"}, "Wow3"},
		{"sender's copy when unresolved", &appstore.GroupInvitePayload{Name: "Wow3"}, "Wow3"},
		{"neither", &appstore.GroupInvitePayload{}, ""},
		{"nil payload", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.payload.DisplayName(); got != tc.want {
				t.Fatalf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// An expired code cannot be resolved and cannot be joined, so it must not cost
// a round trip. This is what stops a history sync full of old invites turning
// into a query storm on first run.
func TestGroupInviteExpiryIsWhatBoundsResolution(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name    string
		payload *appstore.GroupInvitePayload
		want    bool
	}{
		{"still open", &appstore.GroupInvitePayload{ExpiresAt: now.Unix() + 60}, false},
		{"lapsed", &appstore.GroupInvitePayload{ExpiresAt: now.Unix() - 1}, true},
		{"exactly now", &appstore.GroupInvitePayload{ExpiresAt: now.Unix()}, true},
		{"no stated expiry never lapses", &appstore.GroupInvitePayload{}, false},
		{"nil payload", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := groupInviteExpired(tc.payload, now); got != tc.want {
				t.Fatalf("groupInviteExpired() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The inviter is who whatsmeow authenticates the code against, so getting it
// wrong makes every lookup and every join fail.
func TestGroupInvitePartiesNameTheGroupAndTheInviter(t *testing.T) {
	client := newMediaIngestClient(t)
	payload := &appstore.GroupInvitePayload{GroupJID: testGroupJID, Code: "CODE123"}

	groupJID, inviter, ok := client.groupInviteParties(appstore.Message{
		SenderID:  "5551234@s.whatsapp.net",
		Direction: appstore.DirectionIncoming,
	}, payload)
	if !ok {
		t.Fatal("an incoming invite from a known sender must resolve its parties")
	}
	if groupJID.String() != testGroupJID {
		t.Fatalf("group = %s", groupJID)
	}
	if inviter.String() != "5551234@s.whatsapp.net" {
		t.Fatalf("inviter = %s", inviter)
	}

	// A jid that is not a group is not an invite we can act on, however well
	// formed the rest of the payload is.
	if _, _, ok := client.groupInviteParties(appstore.Message{
		SenderID: "5551234@s.whatsapp.net",
	}, &appstore.GroupInvitePayload{GroupJID: "5551234@s.whatsapp.net"}); ok {
		t.Error("a one-to-one jid was accepted as a group invite target")
	}
}

func TestGroupInfoIncludesUsMatchesEitherJIDForm(t *testing.T) {
	own := map[string]bool{"5551234@s.whatsapp.net": true}

	// A group that addresses its members by LID still has to recognise us,
	// which is why the phone number is checked alongside the primary jid.
	byPhone := &types.GroupInfo{Participants: []types.GroupParticipant{{
		JID:         types.JID{User: "7777", Server: types.HiddenUserServer},
		PhoneNumber: types.JID{User: "5551234", Server: types.DefaultUserServer},
	}}}
	if !groupInfoIncludesUs(byPhone, own) {
		t.Error("a member listed by LID with our phone number attached was missed")
	}

	stranger := &types.GroupInfo{Participants: []types.GroupParticipant{{
		JID: types.JID{User: "9999999", Server: types.DefaultUserServer},
	}}}
	if groupInfoIncludesUs(stranger, own) {
		t.Error("a group of strangers claimed us as a member")
	}
	if groupInfoIncludesUs(nil, own) || groupInfoIncludesUs(byPhone, nil) {
		t.Error("membership was claimed with nothing to compare")
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
