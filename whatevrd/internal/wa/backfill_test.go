package wa

import (
	"context"
	"path/filepath"
	"testing"

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
	client.resolveBackfillRequests(ctx, map[string]int{})

	if _, still := client.backfillInFlight[omitted]; !still {
		t.Fatal("a chunk omitting the only in-flight chat resolved it anyway")
	}
	if chat, err := db.GetChat(ctx, omitted); err == nil && chat.HistoryExhausted {
		t.Fatal("silence marked the chat exhausted")
	}

	// A chat the chunk does mention still resolves, and a short answer still
	// means the phone has nothing older.
	client.backfillInFlight[answered] = &backfillRequest{requested: 50}
	client.resolveBackfillRequests(ctx, map[string]int{answered: 3})

	if _, still := client.backfillInFlight[answered]; still {
		t.Fatal("a mentioned chat was left in flight")
	}
	if _, still := client.backfillInFlight[omitted]; !still {
		t.Fatal("the omitted chat was resolved by a chunk about another chat")
	}
}
