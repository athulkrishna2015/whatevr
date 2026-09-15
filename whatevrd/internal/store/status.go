package store

import (
	"context"
	"errors"
	"time"
)

// StatusUpdate is one contact's status (story): text or a media payload.
// Statuses arrive from status@broadcast and live outside chats on purpose —
// storing them as chat messages would materialize a bogus "status" chat row.
type StatusUpdate struct {
	ID                string
	SenderID          string
	SenderName        string
	TimestampUnix     int64
	Kind              string
	Text              string
	MediaMimeType     string
	MediaKind         string
	MediaLocalPath    string
	MediaPayload      []byte
	MediaDurationSecs int32
	MediaSizeBytes    int64
	MediaFileName     string
	Viewed            bool
}

// StatusUpdateInput is the ingest form of a StatusUpdate.
type StatusUpdateInput struct {
	ID                string
	SenderID          string
	SenderName        string
	Timestamp         time.Time
	Kind              string
	Text              string
	MediaMimeType     string
	MediaKind         string
	MediaPayload      []byte
	MediaDurationSecs int32
	MediaSizeBytes    int64
	MediaFileName     string
}

func (db *DB) SaveStatusUpdate(ctx context.Context, input StatusUpdateInput) (StatusUpdate, bool, error) {
	defer db.timeOp("SaveStatusUpdate", time.Now())
	if input.ID == "" {
		return StatusUpdate{}, false, errors.New("status id is required")
	}
	if input.SenderID == "" {
		return StatusUpdate{}, false, errors.New("status sender is required")
	}
	if input.Timestamp.IsZero() {
		input.Timestamp = time.Now()
	}
	if input.MediaPayload == nil {
		input.MediaPayload = []byte{}
	}

	result, err := db.conn.ExecContext(ctx, `
		INSERT INTO status_updates (id, sender_id, sender_name, timestamp, kind, text, media_mime_type, media_kind, media_payload, media_duration_secs, media_size_bytes, media_file_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, input.ID, input.SenderID, input.SenderName, input.Timestamp.Unix(), input.Kind, input.Text, input.MediaMimeType, input.MediaKind, input.MediaPayload, input.MediaDurationSecs, input.MediaSizeBytes, input.MediaFileName)
	if err != nil {
		return StatusUpdate{}, false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return StatusUpdate{}, false, err
	}
	status, err := db.GetStatusUpdate(ctx, input.ID)
	if err != nil {
		return StatusUpdate{}, false, err
	}
	return status, rowsAffected > 0, nil
}

func statusUpdateFromRow(id, senderID, senderName string, timestampUnix int64, kind, text, mediaMimeType, mediaKind, mediaLocalPath string, mediaPayload []byte, durationSecs int32, sizeBytes int64, fileName string, viewed bool) StatusUpdate {
	return StatusUpdate{
		ID:                id,
		SenderID:          senderID,
		SenderName:        senderName,
		TimestampUnix:     timestampUnix,
		Kind:              kind,
		Text:              text,
		MediaMimeType:     mediaMimeType,
		MediaKind:         mediaKind,
		MediaLocalPath:    mediaLocalPath,
		MediaPayload:      mediaPayload,
		MediaDurationSecs: durationSecs,
		MediaSizeBytes:    sizeBytes,
		MediaFileName:     fileName,
		Viewed:            viewed,
	}
}

func (db *DB) GetStatusUpdate(ctx context.Context, id string) (StatusUpdate, error) {
	defer db.timeOp("GetStatusUpdate", time.Now())
	var s StatusUpdate
	err := db.reader().QueryRowContext(ctx, `
		SELECT id, sender_id, sender_name, timestamp, kind, text, media_mime_type, media_kind, media_local_path, media_payload, media_duration_secs, media_size_bytes, media_file_name, is_viewed
		FROM status_updates
		WHERE id = ?
	`, id).Scan(&s.ID, &s.SenderID, &s.SenderName, &s.TimestampUnix, &s.Kind, &s.Text, &s.MediaMimeType, &s.MediaKind, &s.MediaLocalPath, &s.MediaPayload, &s.MediaDurationSecs, &s.MediaSizeBytes, &s.MediaFileName, &s.Viewed)
	if err != nil {
		return StatusUpdate{}, err
	}
	return s, nil
}

// ListStatusUpdates returns statuses newest first, optionally capped.
func (db *DB) ListStatusUpdates(ctx context.Context, limit int) ([]StatusUpdate, error) {
	defer db.timeOp("ListStatusUpdates", time.Now())
	query := `
		SELECT id, sender_id, sender_name, timestamp, kind, text, media_mime_type, media_kind, media_local_path, media_payload, media_duration_secs, media_size_bytes, media_file_name, is_viewed
		FROM status_updates
		ORDER BY timestamp DESC, rowid DESC
	`
	args := []any{}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := []StatusUpdate{}
	for rows.Next() {
		var s StatusUpdate
		if err := rows.Scan(&s.ID, &s.SenderID, &s.SenderName, &s.TimestampUnix, &s.Kind, &s.Text, &s.MediaMimeType, &s.MediaKind, &s.MediaLocalPath, &s.MediaPayload, &s.MediaDurationSecs, &s.MediaSizeBytes, &s.MediaFileName, &s.Viewed); err != nil {
			return nil, err
		}
		statuses = append(statuses, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return statuses, nil
}

// MarkStatusViewed flags a status as seen locally. Viewed receipts to the
// sender are a separate TODO; this only drives the local ring/badge state.
func (db *DB) MarkStatusViewed(ctx context.Context, id string) (StatusUpdate, error) {
	defer db.timeOp("MarkStatusViewed", time.Now())
	if _, err := db.conn.ExecContext(ctx, `UPDATE status_updates SET is_viewed = 1 WHERE id = ?`, id); err != nil {
		return StatusUpdate{}, err
	}
	return db.GetStatusUpdate(ctx, id)
}

// SetStatusMediaPath records a downloaded status payload's cache path.
func (db *DB) SetStatusMediaPath(ctx context.Context, id, localPath string) (StatusUpdate, error) {
	defer db.timeOp("SetStatusMediaPath", time.Now())
	if _, err := db.conn.ExecContext(ctx, `UPDATE status_updates SET media_local_path = ? WHERE id = ?`, localPath, id); err != nil {
		return StatusUpdate{}, err
	}
	return db.GetStatusUpdate(ctx, id)
}

// PruneOldStatusUpdates drops statuses older than maxAge; WhatsApp statuses
// expire after 24h, so anything older is dead weight.
func (db *DB) PruneOldStatusUpdates(ctx context.Context, maxAge time.Duration) (int64, error) {
	defer db.timeOp("PruneOldStatusUpdates", time.Now())
	result, err := db.conn.ExecContext(ctx, `DELETE FROM status_updates WHERE timestamp < ?`, time.Now().Add(-maxAge).Unix())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
