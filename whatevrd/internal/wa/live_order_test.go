package wa

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// Two messages sent in the same second must stay in the order they arrived.
//
// WhatsApp states the time in whole seconds, so both carried the same sort key
// and the tiebreak underneath was the message id, which is random hex. Seen in
// a real chat: a reply drawn above the message it answered, both stamped 13:36.
func TestTwoLiveMessagesInOneSecondKeepTheirArrivalOrder(t *testing.T) {
	client := &Client{}
	stamped := time.Now().Truncate(time.Second).Add(-time.Minute)
	opts := ingestOptions{source: sourceLive}
	// The ids are the real ones from the chat this was found in. Sorted by id,
	// which is what the tiebreak used to do, they come out the wrong way round.
	arrived := func(id string) time.Time {
		return client.messageTimestamp(types.MessageInfo{ID: id, Timestamp: stamped}, opts, nil)
	}

	first := arrived("ACD46B430CBE3B71B3426BA3DB44E4B6")
	second := arrived("AC259C54F518EFF66279BA20E4FCCE0E")

	if !first.Before(second) {
		t.Fatalf("arrival order was lost: first=%s second=%s", first, second)
	}
	if first.Unix() != stamped.Unix() || second.Unix() != stamped.Unix() {
		t.Fatalf("the displayed second moved: %d and %d, want %d",
			first.Unix(), second.Unix(), stamped.Unix())
	}

	// Ingesting the same message again must not renumber it. A retry or a
	// re-decrypt runs this path a second time for the same id.
	if again := arrived("ACD46B430CBE3B71B3426BA3DB44E4B6"); !again.Equal(first) {
		t.Fatalf("a second ingest of the same message moved it: %s, want %s", again, first)
	}
}

// History sync has no arrival order to borrow: the chunk is a batch, and the
// order inside it is whatever the phone wrote. It keeps the plain second so
// two devices holding the same conversation still agree.
func TestHistorySyncKeepsThePlainSecond(t *testing.T) {
	client := &Client{}
	opts := ingestOptions{source: sourceHistorySync, timestampOverride: time.Unix(1_700_000_000, 0)}

	for _, id := range []string{"A", "B", "C"} {
		got := client.messageTimestamp(types.MessageInfo{ID: id}, opts, nil)
		if got.Nanosecond() != 0 {
			t.Fatalf("history sync message %s was given a sub-second position: %s", id, got)
		}
	}
}
