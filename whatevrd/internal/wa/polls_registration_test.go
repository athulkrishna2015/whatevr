package wa

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// A poll's options are what its votes name themselves by, so a poll that was
// stored without them rejects every vote forever. Backfill used to skip the
// registration entirely, which made every poll older than this device's login a
// dead card. Registration must not depend on which path stored the row.
func TestRegisterSavedMessageRecordsPollOptionsFromBackfill(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}

	waMsg := &waE2E.Message{
		PollCreationMessage: &waE2E.PollCreationMessage{
			Name: proto.String("lunch?"),
			Options: []*waE2E.PollCreationMessage_Option{
				{OptionName: proto.String("pizza")},
				{OptionName: proto.String("noodles")},
			},
			SelectableOptionsCount: proto.Uint32(1),
		},
	}
	saved, err := db.SaveMediaMessage(ctx, appstore.MediaMessageInput{
		TextMessageInput: appstore.TextMessageInput{
			ID:        "chat-1:poll-1",
			ChatID:    "chat-1",
			ChatName:  "Test Chat",
			SenderID:  "sender-1",
			Timestamp: time.Unix(1700000000, 0),
		},
		MediaKind:      appstore.MediaKindPoll,
		PayloadSummary: "lunch?",
	})
	if err != nil {
		t.Fatalf("save poll message: %v", err)
	}
	message := saved.Message

	// live=false is the backfill case: the registration must still happen.
	client.registerSavedMessage(ctx, message, waMsg, time.Unix(1700000000, 0), false)

	options, err := db.PollOptionHashes(ctx, message.ID)
	if err != nil {
		t.Fatalf("read poll options: %v", err)
	}
	if len(options) != 2 {
		t.Fatalf("a backfilled poll stored %d options, want 2", len(options))
	}
	for i, want := range []string{"pizza", "noodles"} {
		if options[i].Name != want {
			t.Fatalf("option %d = %q, want %q", i, options[i].Name, want)
		}
		if len(options[i].SHA256) == 0 {
			t.Fatalf("option %d has no hash, so no vote can ever match it", i)
		}
	}
}
