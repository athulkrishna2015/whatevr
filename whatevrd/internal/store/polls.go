package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Poll state. The tally is attached to message rows at read time from real
// tables, exactly the way reactions are, rather than denormalized into the
// row's payload. A poll is mutated by every vote, and a snapshot rewritten on
// each one is a snapshot that can be stale; a batched join cannot be.

// PollOption is one choice on a poll.
type PollOption struct {
	Index int
	Name  string
	// SHA256 is what a decrypted vote names. Votes are matched to options by
	// hash and never by text, which is also why an option can be added later
	// without invalidating votes already cast.
	SHA256 []byte
	// Voters are the participants who chose this option, in the order their
	// votes landed.
	Voters []PollVoter
}

// PollVoter is one participant's choice.
type PollVoter struct {
	JID             string
	DisplayName     string
	AvatarLocalPath string
	VotedAtUnix     int64
	FromMe          bool
}

// PollState is everything about a poll that is not in its payload.
type PollState struct {
	Options []PollOption
	// TotalVoters counts distinct participants, not selections: a poll that
	// allows several answers would otherwise report more votes than voters.
	TotalVoters int
}

// SavePollOptions records a poll's choices. Called once, when the poll lands.
func (db *DB) SavePollOptions(ctx context.Context, messageID string, options []PollOption) error {
	if messageID == "" || len(options) == 0 {
		return nil
	}
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, option := range options {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO poll_options (message_id, idx, name, sha256)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(message_id, idx) DO UPDATE SET name = excluded.name, sha256 = excluded.sha256
		`, messageID, option.Index, option.Name, option.SHA256); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddPollOption appends a choice somebody added after the poll was created.
func (db *DB) AddPollOption(ctx context.Context, messageID, name string, sha256 []byte) error {
	if messageID == "" || name == "" {
		return nil
	}
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO poll_options (message_id, idx, name, sha256)
		SELECT ?, COALESCE(MAX(idx), -1) + 1, ?, ? FROM poll_options WHERE message_id = ?
		ON CONFLICT(message_id, idx) DO NOTHING
	`, messageID, name, sha256, messageID)
	return err
}

// PollOptionHashes returns a poll's option hashes, for matching a decrypted
// vote back to the choices it names.
func (db *DB) PollOptionHashes(ctx context.Context, messageID string) ([]PollOption, error) {
	rows, err := db.reader().QueryContext(ctx, `
		SELECT idx, name, sha256 FROM poll_options WHERE message_id = ? ORDER BY idx ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var options []PollOption
	for rows.Next() {
		var option PollOption
		if err := rows.Scan(&option.Index, &option.Name, &option.SHA256); err != nil {
			return nil, err
		}
		options = append(options, option)
	}
	return options, rows.Err()
}

// PollVoteHashes returns one voter's current selection. It exists so our own
// vote can be put back when the send that was supposed to publish it fails:
// showing a vote we never managed to cast is worse than showing none.
func (db *DB) PollVoteHashes(ctx context.Context, messageID, voterJID string) ([][]byte, error) {
	rows, err := db.reader().QueryContext(ctx, `
		SELECT option_sha FROM poll_votes WHERE message_id = ? AND voter_jid = ?
	`, messageID, voterJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hashes [][]byte
	for rows.Next() {
		var hash []byte
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, rows.Err()
}

// ApplyPollVote replaces one voter's entire selection. A vote message carries a
// voter's whole current choice rather than a delta, so anything else would
// accumulate every option they ever touched.
//
// A vote older than the one already recorded for that voter is dropped.
// WhatsApp redelivers messages on reconnect and history sync promises no
// ordering, so without this an old vote coming round again silently undoes a
// newer one, including one the voter had withdrawn entirely.
//
// Reports whether the vote was applied.
func (db *DB) ApplyPollVote(ctx context.Context, messageID, voterJID string, optionHashes [][]byte, votedAtUnix int64) (bool, error) {
	if messageID == "" || voterJID == "" {
		return false, nil
	}
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var lastVotedAt int64
	err = tx.QueryRowContext(ctx, `
		SELECT voted_at FROM poll_voters WHERE message_id = ? AND voter_jid = ?
	`, messageID, voterJID).Scan(&lastVotedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	// Equal stamps still apply: a vote replayed at the same second is the same
	// vote, and re-applying it changes nothing.
	if err == nil && votedAtUnix < lastVotedAt {
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO poll_voters (message_id, voter_jid, voted_at) VALUES (?, ?, ?)
		ON CONFLICT(message_id, voter_jid) DO UPDATE SET voted_at = excluded.voted_at
	`, messageID, voterJID, votedAtUnix); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM poll_votes WHERE message_id = ? AND voter_jid = ?
	`, messageID, voterJID); err != nil {
		return false, err
	}
	for _, hash := range optionHashes {
		if len(hash) == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO poll_votes (message_id, voter_jid, option_sha, voted_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(message_id, voter_jid, option_sha) DO UPDATE SET voted_at = excluded.voted_at
		`, messageID, voterJID, hash, votedAtUnix); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// PendingPollVote is a vote that arrived before the poll it votes on.
type PendingPollVote struct {
	ID            string
	ChatID        string
	PollMessageID string
	VoterJID      string
	EncPayload    []byte
	EncIV         []byte
	SenderTSMS    int64
}

// ParkPollVote stores a vote whose poll we have not seen yet. History sync
// promises no ordering and a resend can outrun its original, so a vote arriving
// first is ordinary rather than exceptional.
func (db *DB) ParkPollVote(ctx context.Context, vote PendingPollVote) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO poll_votes_pending (id, chat_id, poll_message_id, voter_jid, enc_payload, enc_iv, sender_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enc_payload = excluded.enc_payload,
			enc_iv = excluded.enc_iv,
			sender_ts = excluded.sender_ts
	`, vote.ID, vote.ChatID, vote.PollMessageID, vote.VoterJID, vote.EncPayload, vote.EncIV, vote.SenderTSMS)
	return err
}

// TakePendingPollVotes removes and returns the votes parked for one poll, so
// they can be decrypted now that it has arrived.
func (db *DB) TakePendingPollVotes(ctx context.Context, pollMessageID string) ([]PendingPollVote, error) {
	rows, err := db.reader().QueryContext(ctx, `
		SELECT id, chat_id, poll_message_id, voter_jid, enc_payload, enc_iv, sender_ts
		FROM poll_votes_pending WHERE poll_message_id = ?
		ORDER BY sender_ts ASC
	`, pollMessageID)
	if err != nil {
		return nil, err
	}
	var votes []PendingPollVote
	for rows.Next() {
		var vote PendingPollVote
		if err := rows.Scan(&vote.ID, &vote.ChatID, &vote.PollMessageID, &vote.VoterJID,
			&vote.EncPayload, &vote.EncIV, &vote.SenderTSMS); err != nil {
			rows.Close()
			return nil, err
		}
		votes = append(votes, vote)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(votes) == 0 {
		return nil, nil
	}
	if _, err := db.conn.ExecContext(ctx, `
		DELETE FROM poll_votes_pending WHERE poll_message_id = ?
	`, pollMessageID); err != nil {
		return nil, err
	}
	return votes, nil
}

// PrunePendingPollVotes drops parked votes whose poll never arrived, so a chat
// that lost a poll to a delete does not keep its votes forever.
func (db *DB) PrunePendingPollVotes(ctx context.Context, olderThanUnix int64) error {
	_, err := db.conn.ExecContext(ctx, `
		DELETE FROM poll_votes_pending WHERE created_at < ?
	`, olderThanUnix)
	return err
}

// attachPolls loads the tally for every poll row in a page, in two batched
// queries, and hangs it off the messages. Rows that are not polls are skipped
// entirely, so an ordinary conversation pays nothing.
func attachPolls(ctx context.Context, q reactionQueryer, messages []Message, selfJID string) error {
	ids := make([]any, 0)
	indexByID := make(map[string]int, len(messages))
	for i := range messages {
		if messages[i].MediaKind != MediaKindPoll {
			continue
		}
		messages[i].Poll = nil
		ids = append(ids, messages[i].ID)
		indexByID[messages[i].ID] = i
	}
	if len(ids) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	// Options first, collected per poll. They are gathered into their own slice
	// rather than appended straight onto the message, because the vote pass
	// below needs a stable way to find an option by hash and a slice that is
	// still growing cannot provide one.
	optionsByMessage := make(map[string][]PollOption, len(ids))
	optionRows, err := q.QueryContext(ctx, `
		SELECT message_id, idx, name, sha256
		FROM poll_options
		WHERE message_id IN (`+placeholders+`)
		ORDER BY message_id ASC, idx ASC
	`, ids...)
	if err != nil {
		return err
	}
	for optionRows.Next() {
		var messageID string
		var option PollOption
		if err := optionRows.Scan(&messageID, &option.Index, &option.Name, &option.SHA256); err != nil {
			optionRows.Close()
			return err
		}
		if _, ok := indexByID[messageID]; !ok {
			continue
		}
		optionsByMessage[messageID] = append(optionsByMessage[messageID], option)
	}
	optionRows.Close()
	if err := optionRows.Err(); err != nil {
		return err
	}

	// Now the options are final, so an option can be addressed by its position.
	positionByHash := make(map[string]map[string]int, len(optionsByMessage))
	for messageID, options := range optionsByMessage {
		byHash := make(map[string]int, len(options))
		for i, option := range options {
			byHash[string(option.SHA256)] = i
		}
		positionByHash[messageID] = byHash
	}

	// The name and avatar are resolved exactly as a message's sender is, so a
	// face in the tally is the same face the transcript shows.
	voteRows, err := q.QueryContext(ctx, `
		SELECT v.message_id, v.voter_jid, v.option_sha, v.voted_at,
		       COALESCE(NULLIF(s.name, ''), NULLIF(c.name, ''), ''),
		       COALESCE(NULLIF(sa.local_path, ''), NULLIF(s.avatar_local_path, ''), '')
		FROM poll_votes v
		LEFT JOIN senders s ON s.id = v.voter_jid
		LEFT JOIN chats c ON c.id = v.voter_jid
		LEFT JOIN avatars sa ON sa.subject_kind = 'sender' AND sa.subject_id = v.voter_jid
		WHERE v.message_id IN (`+placeholders+`)
		ORDER BY v.voted_at ASC, v.voter_jid ASC
	`, ids...)
	if err != nil {
		return err
	}
	defer voteRows.Close()

	voters := make(map[string]map[string]struct{}, len(ids))
	for voteRows.Next() {
		var messageID, voterJID, displayName, avatarPath string
		var optionSHA []byte
		var votedAt int64
		if err := voteRows.Scan(&messageID, &voterJID, &optionSHA, &votedAt, &displayName, &avatarPath); err != nil {
			return err
		}
		position, ok := positionByHash[messageID][string(optionSHA)]
		if !ok {
			// A vote for an option we do not have: the poll was edited, or the
			// sender chose something never announced. Counting it against
			// nothing beats inventing an option for it.
			continue
		}
		optionsByMessage[messageID][position].Voters = append(
			optionsByMessage[messageID][position].Voters, PollVoter{
				JID:             voterJID,
				DisplayName:     displayName,
				AvatarLocalPath: avatarPath,
				VotedAtUnix:     votedAt,
				FromMe:          selfJID != "" && voterJID == selfJID,
			})
		if voters[messageID] == nil {
			voters[messageID] = make(map[string]struct{})
		}
		voters[messageID][voterJID] = struct{}{}
	}
	if err := voteRows.Err(); err != nil {
		return err
	}

	for messageID, options := range optionsByMessage {
		i, ok := indexByID[messageID]
		if !ok {
			continue
		}
		messages[i].Poll = &PollState{
			Options:     options,
			TotalVoters: len(voters[messageID]),
		}
	}
	return nil
}

// daemonConfigSelfJIDKey holds the account's own jid, so a poll tally can mark
// our own vote without the store having to ask the network layer who we are.
const daemonConfigSelfJIDKey = "self_jid"

// SetSelfJID records the account's own jid and caches it. Reading it per page
// would be a daemon_config query on every message list for a value that changes
// once a login.
func (db *DB) SetSelfJID(ctx context.Context, jid string) error {
	if err := db.SetDaemonConfig(ctx, daemonConfigSelfJIDKey, jid); err != nil {
		return err
	}
	db.selfJID.Store(&jid)
	return nil
}

// loadSelfJID primes the cache at startup from whatever the last login stored.
func (db *DB) loadSelfJID(ctx context.Context) {
	jid, err := db.GetDaemonConfig(ctx, daemonConfigSelfJIDKey)
	if err != nil {
		return
	}
	db.selfJID.Store(&jid)
}

func (db *DB) cachedSelfJID() string {
	if jid := db.selfJID.Load(); jid != nil {
		return *jid
	}
	return ""
}
