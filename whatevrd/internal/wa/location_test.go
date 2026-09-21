package wa

import (
	"math"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	appstore "whatevrd/internal/store"
)

func TestLocationPayloadFromMessage(t *testing.T) {
	payload := locationPayloadFromMessage(&waE2E.LocationMessage{
		DegreesLatitude:  proto.Float64(12.9716),
		DegreesLongitude: proto.Float64(77.5946),
		Name:             proto.String("  Cafe Noir  "),
		Address:          proto.String(" 12 MG Road "),
		AccuracyInMeters: proto.Uint32(25),
	})

	if payload.Latitude != 12.9716 || payload.Longitude != 77.5946 {
		t.Fatalf("coordinates = %v, %v", payload.Latitude, payload.Longitude)
	}
	// Senders pad these; a chat-list preview with leading spaces looks broken.
	if payload.Name != "Cafe Noir" || payload.Address != "12 MG Road" {
		t.Fatalf("name = %q, address = %q", payload.Name, payload.Address)
	}
	if payload.AccuracyMeters != 25 {
		t.Fatalf("accuracy = %d", payload.AccuracyMeters)
	}
	if payload.Live {
		t.Error("a plain location is not a live share")
	}
}

func TestLocationSummaryPrefersTheMostUsefulLabel(t *testing.T) {
	cases := []struct {
		name    string
		payload *appstore.LocationPayload
		want    string
	}{
		{"name wins", &appstore.LocationPayload{Name: "Cafe Noir", Address: "12 MG Road", Latitude: 1, Longitude: 2}, "Cafe Noir"},
		{"address when unnamed", &appstore.LocationPayload{Address: "12 MG Road", Latitude: 1, Longitude: 2}, "12 MG Road"},
		{"coordinates as a last resort", &appstore.LocationPayload{Latitude: 12.9716, Longitude: 77.5946}, "12.97160, 77.59460"},
		{"nothing at all", &appstore.LocationPayload{}, ""},
		{"nil payload", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := locationSummary(tc.payload); got != tc.want {
				t.Fatalf("locationSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The slippy-map projection is the one piece of arithmetic here that is easy to
// get subtly wrong and impossible to notice by eye: an off-by-one in the zoom
// exponent still produces a map, just of somewhere else.
func TestProjectToPixels(t *testing.T) {
	const zoom = 16
	worldSize := float64(mapTileSize) * math.Exp2(zoom)

	// Null Island sits at the exact centre of the projected world.
	x, y := projectToPixels(0, 0, zoom)
	if math.Abs(x-worldSize/2) > 0.001 || math.Abs(y-worldSize/2) > 0.001 {
		t.Fatalf("origin projected to %v, %v; want the world centre %v", x, y, worldSize/2)
	}

	// Longitude is linear: the antimeridian is the two edges.
	westX, _ := projectToPixels(0, -180, zoom)
	eastX, _ := projectToPixels(0, 180, zoom)
	if math.Abs(westX) > 0.001 || math.Abs(eastX-worldSize) > 0.001 {
		t.Fatalf("antimeridian projected to %v and %v; want 0 and %v", westX, eastX, worldSize)
	}

	// North is up: a higher latitude is a smaller y.
	_, northY := projectToPixels(45, 0, zoom)
	_, southY := projectToPixels(-45, 0, zoom)
	if !(northY < worldSize/2 && southY > worldSize/2) {
		t.Fatalf("latitude is inverted: north = %v, south = %v", northY, southY)
	}

	// Mercator cannot represent a pole, so the projection has to clamp rather
	// than hand back an infinity that would later become a NaN pixel index.
	_, poleY := projectToPixels(90, 0, zoom)
	if math.IsInf(poleY, 0) || math.IsNaN(poleY) {
		t.Fatalf("the north pole projected to %v", poleY)
	}
}

func TestTileURLTemplateSubstitution(t *testing.T) {
	f := newMapFetcher(t.TempDir(), "test", "https://example.invalid/{z}/{x}/{y}.png")
	if got := f.tileURL(mapTileCoord{Z: 16, X: 47430, Y: 30177}); got != "https://example.invalid/16/47430/30177.png" {
		t.Fatalf("tileURL() = %q", got)
	}
	// An empty template must not produce a request to nowhere.
	if got := newMapFetcher(t.TempDir(), "test", "  ").urlTemplate; got != DefaultMapTileURLTemplate {
		t.Fatalf("blank template = %q, want the default", got)
	}
}
