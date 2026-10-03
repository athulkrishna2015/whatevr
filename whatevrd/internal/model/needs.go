package model

import (
	"context"
	"database/sql"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// Blob is a history notification by its message id.
type Blob struct {
	ID    string
	Notif *waE2E.HistorySyncNotification
}

// PendingHistory is every history notification whose blob was neither
// downloaded nor given up on, oldest first: what a restart has to fetch.
func PendingHistory(ctx context.Context, db *sql.DB) ([]Blob, error) {
	rows, err := db.QueryContext(ctx, `SELECT n.notification, i.body FROM hist_note n JOIN inputs i ON i.seq = n.seq
		WHERE NOT EXISTS (SELECT 1 FROM hist_blob b WHERE b.notification = n.notification)
		ORDER BY n.seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Blob
	for rows.Next() {
		var id string
		var body []byte
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		var n waE2E.HistorySyncNotification
		if proto.Unmarshal(body, &n) == nil {
			out = append(out, Blob{ID: id, Notif: &n})
		}
	}
	return out, rows.Err()
}

// UnfetchedGroups is every group a chat or message names that no fetch,
// join or refusal has described yet.
func UnfetchedGroups(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT chat FROM (
			SELECT DISTINCT chat FROM msg WHERE chat LIKE '%@g.us'
			UNION SELECT chat FROM hist_chat WHERE chat LIKE '%@g.us'
		) WHERE chat NOT IN (SELECT grp FROM grp_floor) AND chat NOT IN (SELECT grp FROM grp_error)
		ORDER BY chat`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Progress is where history sync stands: the newest blob's own progress for
// its sync type, as the phone counts it.
type Progress struct {
	SyncType string
	Progress uint32
	Chunk    uint32
	Complete bool
	// Pending is blobs announced and not yet in
	Pending int
}

// HistoryProgress is the progress of the sync type that moved last.
func (r *Reader) HistoryProgress(ctx context.Context) (Progress, error) {
	var p Progress
	err := r.db.QueryRowContext(ctx, `SELECT sync_type, progress, chunk FROM hist_note ORDER BY at DESC, seq DESC LIMIT 1`).
		Scan(&p.SyncType, &p.Progress, &p.Chunk)
	if isNoRows(err) {
		return Progress{}, nil
	}
	if err != nil {
		return p, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hist_note n
		WHERE NOT EXISTS (SELECT 1 FROM hist_blob b WHERE b.notification = n.notification)`).Scan(&p.Pending); err != nil {
		return p, err
	}
	// done is every blob in and some blob of the type said 100: chunks come
	// out of order, the highest progress seen is not the end
	var full int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hist_note WHERE sync_type = ? AND progress >= 100`, p.SyncType).Scan(&full); err != nil {
		return p, err
	}
	p.Complete = full > 0 && p.Pending == 0
	if p.Complete {
		p.Progress = 100
	}
	return p, nil
}
