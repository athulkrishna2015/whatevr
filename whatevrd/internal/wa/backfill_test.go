package wa

import (
	"context"
	"path/filepath"
	"testing"

	waHistorySync "go.mau.fi/whatsmeow/proto/waHistorySync"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// A chunk that says nothing about a chat is not evidence that the phone has
// nothing older. Exhaustion is a one-way latch, so an omitted chat must stay
// in flight for its expiry timer rather than being resolved by silence, even
// when it is the only request outstanding.
func TestResolveBackfillRequestsIgnoresOmittedChats(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}

	const omitted = "111@s.whatsapp.net"
	const answered = "222@s.whatsapp.net"

	client.backfillInFlight = map[string]*backfillRequest{
		omitted: {requested: 50},
	}
	client.resolveBackfillRequests(ctx, map[string]int{}, nil)

	if _, still := client.backfillInFlight[omitted]; !still {
		t.Fatal("a chunk omitting the only in-flight chat resolved it anyway")
	}
	if chat, err := db.GetChat(ctx, omitted); err == nil && chat.HistoryExhausted {
		t.Fatal("silence marked the chat exhausted")
	}

	// A chat the chunk does mention still resolves, and a short answer still
	// means the phone has nothing older.
	client.backfillInFlight[answered] = &backfillRequest{requested: 50}
	client.resolveBackfillRequests(ctx, map[string]int{answered: 3}, nil)

	if _, still := client.backfillInFlight[answered]; still {
		t.Fatal("a mentioned chat was left in flight")
	}
	if _, still := client.backfillInFlight[omitted]; !still {
		t.Fatal("the omitted chat was resolved by a chunk about another chat")
	}
}

// WhatsApp states outright whether more history remains. Reading that answer is
// the only way to be right; counting how many messages happened to arrive is a
// guess, and it must not overrule the phone.
func TestHistoryExhaustedFromConversation(t *testing.T) {
	cases := []struct {
		name          string
		kind          waHistorySync.Conversation_EndOfHistoryTransferType
		wantExhausted bool
	}{
		{"more remain", waHistorySync.Conversation_COMPLETE_BUT_MORE_MESSAGES_REMAIN_ON_PRIMARY, false},
		{"none remain", waHistorySync.Conversation_COMPLETE_AND_NO_MORE_MESSAGE_REMAIN_ON_PRIMARY, true},
		{"on demand, more remain", waHistorySync.Conversation_COMPLETE_ON_DEMAND_SYNC_BUT_MORE_MSG_REMAIN_ON_PRIMARY, false},
		{"on demand, no access", waHistorySync.Conversation_COMPLETE_ON_DEMAND_SYNC_WITH_MORE_MSG_ON_PRIMARY_BUT_NO_ACCESS, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exhausted, known := historyExhaustedFromConversation(&waHistorySync.Conversation{
				EndOfHistoryTransferType: tc.kind.Enum(),
			})
			if !known {
				t.Fatal("a stated answer was read as unknown")
			}
			if exhausted != tc.wantExhausted {
				t.Fatalf("exhausted = %t, want %t", exhausted, tc.wantExhausted)
			}
		})
	}

	// Absent means the phone said nothing, so the flag must be left alone
	// rather than guessed at.
	if _, known := historyExhaustedFromConversation(&waHistorySync.Conversation{}); known {
		t.Fatal("an absent field was read as an answer")
	}
}
