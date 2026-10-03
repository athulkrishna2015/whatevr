package whatsapp

import (
	"context"
	"encoding/json"

	"google.golang.org/protobuf/encoding/protojson"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
)

// DefaultPreferences is what a daemon nobody configured does.
func DefaultPreferences() *v2.Preferences {
	return v2.Preferences_builder{
		Notifications:        true,
		NotificationPreview:  true,
		AutoDownloadMaxBytes: 16 << 20,
		AutoFetchMaps:        true,
	}.Build()
}

// Preferences is the stored preferences, the defaults when none are. they
// are kept whole as protojson, written with EmitUnpopulated.
func Preferences(raw json.RawMessage) *v2.Preferences {
	if len(raw) == 0 {
		return DefaultPreferences()
	}
	p := &v2.Preferences{}
	if protojson.Unmarshal(raw, p) != nil {
		return DefaultPreferences()
	}
	return p
}

// prefs is the preferences now, the defaults when the read fails.
func (c *Client) prefs(ctx context.Context) *v2.Preferences {
	raw, err := c.r.Prefs(ctx)
	if err != nil {
		c.log.Warn().Err(err).Msg("whatsapp: read the preferences")
	}
	return Preferences(raw)
}

// SetPreferences sets the fields set has; the rest stay.
func (c *Client) SetPreferences(ctx context.Context, set *v2.PreferencesSet) error {
	p := c.prefs(ctx)
	if set.HasNotifications() {
		p.SetNotifications(set.GetNotifications())
	}
	if set.HasNotificationSound() {
		p.SetNotificationSound(set.GetNotificationSound())
	}
	if set.HasNotificationPreview() {
		p.SetNotificationPreview(set.GetNotificationPreview())
	}
	if set.HasAutoDownloadPhotos() {
		p.SetAutoDownloadPhotos(set.GetAutoDownloadPhotos())
	}
	if set.HasAutoDownloadVideos() {
		p.SetAutoDownloadVideos(set.GetAutoDownloadVideos())
	}
	if set.HasAutoDownloadAudio() {
		p.SetAutoDownloadAudio(set.GetAutoDownloadAudio())
	}
	if set.HasAutoDownloadDocuments() {
		p.SetAutoDownloadDocuments(set.GetAutoDownloadDocuments())
	}
	if set.HasAutoDownloadStickers() {
		p.SetAutoDownloadStickers(set.GetAutoDownloadStickers())
	}
	if set.HasAutoDownloadMaxBytes() {
		p.SetAutoDownloadMaxBytes(set.GetAutoDownloadMaxBytes())
	}
	if set.HasAutoFetchMaps() {
		p.SetAutoFetchMaps(set.GetAutoFetchMaps())
	}
	raw, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(p)
	if err != nil {
		return err
	}
	return c.SetPrefsRaw(ctx, raw)
}

// SetPrefsRaw logs the preferences as they are stored.
func (c *Client) SetPrefsRaw(ctx context.Context, raw json.RawMessage) error {
	return c.append(ctx, core.KindPrefs, core.PrefsHead{Prefs: raw}, nil)
}

// append logs one input of the daemon's own.
func (c *Client) append(ctx context.Context, kind string, head any, body []byte) error {
	h, err := json.Marshal(head)
	if err != nil {
		return err
	}
	_, err = c.core.Append(ctx, core.Input{Kind: kind, V: 1, Head: h, Body: body})
	return err
}
