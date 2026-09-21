package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	appstore "whatevrd/internal/store"
)

func TestEventIngestKeepsThePlanAndItsVenue(t *testing.T) {
	client := newMediaIngestClient(t)

	start := time.Now().Add(26 * time.Hour).Unix()
	input, ok := client.mediaMessageInput(context.Background(), mediaIngestEvent("ev1", &waE2E.Message{
		EventMessage: &waE2E.EventMessage{
			Name:               proto.String("  Team dinner  "),
			Description:        proto.String(" bring a friend "),
			StartTime:          proto.Int64(start),
			EndTime:            proto.Int64(start + 7200),
			ExtraGuestsAllowed: proto.Bool(true),
			Location: &waE2E.LocationMessage{
				DegreesLatitude:  proto.Float64(12.9716),
				DegreesLongitude: proto.Float64(77.5946),
				Name:             proto.String("Cafe Noir"),
			},
		},
	}), ingestOptions{source: sourceLive})
	if !ok {
		t.Fatal("an event must ingest as its own kind, not a tombstone")
	}
	if input.MediaKind != appstore.MediaKindEvent {
		t.Fatalf("kind = %q", input.MediaKind)
	}
	if input.PayloadSummary != "Team dinner" {
		t.Fatalf("summary = %q", input.PayloadSummary)
	}

	payload := appstore.DecodePayload(input.PayloadJSON).Event
	if payload == nil {
		t.Fatal("no event payload was stored")
	}
	if payload.Name != "Team dinner" || payload.Description != "bring a friend" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.StartsAt != start || payload.EndsAt != start+7200 {
		t.Fatalf("times = %d..%d", payload.StartsAt, payload.EndsAt)
	}
	if !payload.ExtraGuestsAllowed {
		t.Error("extra guests were allowed and the payload forgot")
	}

	// The venue is an ordinary shared place, and it takes the same route to a
	// map: a PNG the daemon will stitch, sized like every other map.
	if payload.Location == nil || payload.Location.Name != "Cafe Noir" {
		t.Fatalf("venue = %+v", payload.Location)
	}
	if input.MediaMimeType != "image/png" || input.MediaWidth != mapOutputWidth {
		t.Fatalf("an event with a venue must claim the map it will draw: %q %d", input.MediaMimeType, input.MediaWidth)
	}
}

// An event with no venue has no map, and must not look like something waiting
// to be downloaded. The kind alone cannot answer this, which is the whole
// reason MessageCarriesMedia exists.
func TestOnlyAnEventWithAVenueCarriesMedia(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := client.mediaMessageInput(context.Background(), mediaIngestEvent("ev2", &waE2E.Message{
		EventMessage: &waE2E.EventMessage{
			Name:     proto.String("Standup"),
			JoinLink: proto.String("https://call.whatsapp.com/video/abc"),
		},
	}), ingestOptions{source: sourceLive})
	if !ok {
		t.Fatal("an event with no venue still ingests")
	}
	if input.MediaMimeType != "" || input.MediaWidth != 0 {
		t.Fatalf("an event with no venue claimed media: %q %d", input.MediaMimeType, input.MediaWidth)
	}

	withoutVenue := appstore.Message{MediaKind: appstore.MediaKindEvent, PayloadJSON: input.PayloadJSON}
	if appstore.MessageCarriesMedia(withoutVenue) {
		t.Error("an event that is only a call link looked downloadable")
	}
	if isLocationKind(withoutVenue) {
		t.Error("an event with no venue was sent down the map-drawing path")
	}

	venue, err := appstore.EncodePayload(appstore.MessagePayload{Event: &appstore.EventPayload{
		Name:     "Dinner",
		Location: &appstore.LocationPayload{Latitude: 12.9716, Longitude: 77.5946},
	}})
	if err != nil {
		t.Fatal(err)
	}
	withVenue := appstore.Message{MediaKind: appstore.MediaKindEvent, PayloadJSON: venue}
	if !appstore.MessageCarriesMedia(withVenue) {
		t.Error("an event with a venue has a map and must say so")
	}
	if !isLocationKind(withVenue) {
		t.Error("an event with a venue must take the map-drawing path")
	}
	if place := mapLocationFor(withVenue); place == nil || place.Latitude != 12.9716 {
		t.Fatalf("the map path could not find the venue: %+v", place)
	}
}

// The wire enum and the stored string have to round trip, or an answer means
// something different coming in than it does going out.
func TestEventResponseValuesRoundTrip(t *testing.T) {
	cases := []struct {
		wire  waE2E.EventResponseMessage_EventResponseType
		value string
	}{
		{waE2E.EventResponseMessage_GOING, appstore.EventResponseGoing},
		{waE2E.EventResponseMessage_NOT_GOING, appstore.EventResponseNotGoing},
		{waE2E.EventResponseMessage_MAYBE, appstore.EventResponseMaybe},
	}
	for _, tc := range cases {
		if got := eventResponseValue(tc.wire); got != tc.value {
			t.Fatalf("eventResponseValue(%v) = %q, want %q", tc.wire, got, tc.value)
		}
		got, ok := eventResponseType(tc.value)
		if !ok || got != tc.wire {
			t.Fatalf("eventResponseType(%q) = %v, %v", tc.value, got, ok)
		}
	}

	// An answer we cannot name is still somebody having answered, so it lands
	// as "maybe" rather than as no answer at all.
	if got := eventResponseValue(waE2E.EventResponseMessage_UNKNOWN); got != appstore.EventResponseMaybe {
		t.Fatalf("unknown response = %q", got)
	}
	// But we never *send* one we cannot name.
	if _, ok := eventResponseType("interested"); ok {
		t.Error("an unknown response was accepted for sending")
	}
}

// A quoted event names itself, the same way a quoted location says where.
func TestQuotedEventPreviewNamesTheEvent(t *testing.T) {
	text, kind, mime := quotedReplyPreview(&waE2E.Message{
		EventMessage: &waE2E.EventMessage{Name: proto.String("Team dinner")},
	})
	if text != "Team dinner" || kind != appstore.MediaKindEvent || mime != "" {
		t.Fatalf("quotedReplyPreview() = %q, %q, %q", text, kind, mime)
	}
}
