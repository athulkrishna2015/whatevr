package store

import (
	"encoding/json"
	"strings"
)

// The kind-specific payloads a message row can carry in its payload_json
// column. They live here, in the store, because the store writes them and the
// protocol layer reads them back out; the json tags are the wire shape and
// PROTOCOL.md is their contract.
//
// Anything static enough to be written once at ingest belongs here. State that
// keeps moving after the message lands (a poll's tally, a live share's trail,
// an event's RSVPs) gets a real table instead, because it has to be queried and
// updated rather than replaced wholesale.

// MessagePayload is the envelope stored in payload_json. Exactly one field is
// set, chosen by the row's media_kind, so decoding never has to guess.
type MessagePayload struct {
	Location  *LocationPayload  `json:"location,omitempty"`
	LiveShare *LiveSharePayload `json:"live,omitempty"`
	Contacts  *ContactsPayload  `json:"contacts,omitempty"`
	Poll      *PollPayload      `json:"poll,omitempty"`
}

// PollPayload is a poll's fixed settings. The tally is not here: it changes on
// every vote and is joined from poll_options/poll_votes at read time, so a
// snapshot can never go stale.
type PollPayload struct {
	Question string `json:"question,omitempty"`
	// SelectableCount is how many options a voter may choose. 1 is the usual
	// radio-button poll; 0 means WhatsApp did not say, which in practice also
	// means one.
	SelectableCount int  `json:"selectable_count,omitempty"`
	AllowAddOption  bool `json:"allow_add_option,omitempty"`
	// EndsAt is when the poll closes, 0 for a poll that never does.
	EndsAt int64 `json:"ends_at,omitempty"`
	Quiz   bool  `json:"quiz,omitempty"`
}

// ContactsPayload is one or more shared contact cards. WhatsApp sends a single
// ContactMessage and a multi-card ContactsArrayMessage; both land here, so a
// renderer has one shape to deal with and the count tells it which it is.
type ContactsPayload struct {
	// DisplayName is the array's own label, set only by ContactsArrayMessage.
	DisplayName string        `json:"display_name,omitempty"`
	Cards       []ContactCard `json:"cards"`
}

// ContactCard is one person, parsed out of their vCard.
type ContactCard struct {
	DisplayName string         `json:"display_name,omitempty"`
	Org         string         `json:"org,omitempty"`
	Title       string         `json:"title,omitempty"`
	Birthday    string         `json:"birthday,omitempty"`
	Phones      []ContactField `json:"phones,omitempty"`
	Emails      []ContactField `json:"emails,omitempty"`
	URLs        []ContactField `json:"urls,omitempty"`
	Addresses   []ContactField `json:"addresses,omitempty"`
	// VCard is the sender's original text, kept verbatim so exporting the card
	// hands on exactly what arrived rather than a lossy re-serialization.
	VCard string `json:"vcard,omitempty"`
}

// ContactField is one labelled value on a card.
type ContactField struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value"`
	// JID is set when the vCard carried a `waid=` parameter, which is WhatsApp
	// stating that this number is on WhatsApp and what its jid is. It means a
	// "Message" action needs no lookup, and in particular no usync round trip
	// telling the server which contacts somebody forwarded us.
	JID string `json:"jid,omitempty"`
}

// LiveSharePayload is the state of a live-location share, kept on the message
// that opened it. It lives in the row's payload rather than being joined from
// live_location_shares so that listing a conversation stays one query: the
// state only changes on the same writes that rewrite the payload anyway.
type LiveSharePayload struct {
	// Active is false once the share has ended, whether by expiry, by the
	// sender stopping it, or by going quiet with no stop message. The bubble
	// settles from a live pin into a summary of where the share went.
	Active    bool  `json:"active"`
	StartedAt int64 `json:"started_at,omitempty"`
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// UpdatedAt is when the last position landed, so a bubble can say "updated
	// 8s ago" instead of implying a pin is current when it is minutes old.
	UpdatedAt int64 `json:"updated_at,omitempty"`
	// SpeedMPS and HeadingDegrees come from the newest position, when the
	// sender reported them at all.
	SpeedMPS       float64 `json:"speed_mps,omitempty"`
	HeadingDegrees int32   `json:"heading_deg,omitempty"`
	// PointCount is how long the trail is, which is what makes a finished share
	// worth looking at rather than just a stale pin.
	PointCount int `json:"point_count,omitempty"`
}

// LocationPayload is a shared place: a plain LocationMessage, the opening
// message of a live share, or the venue attached to an event.
type LocationPayload struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
	URL       string  `json:"url,omitempty"`
	// AccuracyMeters is the sender's reported precision, 0 when unknown. A
	// large value is worth showing: "within 500 m" is a different claim from a
	// pin on a doorway.
	AccuracyMeters uint32 `json:"accuracy_m,omitempty"`
	// Live marks the opening message of a live share. The moving state itself
	// arrives through the `live` object, not here.
	Live bool `json:"live,omitempty"`
}

// EncodePayload serializes a payload for the payload_json column. An empty
// payload encodes as the empty string rather than "{}", so the common case (a
// message with no structured payload at all) costs nothing on disk.
func EncodePayload(payload MessagePayload) (string, error) {
	if payload.isZero() {
		return "", nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// DecodePayload reads a payload_json column back. A row written by an older
// build, or by a newer one carrying a payload this build has never heard of,
// decodes into whatever fields it does recognise and drops the rest, which is
// the same forward-compatibility rule the wire follows.
func DecodePayload(raw string) MessagePayload {
	if strings.TrimSpace(raw) == "" {
		return MessagePayload{}
	}
	var payload MessagePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		// A payload that will not parse is a daemon bug, not a reason to lose
		// the message: the row still has its kind, its text and its summary.
		return MessagePayload{}
	}
	return payload
}

// isZero reports an envelope with nothing in it. Written out rather than
// compared with == so adding a pointer field here never silently changes what
// counts as empty.
func (p MessagePayload) isZero() bool {
	return p.Location == nil && p.LiveShare == nil && p.Contacts == nil && p.Poll == nil
}
