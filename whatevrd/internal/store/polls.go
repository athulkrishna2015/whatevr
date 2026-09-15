package store

import (
	"context"
	"time"
)

// RecordPollVotes replaces voterID's votes on a poll with optionNames (a
// re-vote replaces the old ballot, matching WhatsApp). It returns the fresh
// full tally: option name to voter count.
func (db *DB) RecordPollVotes(ctx context.Context, messageID, voterJID string, optionNames []string) (map[string]int, error) {
	defer db.timeOp("RecordPollVotes", time.Now())
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM poll_votes WHERE message_id = ? AND voter_jid = ?`, messageID, voterJID); err != nil {
		return nil, err
	}
	for _, option := range optionNames {
		if option == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO poll_votes (message_id, voter_jid, option_name) VALUES (?, ?, ?) ON CONFLICT(message_id, voter_jid, option_name) DO NOTHING`, messageID, voterJID, option); err != nil {
			return nil, err
		}
	}
	tally := map[string]int{}
	rows, err := tx.QueryContext(ctx, `SELECT option_name, COUNT(*) FROM poll_votes WHERE message_id = ? GROUP BY option_name`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var option string
		var count int
		if err := rows.Scan(&option, &count); err != nil {
			return nil, err
		}
		tally[option] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return tally, nil
}

// PollTally loads the current tally for a poll: option name to voter count.
func (db *DB) PollTally(ctx context.Context, messageID string) (map[string]int, error) {
	defer db.timeOp("PollTally", time.Now())
	tally := map[string]int{}
	rows, err := db.reader().QueryContext(ctx, `SELECT option_name, COUNT(*) FROM poll_votes WHERE message_id = ? GROUP BY option_name`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var option string
		var count int
		if err := rows.Scan(&option, &count); err != nil {
			return nil, err
		}
		tally[option] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tally, nil
}

// PollVoters lists who voted for one option, for the vote-details view.
func (db *DB) PollVoters(ctx context.Context, messageID, optionName string) ([]string, error) {
	defer db.timeOp("PollVoters", time.Now())
	rows, err := db.reader().QueryContext(ctx, `SELECT voter_jid FROM poll_votes WHERE message_id = ? AND option_name = ? ORDER BY voter_jid ASC`, messageID, optionName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	voters := []string{}
	for rows.Next() {
		var voter string
		if err := rows.Scan(&voter); err != nil {
			return nil, err
		}
		voters = append(voters, voter)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return voters, nil
}

// SetMessagePollTally persists the denormalized tally JSON on a poll row so
// every view serves live counts without joining per row.
func (db *DB) SetMessagePollTally(ctx context.Context, id, tallyJSON string) (Message, error) {
	defer db.timeOp("SetMessagePollTally", time.Now())
	if _, err := db.conn.ExecContext(ctx, `UPDATE messages SET poll_tally = ? WHERE id = ?`, tallyJSON, id); err != nil {
		return Message{}, err
	}
	return db.GetMessage(ctx, id)
}

// RecordStatusViewer records that viewerJID viewed a status (from its viewed
// receipt). Repeat views refresh the timestamp.
func (db *DB) RecordStatusViewer(ctx context.Context, statusID, viewerJID string, viewedAt time.Time) error {
	defer db.timeOp("RecordStatusViewer", time.Now())
	if statusID == "" || viewerJID == "" {
		return nil
	}
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO status_viewers (status_id, viewer_jid, viewed_at)
		VALUES (?, ?, ?)
		ON CONFLICT(status_id, viewer_jid) DO UPDATE SET viewed_at = excluded.viewed_at
	`, statusID, viewerJID, viewedAt.Unix())
	return err
}

// StatusViewer is one recorded view of our status.
type StatusViewer struct {
	ViewerJID string
	ViewedAt  int64
}

// ListStatusViewers returns who viewed a status, most recent first.
func (db *DB) ListStatusViewers(ctx context.Context, statusID string) ([]StatusViewer, error) {
	defer db.timeOp("ListStatusViewers", time.Now())
	rows, err := db.reader().QueryContext(ctx, `SELECT viewer_jid, viewed_at FROM status_viewers WHERE status_id = ? ORDER BY viewed_at DESC`, statusID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	viewers := []StatusViewer{}
	for rows.Next() {
		var viewer StatusViewer
		if err := rows.Scan(&viewer.ViewerJID, &viewer.ViewedAt); err != nil {
			return nil, err
		}
		viewers = append(viewers, viewer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return viewers, nil
}
