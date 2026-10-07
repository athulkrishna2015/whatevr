//go:build whatevr_mock

package wamock

import (
	"context"
	"slices"
	"sort"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestHeldHistoryWaitsToBeAskedFor(t *testing.T) {
	srv, err := New(context.Background(), Options{Seed: 1, Scenario: "history"})
	if err != nil {
		t.Fatal(err)
	}
	w := srv.world
	chat, ok := w.chatByJID(types.NewJID("917770000011", types.DefaultUserServer))
	if !ok {
		t.Fatal("no chat with Ira")
	}
	var all []*Msg
	for _, m := range w.history {
		if m.Chat == chat {
			all = append(all, m)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })

	ids := func(ms []*Msg) []string {
		out := make([]string, len(ms))
		for i, m := range ms {
			out[i] = m.ID
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		before string
		want   []*Msg
	}{
		{"the oldest synced", all[200].ID, all[150:200]},
		{"part way back", all[30].ID, all[0:30]},
		{"the very first", all[0].ID, nil},
		{"one it never sent", "nope", nil},
	} {
		got := w.olderThan(chat, tc.before, 50)
		if !slices.Equal(ids(got), ids(tc.want)) {
			t.Errorf("%s: got %d messages, want %d", tc.name, len(got), len(tc.want))
		}
	}

	var synced int
	for _, conv := range w.historyChunks() {
		for _, c := range conv.GetConversations() {
			if c.GetID() == chat.JID.String() {
				synced += len(c.GetMessages())
			}
		}
	}
	if synced != 60 {
		t.Fatalf("the first sync carried %d of Ira's messages, want 60", synced)
	}
}
