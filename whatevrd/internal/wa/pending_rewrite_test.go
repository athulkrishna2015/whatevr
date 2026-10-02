package wa

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	waCommon "go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// An edit or a revoke names a message its sender assumes we have. On a fresh
// pairing we do not: it arrives live within seconds of connecting and the
// message it is about is somewhere in a history sync that takes minutes.
// Dropping it leaves the original words standing for good, because nothing ever
// says it twice.
func TestARevokeForAMessageThatHasNotArrivedIsAppliedWhenItDoes(t *testing.T) {
	ctx, db, client := rewriteFixture(t)
	chat := types.NewJID("917770000001", types.DefaultUserServer)
	internalID := internalMessageIDForChat(chat.String(), "MSG1")

	if !client.handleRevokeMessage(ctx, revokeEvent(chat, "MSG1"), false) {
		t.Fatal("the revoke was not recognised as one")
	}
	if parked := client.parkedRewrites(); parked != 1 {
		t.Fatalf("parked rewrites = %d, want the one with nothing to land on", parked)
	}

	// A pass before the message exists keeps it parked rather than dropping it.
	client.reconcilePendingRewrites(ctx, false)
	if parked := client.parkedRewrites(); parked != 1 {
		t.Fatalf("parked rewrites after an early pass = %d, want 1", parked)
	}

	saveMessage(t, ctx, db, internalID, chat, "the words somebody took back")
	client.reconcilePendingRewrites(ctx, false)

	message, err := db.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if !message.IsRevoked {
		t.Fatal("the message arrived and the revoke that was waiting for it was not applied")
	}
	if parked := client.parkedRewrites(); parked != 0 {
		t.Fatalf("parked rewrites after the message landed = %d, want none", parked)
	}
}

func TestAnEditForAMessageThatHasNotArrivedIsAppliedWhenItDoes(t *testing.T) {
	ctx, db, client := rewriteFixture(t)
	chat := types.NewJID("917770000002", types.DefaultUserServer)
	internalID := internalMessageIDForChat(chat.String(), "MSG2")

	if !client.handleEditMessage(ctx, editEvent(chat, "MSG2", "the second wording"), false) {
		t.Fatal("the edit was not recognised as one")
	}
	saveMessage(t, ctx, db, internalID, chat, "the first wording")
	client.reconcilePendingRewrites(ctx, false)

	message, err := db.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if message.Text != "the second wording" {
		t.Fatalf("the message reads %q, want the edit that was waiting for it", message.Text)
	}
	if !message.IsEdited {
		t.Fatal("the message is not marked edited")
	}
}

// Both can be waiting for the same message, and a deletion is the last thing
// that happens to one: an edit behind it is an edit of something that is not
// there any more.
func TestARevokeOutranksAnEditParkedForTheSameMessage(t *testing.T) {
	ctx, db, client := rewriteFixture(t)
	chat := types.NewJID("917770000003", types.DefaultUserServer)
	internalID := internalMessageIDForChat(chat.String(), "MSG3")

	client.handleEditMessage(ctx, editEvent(chat, "MSG3", "an edit nobody will read"), false)
	client.handleRevokeMessage(ctx, revokeEvent(chat, "MSG3"), false)
	client.handleEditMessage(ctx, editEvent(chat, "MSG3", "and one that arrived after the deletion"), false)

	saveMessage(t, ctx, db, internalID, chat, "the original")
	client.reconcilePendingRewrites(ctx, false)

	message, err := db.GetMessage(ctx, internalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if !message.IsRevoked {
		t.Fatalf("the message reads %q, want the deletion to have won", message.Text)
	}
}

// A rewrite for a message older than the history this device was given has
// nothing to land on, ever. It goes when the sync is over rather than being
// retried for the life of the process.
func TestARewriteForAMessageThatNeverArrivesIsDroppedAtTheEndOfTheSync(t *testing.T) {
	ctx, _, client := rewriteFixture(t)
	chat := types.NewJID("917770000004", types.DefaultUserServer)

	client.handleRevokeMessage(ctx, revokeEvent(chat, "GONE"), false)
	client.reconcilePendingRewrites(ctx, true)
	if parked := client.parkedRewrites(); parked != 0 {
		t.Fatalf("parked rewrites after the final pass = %d, want none", parked)
	}
}

func rewriteFixture(t *testing.T) (context.Context, *appstore.DB, *Client) {
	t.Helper()
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return ctx, db, &Client{store: db, daemon: app.NewDaemon(app.Paths{})}
}

func (c *Client) parkedRewrites() int {
	c.pendingRewritesMu.Lock()
	defer c.pendingRewritesMu.Unlock()
	return len(c.pendingRewrites)
}

func revokeEvent(chat types.JID, target string) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			ID:            "REVOKE-" + target,
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Key:  &waCommon.MessageKey{ID: proto.String(target)},
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		}},
	}
}

func editEvent(chat types.JID, target, text string) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			ID:            "EDIT-" + target,
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Key:           &waCommon.MessageKey{ID: proto.String(target)},
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			EditedMessage: &waE2E.Message{Conversation: proto.String(text)},
		}},
	}
}

func saveMessage(t *testing.T, ctx context.Context, db *appstore.DB, internalID string, chat types.JID, text string) {
	t.Helper()
	if _, err := db.SaveMessages(ctx, []appstore.MessageSaveItem{{Text: &appstore.TextMessageInput{
		ID:        internalID,
		ChatID:    chat.String(),
		ChatName:  "Asha",
		SenderID:  chat.String(),
		Text:      text,
		Timestamp: time.Unix(1758000000, 0),
		Direction: appstore.DirectionIncoming,
		Status:    appstore.StatusDelivered,
	}}}); err != nil {
		t.Fatalf("save message: %v", err)
	}
}
