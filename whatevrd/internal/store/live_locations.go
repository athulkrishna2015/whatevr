package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Live-location shares. whatsmeow has no concept of these at all: no event, no
// correlation, no parser. An opening LocationMessage with IsLive set and the
// LiveLocationMessage updates that follow it arrive as unrelated messages, and
// tying them together is entirely the daemon's job. Which is as it should be:
// rule 1 says the daemon owns all state, and a frontend that had to stitch a
// share back together from loose messages would be doing the daemon's work.

// LiveLocationShare is one open (or recently closed) live share.
type LiveLocationShare struct {
	MessageID    string
	ChatID       string
	SenderID     string
	StartedAt    int64
	ExpiresAt    int64
	LastSeq      int64
	LastUpdateAt int64
	Ended        bool
}

// LiveLocationPoint is one position on a share's trail.
type LiveLocationPoint struct {
	Seq            int64
	TimestampUnix  int64
	Latitude       float64
	Longitude      float64
	AccuracyMeters int32
	SpeedMPS       float64
	HeadingDegrees int32
}

// OpenLiveLocationShare records the start of a share, or refreshes the window
// of one already open. A share re-opened by a fresh message keeps its own row.
func (db *DB) OpenLiveLocationShare(ctx context.Context, share LiveLocationShare) error {
	if share.MessageID == "" || share.ChatID == "" {
		return errors.New("live location share needs a message and a chat")
	}
	if share.StartedAt == 0 {
		share.StartedAt = time.Now().Unix()
	}
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO live_location_shares (message_id, chat_id, sender_id, started_at, expires_at, last_seq, last_update_at, ended)
		VALUES (?, ?, ?, ?, ?, 0, ?, 0)
		ON CONFLICT(message_id) DO UPDATE SET
			expires_at = MAX(live_location_shares.expires_at, excluded.expires_at),
			ended = 0
	`, share.MessageID, share.ChatID, share.SenderID, share.StartedAt, share.ExpiresAt, share.StartedAt)
	return err
}

// LatestOpenLiveLocationShare finds the share an update belongs to: the newest
// one from this sender in this chat that has not ended. Correlation has to be
// by sender because the update messages carry no reference to the share they
// belong to, only a sequence number within it.
func (db *DB) LatestOpenLiveLocationShare(ctx context.Context, chatID, senderID string, now int64) (LiveLocationShare, error) {
	var share LiveLocationShare
	var ended int
	err := db.reader().QueryRowContext(ctx, `
		SELECT message_id, chat_id, sender_id, started_at, expires_at, last_seq, last_update_at, ended
		FROM live_location_shares
		WHERE chat_id = ? AND sender_id = ? AND ended = 0 AND (expires_at = 0 OR expires_at > ?)
		ORDER BY started_at DESC
		LIMIT 1
	`, chatID, senderID, now).Scan(&share.MessageID, &share.ChatID, &share.SenderID,
		&share.StartedAt, &share.ExpiresAt, &share.LastSeq, &share.LastUpdateAt, &ended)
	share.Ended = ended != 0
	return share, err
}

// AppendLiveLocationPoint records one position. It returns false when the point
// is stale (a sequence number we have already passed), which happens whenever
// WhatsApp replays or reorders updates and is exactly the case that would
// otherwise drag a pin backwards.
func (db *DB) AppendLiveLocationPoint(ctx context.Context, messageID string, point LiveLocationPoint) (bool, error) {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var lastSeq int64
	if err := tx.QueryRowContext(ctx, `
		SELECT last_seq FROM live_location_shares WHERE message_id = ?
	`, messageID).Scan(&lastSeq); err != nil {
		return false, err
	}
	sequenced := point.Seq > 0
	if !sequenced {
		var maxPointSeq int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(seq), 0) FROM live_location_points WHERE message_id = ?
		`, messageID).Scan(&maxPointSeq); err != nil {
			return false, err
		}
		point.Seq = point.TimestampUnix
		if point.Seq <= 0 || point.Seq <= maxPointSeq {
			point.Seq = maxPointSeq + 1
		}
	} else if point.Seq <= lastSeq {
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_location_points (message_id, seq, ts, lat, lng, accuracy_m, speed_mps, heading_deg)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id, seq) DO UPDATE SET
			ts = excluded.ts, lat = excluded.lat, lng = excluded.lng,
			accuracy_m = excluded.accuracy_m, speed_mps = excluded.speed_mps, heading_deg = excluded.heading_deg
	`, messageID, point.Seq, point.TimestampUnix, point.Latitude, point.Longitude,
		point.AccuracyMeters, point.SpeedMPS, point.HeadingDegrees); err != nil {
		return false, err
	}

	if sequenced {
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_location_shares
			SET last_seq = MAX(last_seq, ?), last_update_at = MAX(last_update_at, ?)
			WHERE message_id = ?
		`, point.Seq, point.TimestampUnix, messageID); err != nil {
			return false, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_location_shares
			SET last_update_at = MAX(last_update_at, ?)
			WHERE message_id = ?
		`, point.TimestampUnix, messageID); err != nil {
			return false, err
		}
	}

	return true, tx.Commit()
}

// LiveLocationTrail returns a share's positions oldest first, capped so a share
// running for hours cannot make a message row unbounded. The cap keeps the
// newest points, which are the ones anybody is looking at.
func (db *DB) LiveLocationTrail(ctx context.Context, messageID string, limit int) ([]LiveLocationPoint, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := db.reader().QueryContext(ctx, `
		SELECT seq, ts, lat, lng, accuracy_m, speed_mps, heading_deg
		FROM live_location_points
		WHERE message_id = ?
		ORDER BY ts DESC, seq DESC
		LIMIT ?
	`, messageID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]LiveLocationPoint, 0, limit)
	for rows.Next() {
		var p LiveLocationPoint
		if err := rows.Scan(&p.Seq, &p.TimestampUnix, &p.Latitude, &p.Longitude,
			&p.AccuracyMeters, &p.SpeedMPS, &p.HeadingDegrees); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Read newest first for the LIMIT, hand back oldest first for drawing.
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
	return points, nil
}

// EndLiveLocationShare closes a share, either because the sender stopped it or
// because its window ran out.
func (db *DB) EndLiveLocationShare(ctx context.Context, messageID string) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE live_location_shares SET ended = 1 WHERE message_id = ?
	`, messageID)
	return err
}

// ListLiveLocationShares returns the shares in a chat that are still running.
// Passing an empty chatID returns them across every chat, which is what the
// expiry sweep wants.
func (db *DB) ListLiveLocationShares(ctx context.Context, chatID string, now int64) ([]LiveLocationShare, error) {
	query := `
		SELECT message_id, chat_id, sender_id, started_at, expires_at, last_seq, last_update_at, ended
		FROM live_location_shares
		WHERE ended = 0 AND (expires_at = 0 OR expires_at > ?)
	`
	args := []any{now}
	if chatID != "" {
		query += ` AND chat_id = ?`
		args = append(args, chatID)
	}
	query += ` ORDER BY started_at DESC`

	rows, err := db.reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []LiveLocationShare
	for rows.Next() {
		var share LiveLocationShare
		var ended int
		if err := rows.Scan(&share.MessageID, &share.ChatID, &share.SenderID,
			&share.StartedAt, &share.ExpiresAt, &share.LastSeq, &share.LastUpdateAt, &ended); err != nil {
			return nil, err
		}
		share.Ended = ended != 0
		shares = append(shares, share)
	}
	return shares, rows.Err()
}

// ExpireLiveLocationShares closes every share whose window has passed and
// returns the ids that changed, so their rows can be re-published.
func (db *DB) ExpireLiveLocationShares(ctx context.Context, now int64) ([]string, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT message_id FROM live_location_shares
		WHERE ended = 0 AND expires_at != 0 AND expires_at <= ?
	`, now)
	if err != nil {
		return nil, err
	}
	var expired []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		expired = append(expired, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(expired) == 0 {
		return nil, nil
	}
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, id := range expired {
		if _, err := tx.ExecContext(ctx, `DELETE FROM live_location_points WHERE message_id = ?`, id); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_location_shares SET ended = 1 WHERE ended = 0 AND expires_at != 0 AND expires_at <= ?
	`, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return expired, nil
}

// GetLiveLocationShare reads one share. Missing is sql.ErrNoRows.
func (db *DB) GetLiveLocationShare(ctx context.Context, messageID string) (LiveLocationShare, error) {
	var share LiveLocationShare
	var ended int
	err := db.reader().QueryRowContext(ctx, `
		SELECT message_id, chat_id, sender_id, started_at, expires_at, last_seq, last_update_at, ended
		FROM live_location_shares WHERE message_id = ?
	`, messageID).Scan(&share.MessageID, &share.ChatID, &share.SenderID,
		&share.StartedAt, &share.ExpiresAt, &share.LastSeq, &share.LastUpdateAt, &ended)
	if errors.Is(err, sql.ErrNoRows) {
		return LiveLocationShare{}, err
	}
	share.Ended = ended != 0
	return share, err
}
