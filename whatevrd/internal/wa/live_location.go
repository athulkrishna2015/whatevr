package wa

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// Live-location correlation.
//
// whatsmeow delivers no help here whatsoever: there is no events.LiveLocation,
// no grouping, no parser. A live share opens as a LocationMessage with IsLive
// set, and every position after it arrives as a bare LiveLocationMessage in its
// own events.Message, carrying a SequenceNumber and a TimeOffset and nothing
// that names the share it belongs to.
//
// So the daemon owns the grouping. An update is matched to the newest open
// share from the same sender in the same chat, appended to that share's trail,
// and folded into the *original* row, which then upserts. The update messages
// never become rows of their own: a share is one thing that moves, not fifty
// messages, and the transcript should say so.

// liveLocationDefaultWindow is how long a share stays open when the opener did
// not say. WhatsApp offers 15 minutes, 1 hour and 8 hours; assuming the longest
// keeps a share we cannot date from closing early, and the sender's own stop
// message closes it properly anyway.
const liveLocationDefaultWindow = 8 * time.Hour

// liveLocationStaleAfter closes a share nothing has updated for this long. A
// sender who walks out of coverage or force-quits never sends a stop, and a
// bubble claiming somebody is live three hours after their last ping is a lie.
const liveLocationStaleAfter = 15 * time.Minute

// handleLiveLocationUpdate folds a LiveLocationMessage into the share it
// belongs to. Returns true when the event was consumed and must not become a
// message row of its own.
func (c *Client) handleLiveLocationUpdate(ctx context.Context, evt *events.Message) bool {
	if evt == nil || evt.Message == nil {
		return false
	}
	update := evt.Message.GetLiveLocationMessage()
	if update == nil {
		return false
	}

	chatJID := c.normalizeJIDForChat(ctx, evt.Info.Chat)
	chatID := chatJID.String()
	sender := senderID(evt.Info)
	now := time.Now().Unix()

	share, err := c.store.LatestOpenLiveLocationShare(ctx, chatID, sender, now)
	if errors.Is(err, sql.ErrNoRows) {
		// The opening message never reached us (a share that started before we
		// linked, or history sync that has not caught up). Rather than dropping
		// the update, promote it into a share of its own so the position is
		// visible; the transcript gains one row instead of none.
		return c.openShareFromUpdate(ctx, evt, chatID, update)
	}
	if err != nil {
		c.log.Warnf("Failed to find live-location share for %s: %v", chatID, err)
		return true
	}

	point := appstore.LiveLocationPoint{
		Seq:            int64(update.GetSequenceNumber()),
		TimestampUnix:  evt.Info.Timestamp.Unix(),
		Latitude:       update.GetDegreesLatitude(),
		Longitude:      update.GetDegreesLongitude(),
		AccuracyMeters: int32(update.GetAccuracyInMeters()),
		SpeedMPS:       float64(update.GetSpeedInMps()),
		HeadingDegrees: int32(update.GetDegreesClockwiseFromMagneticNorth()),
	}
	applied, err := c.store.AppendLiveLocationPoint(ctx, share.MessageID, point)
	if err != nil {
		c.log.Warnf("Failed to append live-location point to %s: %v", share.MessageID, err)
		return true
	}
	if !applied {
		// A replayed or out-of-order update. Consumed, but nothing moved.
		return true
	}

	c.applyLivePositionToRow(ctx, share.MessageID, point)
	return true
}

// openShareFromUpdate turns an orphan update into a share of its own, so a
// position with no opener behind it is still shown rather than dropped.
func (c *Client) openShareFromUpdate(ctx context.Context, evt *events.Message, chatID string, update *waE2E.LiveLocationMessage) bool {
	opts := ingestOptions{source: sourceLive}
	base, _, ok := c.mediaInputBase(ctx, evt, opts, update.GetCaption(), update.GetContextInfo())
	if !ok {
		return true
	}
	payload := &appstore.LocationPayload{
		Latitude:       update.GetDegreesLatitude(),
		Longitude:      update.GetDegreesLongitude(),
		AccuracyMeters: update.GetAccuracyInMeters(),
		Live:           true,
	}
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Location: payload})
	if err != nil {
		c.log.Warnf("Failed to encode orphan live-location payload: %v", err)
		return true
	}

	base.PayloadJSON = encoded

	saved, err := c.store.SaveMediaMessage(ctx, appstore.MediaMessageInput{
		TextMessageInput:        base,
		MediaKind:               appstore.MediaKindLiveLocation,
		MediaMimeType:           "image/png",
		MediaThumbnailLocalPath: c.saveMessageThumbnail(chatID, base.ID, update.GetJPEGThumbnail()),
		MediaWidth:              mapOutputWidth,
		MediaHeight:             mapOutputHeight,
		PayloadSummary:          locationSummary(payload),
	})
	if err != nil {
		c.log.Warnf("Failed to store orphan live-location update: %v", err)
		return true
	}
	c.registerLiveShare(ctx, saved.Message, evt.Info.Timestamp, 0)
	if saved.Inserted {
		c.daemon.PublishNewMessage(toDaemonMessage(saved.Message), toDaemonChat(saved.Chat))
	}
	return true
}

// registerLiveShare opens the share behind a stored live-location row and seeds
// its trail with the opening position.
func (c *Client) registerLiveShare(ctx context.Context, message appstore.Message, startedAt time.Time, window time.Duration) {
	if window <= 0 {
		window = liveLocationDefaultWindow
	}
	if startedAt.IsZero() {
		startedAt = time.Unix(message.TimestampUnix, 0)
	}
	share := appstore.LiveLocationShare{
		MessageID: message.ID,
		ChatID:    message.ChatID,
		SenderID:  message.SenderID,
		StartedAt: startedAt.Unix(),
		ExpiresAt: startedAt.Add(window).Unix(),
	}
	if err := c.store.OpenLiveLocationShare(ctx, share); err != nil {
		c.log.Warnf("Failed to open live-location share for %s: %v", message.ID, err)
		return
	}

	payload := appstore.DecodePayload(message.PayloadJSON)
	if payload.Location != nil {
		if _, err := c.store.AppendLiveLocationPoint(ctx, message.ID, appstore.LiveLocationPoint{
			Seq:            0,
			TimestampUnix:  share.StartedAt,
			Latitude:       payload.Location.Latitude,
			Longitude:      payload.Location.Longitude,
			AccuracyMeters: int32(payload.Location.AccuracyMeters),
		}); err != nil {
			c.log.Warnf("Failed to seed live-location trail for %s: %v", message.ID, err)
		}
	}

	if _, err := c.writeLiveShareState(ctx, message.ID); err != nil {
		c.log.Warnf("Failed to record live-share state for %s: %v", message.ID, err)
	}
}

// writeLiveShareState refreshes the live-share block on a row's payload from
// the share tables. It is what makes the state readable without a join: a
// conversation listing a thousand messages runs one query, not a thousand.
func (c *Client) writeLiveShareState(ctx context.Context, messageID string) (appstore.Message, error) {
	share, err := c.store.GetLiveLocationShare(ctx, messageID)
	if err != nil {
		return appstore.Message{}, err
	}
	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return appstore.Message{}, err
	}
	points, err := c.store.LiveLocationTrail(ctx, messageID, 0)
	if err != nil {
		return appstore.Message{}, err
	}

	payload := appstore.DecodePayload(message.PayloadJSON)
	live := &appstore.LiveSharePayload{
		Active:     !share.Ended && (share.ExpiresAt == 0 || share.ExpiresAt > time.Now().Unix()),
		StartedAt:  share.StartedAt,
		ExpiresAt:  share.ExpiresAt,
		UpdatedAt:  share.LastUpdateAt,
		PointCount: len(points),
	}
	if len(points) > 0 {
		newest := points[len(points)-1]
		live.SpeedMPS = newest.SpeedMPS
		live.HeadingDegrees = newest.HeadingDegrees
	}
	payload.LiveShare = live

	encoded, err := appstore.EncodePayload(payload)
	if err != nil {
		return appstore.Message{}, err
	}
	return c.store.UpdateMessagePayload(ctx, messageID, encoded, message.PayloadSummary)
}

// applyLivePositionToRow moves the share's message row to a new position and
// redraws its map, then publishes both the row and the live view.
func (c *Client) applyLivePositionToRow(ctx context.Context, messageID string, point appstore.LiveLocationPoint) {
	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		c.log.Warnf("Failed to read live-location row %s: %v", messageID, err)
		return
	}
	payload := appstore.DecodePayload(message.PayloadJSON)
	if payload.Location == nil {
		payload.Location = &appstore.LocationPayload{}
	}
	payload.Location.Latitude = point.Latitude
	payload.Location.Longitude = point.Longitude
	payload.Location.AccuracyMeters = uint32(point.AccuracyMeters)
	payload.Location.Live = true

	encoded, err := appstore.EncodePayload(payload)
	if err != nil {
		c.log.Warnf("Failed to encode moved live-location payload for %s: %v", messageID, err)
		return
	}
	if _, err := c.store.UpdateMessagePayload(ctx, messageID, encoded, locationSummary(payload.Location)); err != nil {
		c.log.Warnf("Failed to persist live-location move for %s: %v", messageID, err)
		return
	}
	// Refresh the share block from the tables so `live.updated_at` and the
	// trail length on the row match what just landed.
	updated, err := c.writeLiveShareState(ctx, messageID)
	if err != nil {
		c.log.Warnf("Failed to refresh live-share state for %s: %v", messageID, err)
		return
	}

	c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	c.daemon.PublishLiveLocationsChanged(updated.ChatID)

	// Redrawing the map is a network round trip, so it never blocks the event
	// handler: whatsmeow dispatches events serially and a stalled handler stalls
	// every message behind it.
	go c.redrawLiveLocationMap(c.backgroundContext(), updated)
}

// redrawLiveLocationMap re-stitches a moving share's map. It runs only when the
// row already has one, so a share the user never opened does not quietly pull
// tiles every few seconds.
func (c *Client) redrawLiveLocationMap(ctx context.Context, message appstore.Message) {
	if message.MediaLocalPath == "" {
		return
	}
	updated, err := c.fetchLocationMap(ctx, message, nil)
	if err != nil {
		if !errors.Is(err, ErrMapsDisabled) {
			c.log.Warnf("Failed to redraw live-location map for %s: %v", message.ID, err)
		}
		return
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
}

// liveLocationTrail reads the path a share has taken, for drawing on its map.
func (c *Client) liveLocationTrail(ctx context.Context, message appstore.Message) ([]mapPoint, error) {
	if message.MediaKind != appstore.MediaKindLiveLocation {
		return nil, nil
	}
	points, err := c.store.LiveLocationTrail(ctx, message.ID, 0)
	if err != nil {
		return nil, err
	}
	trail := make([]mapPoint, 0, len(points))
	for _, point := range points {
		trail = append(trail, mapPoint{Lat: point.Latitude, Lng: point.Longitude})
	}
	return trail, nil
}

// liveLocationSweepInterval is how often shares are checked for having run out.
// A share ending is the passage of time, not something anybody tells us, so it
// has to be noticed rather than received.
const liveLocationSweepInterval = time.Minute

// runLiveLocationSweeper closes shares that have expired or gone quiet, for as
// long as the client runs.
func (c *Client) runLiveLocationSweeper(ctx context.Context) {
	ticker := time.NewTicker(liveLocationSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sweepLiveLocationShares(ctx)
		}
	}
}

// sweepLiveLocationShares closes shares whose window has run out or that have
// gone quiet, and republishes their rows so the bubble settles from "live" into
// a summary of where the share went.
func (c *Client) sweepLiveLocationShares(ctx context.Context) {
	now := time.Now()
	expired, err := c.store.ExpireLiveLocationShares(ctx, now.Unix())
	if err != nil {
		c.log.Warnf("Failed to expire live-location shares: %v", err)
	}

	// A share nobody has updated recently is over even if its window says
	// otherwise: a sender who lost signal never gets to send a stop.
	open, err := c.store.ListLiveLocationShares(ctx, "", now.Unix())
	if err != nil {
		c.log.Warnf("Failed to list live-location shares: %v", err)
	}
	staleBefore := now.Add(-liveLocationStaleAfter).Unix()
	for _, share := range open {
		lastSeen := share.LastUpdateAt
		if lastSeen == 0 {
			lastSeen = share.StartedAt
		}
		if lastSeen > staleBefore {
			continue
		}
		if err := c.store.EndLiveLocationShare(ctx, share.MessageID); err != nil {
			c.log.Warnf("Failed to end stale live-location share %s: %v", share.MessageID, err)
			continue
		}
		expired = append(expired, share.MessageID)
	}

	chats := make(map[string]struct{}, len(expired))
	for _, messageID := range expired {
		// Rewriting the share block is what flips `live.active` off, which is
		// what turns the bubble from a moving pin into a summary.
		message, err := c.writeLiveShareState(ctx, messageID)
		if err != nil {
			continue
		}
		c.daemon.PublishMessageUpdated(toDaemonMessage(message))
		chats[message.ChatID] = struct{}{}
	}
	for chatID := range chats {
		c.daemon.PublishLiveLocationsChanged(chatID)
	}
}
