package protocol

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"whatevrd/internal/store"
)

// A poll and a live-location share that arrived before this device existed must
// work like any other, end to end.
//
// Both are cards whose usefulness lives in a side table rather than in the
// message row, and backfill used to skip filling those tables. The poll
// rendered as a dead card that rejected every vote (votes name their option by
// hash, and there were no options to hash against) and the share never reached
// the live-locations view, so the banner that says someone is sharing their
// location simply never appeared for anything older than the login.
//
// This goes through the protocol views rather than the store, because that is
// where the frontend actually reads them from.
func TestABackfilledPollAndLiveShareWorkThroughTheViews(t *testing.T) {
	ctx := context.Background()
	socketPath, _, db := startChatsTestServer(t)
	chat := "backfill@g.us"
	self := "me@s.whatsapp.net"
	base := time.Unix(1_700_000_000, 0)

	// --- a poll from before this device existed
	pollID := "backfill@g.us:poll-1"
	pollPayload, err := json.Marshal(map[string]any{
		"poll": map[string]any{"question": "lunch?", "selectable_count": 1},
	})
	if err != nil {
		t.Fatalf("encode poll payload: %v", err)
	}
	if _, err := db.SaveMediaMessage(ctx, store.MediaMessageInput{
		TextMessageInput: store.TextMessageInput{
			ID:          pollID,
			ChatID:      chat,
			ChatName:    "Backfilled",
			SenderID:    "someone@s.whatsapp.net",
			Timestamp:   base,
			Direction:   store.DirectionIncoming,
			PayloadJSON: string(pollPayload),
		},
		MediaKind:      store.MediaKindPoll,
		PayloadSummary: "lunch?",
	}); err != nil {
		t.Fatalf("save backfilled poll: %v", err)
	}
	options := make([]store.PollOption, 0, 2)
	for i, name := range []string{"pizza", "noodles"} {
		sum := sha256.Sum256([]byte(name))
		options = append(options, store.PollOption{Index: i, Name: name, SHA256: sum[:]})
	}
	if err := db.SavePollOptions(ctx, pollID, options); err != nil {
		t.Fatalf("register backfilled poll options: %v", err)
	}

	// --- a live share from before this device existed, still inside its window
	shareID := "backfill@g.us:share-1"
	startedAt := time.Now().Add(-10 * time.Minute)
	payload, err2 := json.Marshal(map[string]any{
		"location": map[string]any{"latitude": 12.97, "longitude": 77.59},
	})
	if err2 != nil {
		t.Fatalf("encode location payload: %v", err2)
	}
	if _, err := db.SaveMediaMessage(ctx, store.MediaMessageInput{
		TextMessageInput: store.TextMessageInput{
			ID:          shareID,
			ChatID:      chat,
			SenderID:    "someone@s.whatsapp.net",
			Timestamp:   startedAt,
			Direction:   store.DirectionIncoming,
			PayloadJSON: string(payload),
		},
		MediaKind:      store.MediaKindLiveLocation,
		PayloadSummary: "Live location",
	}); err != nil {
		t.Fatalf("save backfilled live share: %v", err)
	}
	if err := db.OpenLiveLocationShare(ctx, store.LiveLocationShare{
		MessageID: shareID,
		ChatID:    chat,
		SenderID:  "someone@s.whatsapp.net",
		StartedAt: startedAt.Unix(),
		ExpiresAt: startedAt.Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("open backfilled live share: %v", err)
	}

	c := dialTest(t, socketPath)
	c.hello()

	// --- opening the chat shows the poll with its options
	sub := c.subscribe(2, fmt.Sprintf(`{"view":"messages","chat_id":%q}`, chat))
	pollItem := map[string]any(nil)
	for range 2 {
		msg := c.recvEvent()
		if msg["event"] != "upsert" || msg["sub"] != sub {
			t.Fatalf("expected upsert, got %v", msg)
		}
		item := msg["item"].(map[string]any)
		if item["id"] == pollID {
			pollItem = item
		}
	}
	c.expectReady(sub, true)

	if pollItem == nil {
		t.Fatal("the backfilled poll never reached the transcript")
	}
	poll, ok := pollItem["poll"].(map[string]any)
	if !ok {
		t.Fatalf("the backfilled poll carries no poll block: %v", pollItem)
	}
	pollOptions, ok := poll["options"].([]any)
	if !ok || len(pollOptions) != 2 {
		t.Fatalf("a backfilled poll offered %v options, want 2; nothing can be voted for",
			poll["options"])
	}

	// --- voting on it lands, and the tally moves in the view
	moved, err := db.ApplyPollVote(ctx, pollID, self, [][]byte{options[0].SHA256}, time.Now().Unix())
	if err != nil {
		t.Fatalf("vote on the backfilled poll: %v", err)
	}
	if !moved {
		t.Fatal("the vote was accepted but changed nothing, which is the dead-card symptom")
	}

	voted := c.subscribe(3, fmt.Sprintf(`{"view":"messages","chat_id":%q}`, chat))
	tallied := false
	for range 2 {
		item := c.recvEvent()["item"].(map[string]any)
		if item["id"] != pollID {
			continue
		}
		block := item["poll"].(map[string]any)
		if total, _ := block["total_voters"].(float64); total != 1 {
			t.Fatalf("after one vote the tally reads %v voters, want 1", block["total_voters"])
		}
		first := block["options"].([]any)[0].(map[string]any)
		voters, _ := first["voters"].([]any)
		if len(voters) != 1 {
			t.Fatalf("the voted option names %d voter(s), want 1: %v", len(voters), first)
		}
		tallied = true
	}
	c.expectReady(voted, true)
	if !tallied {
		t.Fatal("the poll did not come back after the vote")
	}

	// --- and the backfilled share is in the live-locations view
	shares := c.subscribe(4, fmt.Sprintf(`{"view":"live_locations","chat_id":%q}`, chat))
	share := c.recvEvent()
	if share["event"] != "upsert" || share["sub"] != shares {
		t.Fatalf("the backfilled live share never reached the banner: %v", share)
	}
	if got := share["item"].(map[string]any)["id"]; got != shareID {
		t.Fatalf("live share id = %v, want %v", got, shareID)
	}
	c.expectReady(shares, true)
}
