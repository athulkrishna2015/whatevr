package wa

import (
	"context"
	"testing"

	waSyncAction "go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
)

func TestHandleArchiveEventFollowsPhone(t *testing.T) {
	ctx := context.Background()
	client, db := newMarkReadTestClient(t)
	jid := types.NewJID("12345", types.DefaultUserServer)
	chatID := jid.String()
	if _, err := db.EnsureChat(ctx, chatID, "Test Chat", false); err != nil {
		t.Fatalf("ensure chat: %v", err)
	}
	if _, _, err := db.UpdateChatArchiveState(ctx, chatID, true); err != nil {
		t.Fatalf("archive chat: %v", err)
	}

	client.handleArchiveEvent(ctx, &events.Archive{
		JID:    jid,
		Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(false)},
	})

	chat, err := db.GetChat(ctx, chatID)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if chat.IsArchived {
		t.Fatalf("chat stayed archived without the preference: %+v", chat)
	}
}

func TestHandleArchiveEventKeepChatsArchived(t *testing.T) {
	ctx := context.Background()
	client, db := newMarkReadTestClient(t)
	client.appPrefs.Store(&app.AppPreferences{KeepChatsArchived: true})

	jid := types.NewJID("12345", types.DefaultUserServer)
	chatID := jid.String()
	if _, err := db.EnsureChat(ctx, chatID, "Test Chat", false); err != nil {
		t.Fatalf("ensure chat: %v", err)
	}
	if _, _, err := db.UpdateChatArchiveState(ctx, chatID, true); err != nil {
		t.Fatalf("archive chat: %v", err)
	}

	client.handleArchiveEvent(ctx, &events.Archive{
		JID:    jid,
		Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(false)},
	})

	chat, err := db.GetChat(ctx, chatID)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if !chat.IsArchived {
		t.Fatalf("keep-archived preference was not honoured: %+v", chat)
	}

	// Archives still apply with the preference on: it holds chats, it does not
	// freeze them.
	other := types.NewJID("67890", types.DefaultUserServer)
	if _, err := db.EnsureChat(ctx, other.String(), "Other", false); err != nil {
		t.Fatalf("ensure other chat: %v", err)
	}
	client.handleArchiveEvent(ctx, &events.Archive{
		JID:    other,
		Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)},
	})

	otherChat, err := db.GetChat(ctx, other.String())
	if err != nil {
		t.Fatalf("get other chat: %v", err)
	}
	if !otherChat.IsArchived {
		t.Fatalf("archive event dropped by the preference: %+v", otherChat)
	}
}
