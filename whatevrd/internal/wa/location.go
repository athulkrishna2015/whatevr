package wa

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// Location ingest. A LocationMessage becomes an ordinary media-bearing row
// whose "media" is the map the daemon stitches for it (see maps.go), so it
// inherits the whole download lifecycle: the sender's embedded JPEG is the
// thumbnail and shows instantly offline, media_local_path fills in when the map
// lands, and a failure sets media_download_error like any other fetch.

// locationMessageInput turns a LocationMessage into a row. A share with IsLive
// set opens a live share instead of a static pin, and the updates that follow
// are folded into this same row (see live_location.go).
func (c *Client) locationMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	location := evt.Message.GetLocationMessage()
	if location == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := c.mediaInputBase(ctx, evt, opts, location.GetComment(), location.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := locationPayloadFromMessage(location)
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Location: payload})
	if err != nil {
		c.log.Warnf("Failed to encode location payload for %s: %v", base.ID, err)
	}

	kind := appstore.MediaKindLocation
	if payload.Live {
		kind = appstore.MediaKindLiveLocation
	}

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        kind,
		// The stitched map is a PNG. The row claims that mime up front so a
		// frontend knows what it will get before the fetch finishes.
		MediaMimeType:           "image/png",
		MediaThumbnailLocalPath: c.saveMessageThumbnail(chatID, base.ID, location.GetJPEGThumbnail()),
		MediaWidth:              mapOutputWidth,
		MediaHeight:             mapOutputHeight,
		PayloadJSON:             encoded,
		PayloadSummary:          locationSummary(payload),
	}, true
}

func locationPayloadFromMessage(location *waE2E.LocationMessage) *appstore.LocationPayload {
	return &appstore.LocationPayload{
		Latitude:       location.GetDegreesLatitude(),
		Longitude:      location.GetDegreesLongitude(),
		Name:           strings.TrimSpace(location.GetName()),
		Address:        strings.TrimSpace(location.GetAddress()),
		URL:            strings.TrimSpace(location.GetURL()),
		AccuracyMeters: location.GetAccuracyInMeters(),
		Live:           location.GetIsLive(),
	}
}

// locationSummary is the detail that rides the one-line rendering: the place
// name if the sender named one, its address if not, and the coordinates as a
// last resort, because "📍 Location" alone tells you nothing about which one.
func locationSummary(payload *appstore.LocationPayload) string {
	if payload == nil {
		return ""
	}
	if payload.Name != "" {
		return payload.Name
	}
	if payload.Address != "" {
		return payload.Address
	}
	return formatCoordinates(payload.Latitude, payload.Longitude)
}

// formatCoordinates renders a position at roughly one-metre resolution, which
// is as precise as a shared pin ever is and short enough to read in a chat list.
func formatCoordinates(lat, lng float64) string {
	if lat == 0 && lng == 0 {
		return ""
	}
	return fmt.Sprintf("%.5f, %.5f", lat, lng)
}

// ---- map fetching ----

// daemonConfigMapTileTemplateKey lets an operator point the tile fetch
// somewhere other than the OSM standard layer: a self-hosted renderer, a paid
// provider, a mirror closer to them. It is daemon config rather than a user
// preference because it is a deployment decision, not a per-session one.
const daemonConfigMapTileTemplateKey = "map_tile_url_template"

func mapTileURLTemplate(ctx context.Context, store *appstore.DB) string {
	if store == nil {
		return DefaultMapTileURLTemplate
	}
	template, err := store.GetDaemonConfig(ctx, daemonConfigMapTileTemplateKey)
	if err != nil || strings.TrimSpace(template) == "" {
		return DefaultMapTileURLTemplate
	}
	return template
}

// locationMapPath is where a row's stitched map lives. It is keyed by message
// so a live share redraws over its own file as the pin moves.
func (c *Client) locationMapPath(message appstore.Message) string {
	return filepath.Join(c.paths.MediaCacheDir, "messages", message.ChatID,
		safeMediaFileName(message.ID, ".map.png"))
}

// isLocationKind reports whether a row's media is a map we draw rather than a
// blob WhatsApp holds. An event counts when it named a venue: its map is the
// same map, drawn the same way, and giving events their own lesser one would
// be the only reason an event's place ever looked different from a shared one.
func isLocationKind(message appstore.Message) bool {
	switch message.MediaKind {
	case appstore.MediaKindLocation, appstore.MediaKindLiveLocation:
		return true
	case appstore.MediaKindEvent:
		return appstore.DecodePayload(message.PayloadJSON).Event.HasVenue()
	default:
		return false
	}
}

// mapLocationFor picks the place a row's map should be drawn around, whichever
// kind of row it is.
func mapLocationFor(message appstore.Message) *appstore.LocationPayload {
	payload := appstore.DecodePayload(message.PayloadJSON)
	if payload.Location != nil {
		return payload.Location
	}
	if payload.Event != nil {
		return payload.Event.Location
	}
	return nil
}

// fetchLocationMap draws the map for a location row and persists it as the
// row's media path. It reports tile progress through the same counters an
// ordinary download uses, so the bubble's progress ring works unchanged.
func (c *Client) fetchLocationMap(ctx context.Context, message appstore.Message, progress func(received, total uint64)) (appstore.Message, error) {
	if !c.appPreferences().AutoFetchMaps {
		return appstore.Message{}, ErrMapsDisabled
	}
	payload := mapLocationFor(message)
	if payload == nil {
		return appstore.Message{}, fmt.Errorf("location row %s has no coordinates", message.ID)
	}
	if math.IsNaN(payload.Latitude) || math.IsNaN(payload.Longitude) {
		return appstore.Message{}, fmt.Errorf("location row %s has invalid coordinates", message.ID)
	}

	trail, err := c.liveLocationTrail(ctx, message)
	if err != nil {
		c.log.Warnf("Failed to read live-location trail for %s: %v", message.ID, err)
	}

	outputPath := c.locationMapPath(message)
	err = c.maps.StitchMap(ctx, payload.Latitude, payload.Longitude, trail, outputPath, func(done, total int) {
		if progress != nil {
			progress(uint64(done), uint64(total))
		}
	})
	if err != nil {
		return appstore.Message{}, err
	}
	return c.store.UpdateMessageMediaLocalPathWithDimensions(ctx, message.ID, outputPath, mapOutputWidth, mapOutputHeight)
}
