package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"whatevrd/internal/store"
)

type memFolders struct {
	folders []store.ChatFolder
}

func (m *memFolders) ListChatFolders(context.Context) ([]store.ChatFolder, error) {
	return m.folders, nil
}

type failingFolders struct{ err error }

func (f failingFolders) ListChatFolders(context.Context) ([]store.ChatFolder, error) {
	return nil, f.err
}

// A client always sends `view` (and may send `limit`) in subscribe params, so
// a view that rejects them can never be opened at all.
func TestFoldersViewOpensWithClientParams(t *testing.T) {
	v := &foldersView{lister: &memFolders{folders: []store.ChatFolder{{ID: 3, Name: "Work"}}}}
	for _, raw := range []string{`{}`, `{"view":"chat_folders"}`, `{"view":"chat_folders","limit":10}`} {
		s, meta, err := v.Open(json.RawMessage(raw), func() {})
		if err != nil {
			t.Fatalf("Open(%s): %v", raw, err)
		}
		if meta != nil {
			t.Fatalf("Open(%s): unexpected meta %#v", raw, meta)
		}
		items := s.Items(0)
		if len(items) != 1 || items[0].ID != "3" {
			t.Fatalf("Items(%s) = %#v, want one row with id 3", raw, items)
		}
		s.Close()
	}
}

func TestFoldersViewReportsListFailures(t *testing.T) {
	want := errors.New("boom")
	v := &foldersView{lister: failingFolders{err: want}}
	s, _, err := v.Open(json.RawMessage(`{"view":"chat_folders"}`), func() {})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	fallible, ok := s.(FallibleSession)
	if !ok {
		t.Fatal("folderSession must report read failures through FallibleSession")
	}
	if items, ierr := fallible.ItemsErr(0); items != nil || ierr != want {
		t.Fatalf("ItemsErr = (%v, %v), want (nil, %v)", items, ierr, want)
	}
}
