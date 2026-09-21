package protocol

import (
	"testing"

	"whatevrd/internal/store"
)

// The tiebreak used to be the row's insert order, which made a backfilled
// message sort after a live one from the same second (exactly backwards, since
// backfill is always the later writer) and gave two devices holding the same
// conversation two different orders. The id is the same everywhere and does not
// change when a row is written again.
func TestMessageSortIsDeterministicAndDeviceIndependent(t *testing.T) {
	older := store.Message{ID: "chat:b", SortMS: 1_700_000_000_000}
	newer := store.Message{ID: "chat:a", SortMS: 1_700_000_000_500}

	if messageSort(older) >= messageSort(newer) {
		t.Fatal("a message half a second later did not sort after")
	}

	// Same instant, so the id decides, and it decides the same way every time
	// and on every device.
	first := store.Message{ID: "chat:aaa", SortMS: 1_700_000_000_000}
	second := store.Message{ID: "chat:bbb", SortMS: 1_700_000_000_000}
	if messageSort(first) >= messageSort(second) {
		t.Fatal("a tie was not broken by the message id")
	}

	// Re-ingesting a message must not move it.
	if messageSort(first) != messageSort(store.Message{ID: "chat:aaa", SortMS: 1_700_000_000_000}) {
		t.Fatal("the same message produced two different sort keys")
	}
}
