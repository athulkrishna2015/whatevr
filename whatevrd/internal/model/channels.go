package model

import (
	"context"
)

// Channel is one followed channel: the directory row, never a chat.
type Channel struct {
	JID         string
	Name        string
	Description string
	Followers   int64
	Verified    bool
	Muted       bool
}

// Channels lists followed channels by name. Unfollows (event leave) stay
// out; a re-followed channel comes back on its next join or refresh.
func (r *Reader) Channels(ctx context.Context) ([]Channel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT jid, name, description, followers, verified, mute
		FROM newsletter WHERE event != 'leave' ORDER BY name COLLATE NOCASE, jid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		var mute string
		if err := rows.Scan(&c.JID, &c.Name, &c.Description, &c.Followers, &c.Verified, &mute); err != nil {
			return nil, err
		}
		c.Muted = mute != "" && mute != "off"
		out = append(out, c)
	}
	return out, rows.Err()
}
