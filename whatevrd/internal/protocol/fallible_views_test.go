package protocol

import (
	"context"
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
	_ FallibleSession = (*chatMediaSession)(nil)
	_ FallibleSession = (*starredSession)(nil)
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
