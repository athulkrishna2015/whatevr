package proto

import "encoding/json"

// ViewSink is everything a view model has to implement. The client routes a
// subscription's events here and never looks inside an item, so one sink
// implementation serves every view in the inventory.
//
// Every method is called on the client's read goroutine, one at a time, so an
// implementation needs no locking against the client. It does need locking
// against whoever reads it, which is [view.Collection]'s job.
type ViewSink interface {
	// Upsert inserts or replaces the item with this item.id, positioned by
	// sort. Sort is opaque: order by bytewise comparison, ascending.
	Upsert(sort string, item json.RawMessage)

	// Remove deletes the item with this id.
	Remove(id string)

	// Ready says the window is fully populated for the latest subscribe or
	// extend. hasExhausted tells an omitted `exhausted` from a false one.
	Ready(exhausted bool, hasExhausted bool)

	// Reset discards the local copy. Fresh upserts follow, then Ready.
	Reset()

	// BatchBegin and BatchEnd bracket every run of upserts and removes that
	// arrived in one read. A window fill is one transaction rather than one
	// per item, which is the difference between eighty layout passes and one.
	// Ready and Reset apply eagerly and are not batched.
	BatchBegin()
	BatchEnd()
}
