package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// System rows: what the chat did, rather than what anybody said.
//
// A membership change, a renamed group, a changed security code. WhatsApp
// delivers these as events rather than as messages, so they have no id of their
// own and nothing to dedup against. The id is synthesized from what happened
// (SystemMessageID), which is what makes a replayed event land on the row it
// already wrote instead of stacking a second identical pill.
//
// They are quiet by default. A quiet row skips the chat bump entirely: the chat
// list neither reorders nor changes its preview, and nothing counts as unread.
// That is the whole reason this is a separate save path rather than a media
// kind passed through saveMediaMessageTx. Somebody being added to a group four
// hundred messages ago must not push a conversation to the top of the list.
//
// A row is loud only when the event named us, and that decision is made once,
// at ingest, and rides on the row.

// SystemMessageInput is one system event, already resolved to names and already
// summarized into the sentence a pill will read.
type SystemMessageInput struct {
	ChatID     string
	ChatName   string
	IsGroup    bool
	SenderID   string
	SenderName string
	Timestamp  time.Time
	// Summary is the whole line: "Ana, Bo and 12 others joined".
	Summary string
	Payload SystemPayload
	// Loud makes the row behave like a message: it bumps the chat list and
	// counts towards unread. Reserved for events that named us.
	Loud bool
	// CoalesceWith names an existing system row to rewrite in place rather than
	// insert alongside. It is how a burst of joins becomes one pill.
	CoalesceWith string
}

// SystemCoalesceWindow is how close two system events of the same kind have to
// be before the second folds into the first. WhatsApp delivers a bulk add as a
// burst of separate events, seconds apart; anything wider than this is two
// things that happened, and deserves two pills.
const SystemCoalesceWindow = 60 * time.Second

// SystemMessageID synthesizes the id for a system row. It is derived from what
// happened rather than randomly, so the same event delivered twice (a
// reconnect, a resynced group) collides on the primary key and is dropped
// instead of doubling the pill.
func SystemMessageID(chatID, eventType string, timestamp time.Time, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%s:system-%s-%d-%s", chatID, eventType, timestamp.Unix(), hex.EncodeToString(sum[:6]))
}

// NewestSystemMessage returns the chat's newest row when that row is a system
// row, so a caller can decide whether the event in hand folds into it. It
// returns sql.ErrNoRows when the newest row is anything else, because a system
// event that arrives after somebody has spoken starts a new pill: folding it
// into an older one would put it out of order in the transcript.
func (db *DB) NewestSystemMessage(ctx context.Context, chatID string) (Message, error) {
	rows, err := db.reader().QueryContext(ctx, messageSelectPrefix+`
		WHERE m.chat_id = ?
		ORDER BY m.timestamp DESC, m.rowid DESC
		LIMIT 1
	`, chatID)
	if err != nil {
		return Message{}, err
	}
	defer rows.Close()

	messages, err := scanMessageRows(rows, 1)
	if err != nil {
		return Message{}, err
	}
	if len(messages) == 0 || messages[0].MediaKind != MediaKindSystem {
		return Message{}, sql.ErrNoRows
	}
	return messages[0], nil
}

// SaveSystemMessage stores one system row, or rewrites the row named by
// CoalesceWith when the event folds into one already there.
func (db *DB) SaveSystemMessage(ctx context.Context, input SystemMessageInput) (SavedTextMessage, error) {
	defer db.timeOp("SaveSystemMessage", time.Now())
	if input.ChatID == "" {
		return SavedTextMessage{}, errors.New("chat id is required")
	}
	if input.Payload.Type == "" {
		return SavedTextMessage{}, errors.New("system event type is required")
	}
	if input.Timestamp.IsZero() {
		input.Timestamp = time.Now()
	}
	if input.SenderID == "" {
		input.SenderID = input.ChatID
	}

	payloadJSON, err := EncodePayload(MessagePayload{System: &input.Payload})
	if err != nil {
		return SavedTextMessage{}, err
	}

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return SavedTextMessage{}, err
	}
	defer tx.Rollback()

	base := TextMessageInput{
		ID:        input.CoalesceWith,
		ChatID:    input.ChatID,
		ChatName:  input.ChatName,
		SenderID:  input.SenderID,
		Timestamp: input.Timestamp,
		Direction: DirectionIncoming,
		Status:    StatusDelivered,
		IsGroup:   input.IsGroup,
	}
	if err := upsertChat(ctx, tx, base); err != nil {
		return SavedTextMessage{}, err
	}
	if err := upsertSender(ctx, tx, input.SenderID, input.SenderName); err != nil {
		return SavedTextMessage{}, err
	}

	id := input.CoalesceWith
	inserted := false
	if id != "" {
		// Folding into the pill already there: the merged list, the newer
		// sentence and the newer timestamp, all in place. The row keeps its id,
		// so a frontend sees one item change rather than one vanish and another
		// appear where the reader was looking.
		if _, err := tx.ExecContext(ctx, `
			UPDATE messages SET payload_json = ?, payload_summary = ?, timestamp = ?
			WHERE id = ?
		`, payloadJSON, input.Summary, input.Timestamp.Unix(), id); err != nil {
			return SavedTextMessage{}, err
		}
	} else {
		id = SystemMessageID(input.ChatID, input.Payload.Type, input.Timestamp, systemIdentityParts(input)...)
		base.ID = id
		result, err := tx.ExecContext(ctx, `
			INSERT INTO messages (id, chat_id, sender_id, text, timestamp, direction, is_read, status, media_kind, payload_json, payload_summary)
			VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, id, input.ChatID, input.SenderID, input.Timestamp.Unix(), DirectionIncoming,
			boolToInt(!input.Loud), StatusDelivered, MediaKindSystem, payloadJSON, input.Summary)
		if err != nil {
			return SavedTextMessage{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return SavedTextMessage{}, err
		}
		inserted = affected > 0
	}

	// Only a loud row touches the chat: a quiet one must not reorder the list,
	// must not replace the preview, and must not raise a badge.
	if inserted && input.Loud {
		base.CountUnread = true
		if err := bumpChatForInsertedMessage(ctx, tx, base, input.Summary); err != nil {
			return SavedTextMessage{}, err
		}
	}

	message, err := getMessageTx(ctx, tx, id)
	if err != nil {
		return SavedTextMessage{}, err
	}
	chat, err := getChatTx(ctx, tx, input.ChatID)
	if err != nil {
		return SavedTextMessage{}, err
	}
	if err := tx.Commit(); err != nil {
		return SavedTextMessage{}, err
	}
	return SavedTextMessage{Message: message, Chat: chat, Inserted: inserted || input.CoalesceWith != ""}, nil
}

// systemIdentityParts is what makes two deliveries of one event the same event:
// who did it, to whom, and what it set. The timestamp is already in the id.
func systemIdentityParts(input SystemMessageInput) []string {
	parts := make([]string, 0, len(input.Payload.Participants)+2)
	parts = append(parts, input.SenderID, input.Payload.Value)
	for _, participant := range input.Payload.Participants {
		parts = append(parts, participant.JID)
	}
	return parts
}
