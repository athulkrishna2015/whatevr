package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// An event's RSVPs. Like a poll's tally, these keep moving after the message
// lands, so they are a real table rather than a snapshot in payload_json: the
// event is queried and updated, never rewritten wholesale.

// Event response values, spelled as they cross the wire. The strings are the
// storage format too, because an integer here would mean a lookup table in
// three places to answer "who is coming".
const (
	EventResponseGoing    = "going"
	EventResponseNotGoing = "not_going"
	EventResponseMaybe    = "maybe"
)

// EventResponder is one person's answer.
type EventResponder struct {
	JID             string
	DisplayName     string
	AvatarLocalPath string
	Response        string
	// ExtraGuests is how many people they are bringing, for an event whose
	// author allowed it. It is counted separately from the responders
	// themselves, because a head count and an attendee list are different
	// questions and only one of them has faces.
	ExtraGuests     int
	RespondedAtUnix int64
	FromMe          bool
}

// EventState is everything about an event that is not in its payload.
type EventState struct {
	Responders []EventResponder
	// SelfResponse is our own answer, empty when we have not answered. It is
	// resolved here rather than searched for in Responders, so a bubble can
	// show which chip is ours without walking the list.
	SelfResponse string
	SelfGuests   int
}

// Going counts the people who said yes, plus the guests they are bringing.
func (s *EventState) Going() int {
	if s == nil {
		return 0
	}
	total := 0
	for _, responder := range s.Responders {
		if responder.Response == EventResponseGoing {
			total += 1 + responder.ExtraGuests
		}
	}
	return total
}

// ApplyEventResponse records one person's RSVP, replacing whatever they said
// before. Reports whether anything changed, so a redelivered answer publishes
// nothing.
//
// The staleness rule is the poll's rule for the same reason: WhatsApp
// redelivers on reconnect, and an older answer arriving after a newer one must
// not overwrite it. Equal stamps still apply, because a replay of the same
// answer at the same second is that same answer.
func (db *DB) ApplyEventResponse(ctx context.Context, messageID, responderJID, response string, extraGuests int, respondedAtUnix int64) (bool, error) {
	messageID = strings.TrimSpace(messageID)
	responderJID = strings.TrimSpace(responderJID)
	if messageID == "" || responderJID == "" {
		return false, nil
	}
	if extraGuests < 0 {
		extraGuests = 0
	}

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var lastResponse string
	var lastGuests int
	var lastAt int64
	err = tx.QueryRowContext(ctx, `
		SELECT response, extra_guests, ts FROM event_responses
		WHERE message_id = ? AND responder_jid = ?
	`, messageID, responderJID).Scan(&lastResponse, &lastGuests, &lastAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	known := err == nil
	if known {
		if respondedAtUnix < lastAt {
			return false, nil
		}
		if lastResponse == response && lastGuests == extraGuests {
			// The same answer again. Keeping the newer timestamp would be
			// harmless but publishing an update would not: it redraws every
			// open transcript to say exactly what it already said.
			return false, nil
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_responses (message_id, responder_jid, response, extra_guests, ts)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(message_id, responder_jid) DO UPDATE SET
			response = excluded.response,
			extra_guests = excluded.extra_guests,
			ts = excluded.ts
	`, messageID, responderJID, response, extraGuests, respondedAtUnix); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// EventResponseOf returns one person's current answer, for putting back what an
// optimistic RSVP claimed when the send that was meant to publish it fails.
func (db *DB) EventResponseOf(ctx context.Context, messageID, responderJID string) (string, int, error) {
	var response string
	var guests int
	err := db.conn.QueryRowContext(ctx, `
		SELECT response, extra_guests FROM event_responses
		WHERE message_id = ? AND responder_jid = ?
	`, messageID, responderJID).Scan(&response, &guests)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	return response, guests, nil
}

// ClearEventResponse removes an answer entirely, which is what putting back
// "never answered" means.
func (db *DB) ClearEventResponse(ctx context.Context, messageID, responderJID string) error {
	_, err := db.conn.ExecContext(ctx, `
		DELETE FROM event_responses WHERE message_id = ? AND responder_jid = ?
	`, messageID, responderJID)
	return err
}

// attachEvents loads the RSVPs for every event row in a page in one batched
// query. Rows that are not events are skipped, so an ordinary conversation pays
// nothing for this.
func attachEvents(ctx context.Context, q reactionQueryer, messages []Message, selfJID string) error {
	ids := make([]any, 0)
	indexByID := make(map[string]int, len(messages))
	for i := range messages {
		if messages[i].MediaKind != MediaKindEvent {
			continue
		}
		messages[i].Event = nil
		ids = append(ids, messages[i].ID)
		indexByID[messages[i].ID] = i
	}
	if len(ids) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	// Name and avatar resolve exactly as a message sender's do, so a face on an
	// event is the same face the transcript shows.
	rows, err := q.QueryContext(ctx, `
		SELECT r.message_id, r.responder_jid, r.response, r.extra_guests, r.ts,
		       COALESCE(NULLIF(s.name, ''), NULLIF(c.name, ''), ''),
		       COALESCE(NULLIF(sa.local_path, ''), NULLIF(s.avatar_local_path, ''), '')
		FROM event_responses r
		LEFT JOIN senders s ON s.id = r.responder_jid
		LEFT JOIN chats c ON c.id = r.responder_jid
		LEFT JOIN avatars sa ON sa.subject_kind = 'sender' AND sa.subject_id = r.responder_jid
		WHERE r.message_id IN (`+placeholders+`)
		ORDER BY r.message_id ASC, r.ts ASC, r.responder_jid ASC
	`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var messageID string
		var responder EventResponder
		if err := rows.Scan(&messageID, &responder.JID, &responder.Response,
			&responder.ExtraGuests, &responder.RespondedAtUnix,
			&responder.DisplayName, &responder.AvatarLocalPath); err != nil {
			return err
		}
		index, ok := indexByID[messageID]
		if !ok {
			continue
		}
		responder.FromMe = selfJID != "" && responder.JID == selfJID
		if messages[index].Event == nil {
			messages[index].Event = &EventState{}
		}
		state := messages[index].Event
		state.Responders = append(state.Responders, responder)
		if responder.FromMe {
			state.SelfResponse = responder.Response
			state.SelfGuests = responder.ExtraGuests
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// An event nobody has answered still has an event object, so a bubble can
	// tell "nobody has answered" from "this build does not know about events".
	for _, index := range indexByID {
		if messages[index].Event == nil {
			messages[index].Event = &EventState{}
		}
	}
	return nil
}
