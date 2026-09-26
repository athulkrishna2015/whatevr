package protocol

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"whatevrd/internal/store"
)

// Every list view reads a store that can fail, and without this the engine
// cannot tell an empty window from an unreadable one: it takes the nil slice at
// face value and removes every row the client holds. One locked database and
// the sidebar, the gallery or the starred page goes blank.
var (
	_ FallibleSession = (*chatsSession)(nil)
	_ FallibleSession = (*chatSession)(nil)
	_ FallibleSession = (*pinnedSession)(nil)
	_ FallibleSession = (*receiptsSession)(nil)
	_ FallibleSession = (*statusSession)(nil)
	_ FallibleSession = (*statusKeptSession)(nil)
	_ FallibleSession = (*statusMutedSession)(nil)
	_ FallibleSession = (*channelsSession)(nil)
	_ FallibleSession = (*channelMessagesSession)(nil)
	_ FallibleSession = (*chatLinksSession)(nil)
	_ FallibleSession = (*folderSession)(nil)
	_ FallibleSession = (*stickerPacksSession)(nil)
	_ FallibleSession = (*stickerPackSession)(nil)
	_ FallibleSession = (*logsSession)(nil)
	_ FallibleSession = (*chatMediaSession)(nil)
	_ FallibleSession = (*starredSession)(nil)
	_ FallibleSession = (*callHistorySession)(nil)
	_ FallibleSession = (*stickersSession)(nil)
	_ FallibleSession = (*liveLocationsSession)(nil)
)

type failingChatLister struct{ err error }

func (f failingChatLister) ListChatsForView(context.Context, store.ChatListFilter) ([]store.Chat, error) {
	return nil, f.err
}

func (f failingChatLister) GetChatForView(context.Context, string) (store.Chat, error) {
	return store.Chat{}, f.err
}

func TestChatsSessionReportsAReadFailureInsteadOfAnEmptyList(t *testing.T) {
	want := errors.New("database is locked")
	session := &chatsSession{lister: failingChatLister{err: want}, ctx: context.Background()}

	items, err := session.ItemsErr(20)
	if !errors.Is(err, want) {
		t.Fatalf("ItemsErr returned %v, want the store error", err)
	}
	if items != nil {
		t.Fatalf("a failed read returned %d items", len(items))
	}

	// The plain call still answers harmlessly for anything that only wants rows.
	if got := session.Items(20); got != nil {
		t.Fatalf("Items returned %d items after a failed read", len(got))
	}
}

func TestChatSessionKeepsADeletedChatEmptyAndReportsReadFailures(t *testing.T) {
	gone := &chatSession{
		chatID: "chat@s.whatsapp.net",
		lister: failingChatLister{err: sql.ErrNoRows},
		ctx:    context.Background(),
	}
	if items, err := gone.ItemsErr(1); err != nil || items != nil {
		t.Fatalf("ItemsErr for a deleted chat = (%v, %v), want (nil, nil)", items, err)
	}

	want := errors.New("database is locked")
	broken := &chatSession{
		chatID: "chat@s.whatsapp.net",
		lister: failingChatLister{err: want},
		ctx:    context.Background(),
	}
	items, err := broken.ItemsErr(1)
	if !errors.Is(err, want) {
		t.Fatalf("ItemsErr returned %v, want the store error", err)
	}
	if items != nil {
		t.Fatalf("a failed read returned %d items", len(items))
	}
}
