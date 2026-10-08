package model

import (
	"context"
	"encoding/json"

	"whatevrd/internal/core"
)

// serverIDsBackfill is the core_meta flag saying the msg_server backfill
// ran. Old rows predate the putMsg hook; without it only new messages would
// carry server ids.
const serverIDsBackfill = "serverids_backfill"

// BackfillServerIDs fills msg_server from the input log's message heads.
// Later messages fill it through the putMsg hook. It appends one backfill
// input and waits for it; the fold is idempotent, so reruns are no-ops.
func BackfillServerIDs(ctx context.Context, db *core.DB) error {
	var done string
	if err := db.Read().QueryRowContext(ctx, `SELECT value FROM core_meta WHERE key = ?`, serverIDsBackfill).Scan(&done); err == nil {
		return nil
	} else if !isNoRows(err) {
		return err
	}
	seq, err := db.Append(ctx, core.Input{Kind: core.KindServerIDs, V: 1, Head: json.RawMessage(`{}`)})
	if err != nil {
		return err
	}
	return db.WaitFolded(ctx, seq)
}

// ServerIDs maps message ids to WhatsApp's server ids, for the messages in
// chat that carry one.
func (r *Reader) ServerIDs(ctx context.Context, chat string, ids []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, server_id FROM msg_server WHERE chat = ? AND id IN (`+placeholders(len(ids))+`)`,
		append([]any{chat}, anys(ids)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var serverID int64
		if err := rows.Scan(&id, &serverID); err != nil {
			return nil, err
		}
		out[id] = serverID
	}
	return out, rows.Err()
}
