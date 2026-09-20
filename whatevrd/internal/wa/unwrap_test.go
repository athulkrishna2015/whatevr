package wa

import (
	"testing"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// whatsmeow's UnwrapRaw peels only some wrappers. The rest arrive still
// wrapped, match no builder, and vanish without a row or a log line. A poll
// inside a V4 wrapper is the clearest case: it is a poll, and it has to come
// out as one.
//
// associatedChildMessage used to be in this list and is deliberately not any
// more: it is a companion of a message that arrived separately rather than a
// message that arrived wrapped, so peeling it drew every HD photo twice. See
// TestTheHDHalfOfAPhotoIsNotASecondMessage.
func TestUnwrapNestedMessageReachesTheRealMessage(t *testing.T) {
	secret := []byte("message-secret")

	cases := []struct {
		name string
		msg  *waE2E.Message
	}{
		{"group mentioned", &waE2E.Message{
			GroupMentionedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{Conversation: proto.String("hi")}},
		}},
		{"spoiler", &waE2E.Message{
			SpoilerMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{Conversation: proto.String("hi")}},
		}},
		{"nested twice", &waE2E.Message{
			GroupMentionedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
				SpoilerMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{Conversation: proto.String("hi")}},
			}},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.msg.MessageContextInfo = &waE2E.MessageContextInfo{MessageSecret: secret}
			got := unwrapNestedMessage(tc.msg)
			if got.GetConversation() != "hi" {
				t.Fatalf("unwrap did not reach the text, got %+v", got)
			}
			if string(got.GetMessageContextInfo().GetMessageSecret()) != string(secret) {
				t.Fatal("the secret did not travel inward with the message")
			}
		})
	}
}

// A V4 poll is a FutureProofMessage, so reading it as a poll directly is a type
// error. Unwrapped, it is an ordinary poll that pollCreationFromMessage finds.
func TestUnwrapNestedMessageExposesV4Poll(t *testing.T) {
	msg := &waE2E.Message{
		PollCreationMessageV4: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			PollCreationMessage: &waE2E.PollCreationMessage{
				Name:    proto.String("lunch?"),
				Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("pizza")}},
			},
		}},
	}

	if pollCreationFromMessage(msg) != nil {
		t.Fatal("a wrapped V4 poll was read as a poll before unwrapping")
	}
	poll := pollCreationFromMessage(unwrapNestedMessage(msg))
	if poll == nil || poll.GetName() != "lunch?" {
		t.Fatal("a V4 poll is still invisible after unwrapping")
	}
}

// A payload nothing renders must still leave an honest row behind. Silence is
// the failure mode that makes history look like it is missing.
func TestUnsupportedMessageLabelCoversUserVisibleKinds(t *testing.T) {
	cases := map[string]*waE2E.Message{
		"Scheduled call":   {ScheduledCallCreationMessage: &waE2E.ScheduledCallCreationMessage{}},
		"Event invite":     {EventInviteMessage: &waE2E.EventInviteMessage{}},
		"Music":            {MusicMessage: &waE2E.MusicMessage{}},
		"Comment":          {CommentMessage: &waE2E.CommentMessage{}},
		"Contacts":         {ContactsArrayMessage: &waE2E.ContactsArrayMessage{}},
		"Payment reminder": {PaymentReminderMessage: &waE2E.PaymentReminderMessage{}},
	}
	for want, msg := range cases {
		label, ok := unsupportedMessageLabel(&events.Message{Message: msg})
		if !ok {
			t.Fatalf("%q produced no row at all", want)
		}
		if label != want {
			t.Fatalf("label = %q, want %q", label, want)
		}
	}

	// Protocol noise stays invisible: a vote is a change to a poll, not a row.
	if _, ok := unsupportedMessageLabel(&events.Message{Message: &waE2E.Message{
		PollUpdateMessage: &waE2E.PollUpdateMessage{},
	}}); ok {
		t.Fatal("a poll vote became a visible row")
	}
}
