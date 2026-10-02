package wa

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// App state lands seconds after connecting and history sync takes minutes, so
// on a fresh pairing every star in the account arrives before the message it
// marks. Dropping those, which an unknown message id used to mean, loses every
// star the account has: app state is a snapshot taken at connect and the star
// is never sent again.
func TestAStarForAMessageThatHasNotArrivedIsAppliedWhenItDoes(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{})}
	chat := types.NewJID("917770000001", types.DefaultUserServer)
	internalID := internalMessageIDForChat(chat.String(), "MSG1")

	client.handleStarEvent(ctx, &events.Star{
		ChatJID:   chat,
		MessageID: "MSG1",
		Action:    &waSyncAction.StarAction{Starred: proto.Bool(true)},
	})
	client.pendingStarsMu.Lock()
	parked := len(client.pendingStars)
	client.pendingStarsMu.Unlock()
	if parked != 1 {
		t.Fatalf("parked stars = %d, want the one with nothing to land on", parked)
	}

	// A pass before the message exists keeps it parked rather than dropping it.
	client.reconcilePendingAppState(ctx, false)
	client.pendingStarsMu.Lock()
	parked = len(client.pendingStars)
	client.pendingStarsMu.Unlock()
	if parked != 1 {
		t.Fatalf("parked stars after an early pass = %d, want 1", parked)
	}

	// History sync brings the message, and the next pass applies the star.
	if _, err := db.SaveMessages(ctx, []appstore.MessageSaveItem{{Text: &appstore.TextMessageInput{
		ID:        internalID,
		ChatID:    chat.String(),
		ChatName:  "Asha",
		SenderID:  chat.String(),
		Text:      "starred on the phone",
		Timestamp: time.Unix(1758000000, 0),
		Direction: appstore.DirectionIncoming,
		Status:    appstore.StatusDelivered,
	}}}); err != nil {
		t.Fatalf("save message: %v", err)
	}
	client.reconcilePendingAppState(ctx, false)

	message, err := db.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if !message.IsStarred {
		t.Fatal("the message arrived and the star that was waiting for it was not applied")
	}
	client.pendingStarsMu.Lock()
	parked = len(client.pendingStars)
	client.pendingStarsMu.Unlock()
	if parked != 0 {
		t.Fatalf("parked stars after the message landed = %d, want none", parked)
	}
}

// A star for a message older than the history this device was given has
// nothing to land on, ever. It goes when the sync is over rather than being
// retried for the life of the process.
func TestAStarForAMessageThatNeverArrivesIsDroppedAtTheEndOfTheSync(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{})}
	client.handleStarEvent(ctx, &events.Star{
		ChatJID:   types.NewJID("917770000001", types.DefaultUserServer),
		MessageID: "GONE",
		Action:    &waSyncAction.StarAction{Starred: proto.Bool(true)},
	})

	client.reconcilePendingAppState(ctx, true)
	client.pendingStarsMu.Lock()
	parked := len(client.pendingStars)
	client.pendingStarsMu.Unlock()
	if parked != 0 {
		t.Fatalf("parked stars after the final pass = %d, want none", parked)
	}
}
