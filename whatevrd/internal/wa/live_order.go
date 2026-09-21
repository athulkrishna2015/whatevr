package wa

import (
	"time"

	"go.mau.fi/whatsmeow/types"
)

// WhatsApp states a message's time in whole seconds. Two messages sent in the
// same second therefore carry the same sort key, and the tiebreak underneath it
// is the message id, which is random hex: the transcript put them in whatever
// order the ids happened to fall in, and a reply could be drawn above the thing
// it answered.
//
// For a message arriving live there is a real answer. The socket delivered them
// in order, so the position within that second's live arrivals is the order they
// were sent in. History sync carries no such evidence and keeps the plain
// second, where the id tiebreak at least stays the same on every device.
//
// Kept for a handful of seconds only, and keyed by message id, so a re-ingest of
// the same message lands on the number it already had.
const liveOrderSecondsKept = 4

// The key has three digits of room below the second. A second that somehow
// carries more than that many live messages stops advancing rather than
// spilling into the next one.
const liveOrderMaxOrdinal = 999

func (c *Client) liveArrivalOrdinal(second int64, messageID string) int {
	if messageID == "" {
		return 0
	}
	c.liveOrderMu.Lock()
	defer c.liveOrderMu.Unlock()

	if c.liveOrder == nil {
		c.liveOrder = make(map[int64][]string, liveOrderSecondsKept)
	}
	ids := c.liveOrder[second]
	for i, id := range ids {
		if id == messageID {
			return i
		}
	}
	ordinal := len(ids)
	if ordinal > liveOrderMaxOrdinal {
		return liveOrderMaxOrdinal
	}
	c.liveOrder[second] = append(ids, messageID)

	for known := range c.liveOrder {
		if known < second-liveOrderSecondsKept {
			delete(c.liveOrder, known)
		}
	}
	return ordinal
}

// liveOrderedTimestamp keeps the second WhatsApp stated and fills in the
// milliseconds from arrival order, so the displayed time is unchanged and only
// the ordering underneath it gets better.
func (c *Client) liveOrderedTimestamp(info types.MessageInfo, opts ingestOptions, base time.Time) time.Time {
	if opts.source != sourceLive || base.IsZero() || base.Nanosecond() != 0 {
		return base
	}
	ordinal := c.liveArrivalOrdinal(base.Unix(), info.ID)
	if ordinal == 0 {
		return base
	}
	return base.Add(time.Duration(ordinal) * time.Millisecond)
}
