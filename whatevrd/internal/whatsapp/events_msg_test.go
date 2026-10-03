package whatsapp

import (
	"context"
	"testing"
	"time"
	appstore "whatevrd/internal/store"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestEventIngestKeepsThePlanAndItsVenue(t *testing.T) {
	d := newTestDecoder(t)

	start := time.Now().Add(26 * time.Hour).Unix()
	input, ok := d.mediaMessageInput(context.Background(), mediaIngestEvent("ev1", &waE2E.Message{
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

// A quoted event names itself, the same way a quoted location says where.
func TestQuotedEventPreviewNamesTheEvent(t *testing.T) {
	text, kind, mime := quotedReplyPreview(&waE2E.Message{
		EventMessage: &waE2E.EventMessage{Name: proto.String("Team dinner")},
	})
	if text != "Team dinner" || kind != appstore.MediaKindEvent || mime != "" {
		t.Fatalf("quotedReplyPreview() = %q, %q, %q", text, kind, mime)
	}
}
