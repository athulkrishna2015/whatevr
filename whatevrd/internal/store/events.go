package store

// event response values, spelled as they cross the wire
const (
	EventResponseGoing    = "going"
	EventResponseNotGoing = "not_going"
	EventResponseMaybe    = "maybe"
)

// EventResponder is one person's answer.
type EventResponder struct {
	JID             string
	DisplayName     string
	AvatarLocalPath string
	Response        string
	// ExtraGuests is how many people they are bringing, for an event whose
	// author allowed it. It is counted separately from the responders
	// themselves, because a head count and an attendee list are different
	// questions and only one of them has faces.
	ExtraGuests     int
	RespondedAtUnix int64
	FromMe          bool
}

// EventState is everything about an event that is not in its payload.
type EventState struct {
	Responders []EventResponder
	// SelfResponse is our own answer, empty when we have not answered. It is
	// resolved here rather than searched for in Responders, so a bubble can
	// show which chip is ours without walking the list.
	SelfResponse string
	SelfGuests   int
}
