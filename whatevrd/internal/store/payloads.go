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
	Location *LocationPayload `json:"location,omitempty"`
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
	if payload == (MessagePayload{}) {
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
