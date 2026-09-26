package protocol

import (
	"context"
	"fmt"
	"testing"
	"time"

	"whatevrd/internal/app"
	"whatevrd/internal/store"
)

// seedCallLog inserts one call-log row into the chat and returns its id.
func seedCallLog(t *testing.T, db *store.DB, chatID, chatName string, payload store.CallLogPayload, ts time.Time) string {
	t.Helper()
	encoded, err := store.EncodePayload(store.MessagePayload{CallLog: &payload})
	if err != nil {
		t.Fatalf("encode call log payload: %v", err)
	}
	id := fmt.Sprintf("cl-%s-%d", chatID, ts.UnixNano())
	if _, err := db.SaveMediaMessage(context.Background(), store.MediaMessageInput{
		TextMessageInput: store.TextMessageInput{
			ID:          id,
			ChatID:      chatID,
			ChatName:    chatName,
			Timestamp:   ts,
			Direction:   store.DirectionIncoming,
			PayloadJSON: encoded,
		},
		MediaKind:      store.MediaKindCallLog,
		PayloadSummary: "Missed voice call",
	}); err != nil {
		t.Fatalf("seed call log in %s: %v", chatID, err)
	}
	return id
}

// call_history: fill from every chat's call logs newest-first, then a live one.
func TestCallHistoryViewFillAndLiveUpdate(t *testing.T) {
	socketPath, daemon, db := startChatsTestServer(t)
	base := time.Unix(1_700_000_000, 0)
	alice := "alice@s.whatsapp.net"
	bob := "bob@s.whatsapp.net"

	// An ordinary text message must never reach the calls list.
	seedTextMessage(t, db, alice, "not a call", base)
	missed := seedCallLog(t, db, alice, "Alice", store.CallLogPayload{Outcome: store.CallOutcomeMissed}, base.Add(time.Minute))
	answered := seedCallLog(t, db, bob, "Bob", store.CallLogPayload{
		Outcome:      store.CallOutcomeConnected,
		DurationSecs: 42,
		Video:        true,
	}, base.Add(2*time.Minute))

	c := dialTest(t, socketPath)
	c.hello()
	sub := c.subscribe(2, `{"view":"call_history"}`)

	// Newest first across chats: Bob's answered call, then Alice's missed one.
	newest := c.expectUpsert(sub, answered)
	older := c.expectUpsert(sub, missed)
	if newest["sort"].(string) >= older["sort"].(string) {
		t.Fatalf("call_history sort order = %q then %q, want newest first", newest["sort"], older["sort"])
	}
	c.expectReady(sub, true)

	item := newest["item"].(map[string]any)
	if item["kind"] != "call_log" || item["chat_id"] != bob || item["chat_name"] != "Bob" {
		t.Fatalf("call_history row = %v", item)
	}
	if item["direction"] != store.DirectionIncoming {
		t.Fatalf("call_history direction = %v, want %q", item["direction"], store.DirectionIncoming)
	}
	logPayload, ok := item["call_log"].(map[string]any)
	if !ok {
		t.Fatalf("call_history row without a call_log payload: %v", item)
	}
	if logPayload["outcome"] != store.CallOutcomeConnected || logPayload["duration_secs"] != float64(42) || logPayload["video"] != true {
		t.Fatalf("call_log payload = %v", logPayload)
	}

	// A call finishing now upserts the same way the conversation sees it.
	fresh := seedCallLog(t, db, alice, "Alice", store.CallLogPayload{Outcome: store.CallOutcomeMissed}, base.Add(3*time.Minute))
	daemon.PublishNewMessage(app.Message{ID: fresh, ChatID: alice}, app.Chat{ID: alice})
	item = c.expectUpsert(sub, fresh)["item"].(map[string]any)
	if item["kind"] != "call_log" || item["chat_name"] != "Alice" {
		t.Fatalf("live call_history row = %v", item)
	}
}

// call_history: the subscribe window is what caps the list, so a one-row window
// delivers only the newest call and reports older rows still reachable.
func TestCallHistoryViewWindow(t *testing.T) {
	socketPath, _, db := startChatsTestServer(t)
	base := time.Unix(1_700_000_000, 0)
	chat := "c@s.whatsapp.net"
	seedCallLog(t, db, chat, "Alice", store.CallLogPayload{Outcome: store.CallOutcomeMissed}, base)
	newest := seedCallLog(t, db, chat, "Alice", store.CallLogPayload{Outcome: store.CallOutcomeMissed}, base.Add(time.Minute))

	c := dialTest(t, socketPath)
	c.hello()
	sub := c.subscribe(2, `{"view":"call_history","limit":1}`)

	c.expectUpsert(sub, newest)
	c.expectReady(sub, false)
}
