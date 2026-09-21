package store

import (
	"context"
	"testing"
	"time"
)

func seedEvent(t *testing.T, db *DB, messageID, chatID string) {
	t.Helper()
	payload, err := EncodePayload(MessagePayload{Event: &EventPayload{
		Name:               "Team dinner",
		StartsAt:           1_700_100_000,
		ExtraGuestsAllowed: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveMediaMessage(context.Background(), MediaMessageInput{
		TextMessageInput: TextMessageInput{
			ID:          messageID,
			ChatID:      chatID,
			SenderID:    "ana@s.whatsapp.net",
			Timestamp:   time.Unix(1_700_000_000, 0),
			PayloadJSON: payload,
		},
		MediaKind:      MediaKindEvent,
		PayloadSummary: "Team dinner",
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func eventFor(t *testing.T, db *DB, chatID, messageID string) *EventState {
	t.Helper()
	messages, err := db.ListMessages(context.Background(), chatID, 50, "")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	for _, m := range messages {
		if m.ID == messageID {
			return m.Event
		}
	}
	t.Fatalf("event %s not in the page", messageID)
	return nil
}

// An RSVP is a whole answer, not a delta: changing your mind replaces what you
// said rather than adding to it.
func TestApplyEventResponseReplacesTheAnswer(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEvent(t, db, "chat-1:ev", "chat-1")

	if _, err := db.ApplyEventResponse(ctx, "chat-1:ev", "ana@s.whatsapp.net", EventResponseMaybe, 0, 100); err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if _, err := db.ApplyEventResponse(ctx, "chat-1:ev", "ana@s.whatsapp.net", EventResponseGoing, 2, 200); err != nil {
		t.Fatalf("changed answer: %v", err)
	}

	state := eventFor(t, db, "chat-1", "chat-1:ev")
	if len(state.Responders) != 1 {
		t.Fatalf("responders = %d, want 1", len(state.Responders))
	}
	if state.Responders[0].Response != EventResponseGoing || state.Responders[0].ExtraGuests != 2 {
		t.Fatalf("responder = %+v", state.Responders[0])
	}
	// Heads, not answers: one person bringing two guests is three at the door.
	if got := state.Going(); got != 3 {
		t.Fatalf("going = %d, want 3", got)
	}
}

// WhatsApp redelivers on every reconnect, so an answer that arrived before the
// one currently stored must not overwrite it. This is the same defect that let
// a withdrawn poll vote come back to life.
func TestStaleEventResponsesAreIgnored(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEvent(t, db, "chat-1:ev", "chat-1")

	if _, err := db.ApplyEventResponse(ctx, "chat-1:ev", "ana@s.whatsapp.net", EventResponseGoing, 0, 200); err != nil {
		t.Fatal(err)
	}
	applied, err := db.ApplyEventResponse(ctx, "chat-1:ev", "ana@s.whatsapp.net", EventResponseNotGoing, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("an older answer overwrote a newer one")
	}
	if state := eventFor(t, db, "chat-1", "chat-1:ev"); state.Responders[0].Response != EventResponseGoing {
		t.Fatalf("answer = %q, want the newer one", state.Responders[0].Response)
	}
}

// A redelivered copy of the answer already stored changes nothing, and saying
// so is what stops every reconnect redrawing every open transcript.
func TestRepeatingTheSameAnswerChangesNothing(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEvent(t, db, "chat-1:ev", "chat-1")

	if applied, err := db.ApplyEventResponse(ctx, "chat-1:ev", "ana@s.whatsapp.net", EventResponseGoing, 1, 200); err != nil || !applied {
		t.Fatalf("first answer: applied=%v err=%v", applied, err)
	}
	applied, err := db.ApplyEventResponse(ctx, "chat-1:ev", "ana@s.whatsapp.net", EventResponseGoing, 1, 300)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("the same answer again reported a change")
	}
}

// Our own answer is resolved as the page loads, so a bubble knows which chip is
// lit without searching the responder list for itself.
func TestSelfResponseIsResolvedOnTheEvent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.SetSelfJID(ctx, "me@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	seedEvent(t, db, "chat-1:ev", "chat-1")

	if _, err := db.ApplyEventResponse(ctx, "chat-1:ev", "ana@s.whatsapp.net", EventResponseGoing, 0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApplyEventResponse(ctx, "chat-1:ev", "me@s.whatsapp.net", EventResponseMaybe, 3, 200); err != nil {
		t.Fatal(err)
	}

	state := eventFor(t, db, "chat-1", "chat-1:ev")
	if state.SelfResponse != EventResponseMaybe || state.SelfGuests != 3 {
		t.Fatalf("self = %q/%d", state.SelfResponse, state.SelfGuests)
	}
	// Only the people who said yes count towards the door, and a maybe with
	// three guests is still nobody.
	if got := state.Going(); got != 1 {
		t.Fatalf("going = %d, want 1", got)
	}
}

// Taking an answer back removes it rather than storing an empty one, which is
// what an RSVP whose send failed has to roll back to.
func TestClearingAnAnswerRemovesTheResponder(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEvent(t, db, "chat-1:ev", "chat-1")

	if _, err := db.ApplyEventResponse(ctx, "chat-1:ev", "me@s.whatsapp.net", EventResponseGoing, 0, 100); err != nil {
		t.Fatal(err)
	}
	response, guests, err := db.EventResponseOf(ctx, "chat-1:ev", "me@s.whatsapp.net")
	if err != nil || response != EventResponseGoing || guests != 0 {
		t.Fatalf("stored answer = %q/%d, %v", response, guests, err)
	}
	if err := db.ClearEventResponse(ctx, "chat-1:ev", "me@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if state := eventFor(t, db, "chat-1", "chat-1:ev"); len(state.Responders) != 0 {
		t.Fatalf("responders = %d after clearing, want 0", len(state.Responders))
	}
	// And an event nobody has answered still has a state, so a bubble can tell
	// "nobody answered" from "this build knows nothing about events".
	if eventFor(t, db, "chat-1", "chat-1:ev") == nil {
		t.Fatal("an unanswered event carried no state at all")
	}
}
