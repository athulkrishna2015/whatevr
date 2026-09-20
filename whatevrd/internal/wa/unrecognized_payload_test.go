package wa

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// WhatsApp ships message kinds faster than whatevr learns them, and an allowlist
// can only describe the ones already known. Anything set on a message that is
// not plumbing has to leave a row, even when nothing here can render it.
func TestUnrecognizedPayloadField(t *testing.T) {
	for name, msg := range map[string]*waE2E.Message{
		"a sticker format nobody wrote code for": {LottieStickerMessage: &waE2E.FutureProofMessage{}},
		"a bot task":                             {BotTaskMessage: &waE2E.FutureProofMessage{}},
		"a question":                             {QuestionMessage: &waE2E.FutureProofMessage{}},
		"a sharing-limit change":                 {LimitSharingMessage: &waE2E.FutureProofMessage{}},
	} {
		if _, ok := unrecognizedPayloadField(msg); !ok {
			t.Fatalf("%s was treated as an empty envelope", name)
		}
	}

	// Plumbing stays silent. A message carrying only these said nothing.
	for name, msg := range map[string]*waE2E.Message{
		"a session key":  {SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{}},
		"a poll vote":    {PollUpdateMessage: &waE2E.PollUpdateMessage{}},
		"a reaction":     {ReactionMessage: &waE2E.ReactionMessage{}},
		"a pin":          {PinInChatMessage: &waE2E.PinInChatMessage{}},
		"bare context":   {MessageContextInfo: &waE2E.MessageContextInfo{}},
		"an empty event": {},
	} {
		if field, ok := unrecognizedPayloadField(msg); ok {
			t.Fatalf("%s became a visible row through field %q", name, field)
		}
	}
}

// End to end: an unknown kind lands in the transcript as a tombstone that keeps
// its payload, so the row can be upgraded once the kind is understood.
func TestUnknownMessageKindIsStoredAsATombstone(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}

	evt := &events.Message{
		Info: types.MessageInfo{
			ID:        "unknown-1",
			Timestamp: time.Unix(1700000000, 0),
			MessageSource: types.MessageSource{
				Chat:   types.JID{User: "peer", Server: types.DefaultUserServer},
				Sender: types.JID{User: "peer", Server: types.DefaultUserServer},
			},
		},
		Message: &waE2E.Message{LottieStickerMessage: &waE2E.FutureProofMessage{}},
	}
	if !client.handleMessage(ctx, evt, false) {
		t.Fatal("an unknown payload refused the ack")
	}

	chatID := "peer@" + types.DefaultUserServer
	message, err := db.GetMessage(ctx, internalMessageIDForChat(chatID, "unknown-1"))
	if err != nil {
		t.Fatalf("an unknown payload left no row at all: %v", err)
	}
	if message.MediaKind != appstore.MediaKindUnsupported {
		t.Fatalf("row kind is %q, want %q", message.MediaKind, appstore.MediaKindUnsupported)
	}
	if message.Text == "" {
		t.Fatal("the tombstone carries no label, so the bubble would render blank")
	}
	if len(message.MediaPayload) == 0 {
		t.Fatal("the tombstone dropped its payload, so it can never be upgraded")
	}
}

// An HD photo must draw once, not twice.
//
// WhatsApp sends an HD photo as two stanzas: the ordinary image, and a
// companion carrying the full-size copy inside associatedChildMessage. The
// companion was being peeled like any other wrapper, which made it a message in
// its own right: found in a real chat as the same photo twice, seconds apart,
// at 720x1280 and 2160x3840.
func TestTheHDHalfOfAPhotoIsNotASecondMessage(t *testing.T) {
	companion := &waE2E.Message{
		AssociatedChildMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				ImageMessage: &waE2E.ImageMessage{
					Mimetype: proto.String("image/jpeg"),
					Width:    proto.Uint32(2160),
					Height:   proto.Uint32(3840),
				},
			},
		},
	}

	// Nothing peels it into the image it wraps.
	if unwrapped := unwrapNestedMessage(companion); unwrapped.GetImageMessage() != nil {
		t.Fatal("the HD companion was peeled into an image message, so it becomes a second row")
	}

	// And it leaves no tombstone either: there is already a row for this photo.
	if field, unknown := unrecognizedPayloadField(companion); unknown {
		t.Fatalf("the HD companion would be stored as an unsupported %q row", field)
	}
}
