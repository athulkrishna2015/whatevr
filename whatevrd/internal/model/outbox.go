package model

import (
	"context"
	"encoding/json"
	"strings"

	"whatevrd/internal/core"
)

// outbox is every send this daemon queued. a message goes out only from
// here: one the phone left pending or failed in history never does.
var outboxDomain = core.Domain{
	Name:    "outbox",
	Version: 2,
	Tables:  []string{"outbox", "outbox_try"},
	Schema: []string{
		// queued_t 0 is a cancel seen before its queue. body and file are
		// the queue's, see OutboxHead
		`CREATE TABLE outbox (
			chat      TEXT NOT NULL,
			id        TEXT NOT NULL,
			queued_t  INTEGER NOT NULL DEFAULT 0,
			cancelled INTEGER NOT NULL DEFAULT 0,
			body      BLOB NOT NULL DEFAULT x'',
			file      TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (chat, id)
		)`,
		// one row per failed try, so a try logged twice counts once
		`CREATE TABLE outbox_try (
			chat  TEXT NOT NULL,
			id    TEXT NOT NULL,
			t     INTEGER NOT NULL,
			error TEXT NOT NULL,
			final INTEGER NOT NULL,
			PRIMARY KEY (chat, id, t, error)
		)`,
	},
	Folds: map[string]core.FoldFunc{core.KindOutbox: foldOutbox},
}

func foldOutbox(tx *core.Tx, in core.Input) error {
	var h core.OutboxHead
	if err := json.Unmarshal(in.Head, &h); err != nil {
		return err
	}
	if h.Chat == "" || h.ID == "" {
		return nil
	}
	t := in.At.UnixMilli()
	var err error
	switch h.Op {
	case core.OutboxQueue:
		// one id is queued once; were it twice, the first queue wins
		_, err = tx.Exec(`INSERT INTO outbox (chat, id, queued_t, body, file) VALUES (?, ?, ?, COALESCE(?, x''), ?)
			ON CONFLICT (chat, id) DO UPDATE SET queued_t = excluded.queued_t, body = excluded.body, file = excluded.file
			WHERE outbox.queued_t = 0 OR (excluded.queued_t, excluded.body, excluded.file) < (outbox.queued_t, outbox.body, outbox.file)`,
			h.Chat, h.ID, t, in.Body, h.File)
	case core.OutboxCancel:
		_, err = tx.Exec(`INSERT INTO outbox (chat, id, cancelled) VALUES (?, ?, 1)
			ON CONFLICT (chat, id) DO UPDATE SET cancelled = 1`, h.Chat, h.ID)
	case core.OutboxAttempt:
		_, err = tx.Exec(`INSERT INTO outbox_try (chat, id, t, error, final) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT DO UPDATE SET final = MAX(final, excluded.final)`, h.Chat, h.ID, t, h.Error, h.Final)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	tx.Touch("message", h.Chat+":"+h.ID)
	tx.Touch("chat", h.Chat)
	return nil
}

// Outgoing is one send this daemon queued, as far as it got.
type Outgoing struct {
	Chat, ID  string
	T         int64
	Cancelled bool
	Failed    bool
	Attempts  int
	Error     string
	// Sent is the message in the log as this device's own: it went out
	Sent bool
	// Body is the message as it will go, File the media to upload into it
	Body []byte
	File string
}

const outgoingCols = `o.chat, o.id, o.queued_t, o.cancelled,
	EXISTS (SELECT 1 FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id AND x.final = 1),
	(SELECT COUNT(*) FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id),
	COALESCE((SELECT x.error FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id ORDER BY x.t DESC, x.error DESC LIMIT 1), ''),
	EXISTS (SELECT 1 FROM msg_src s WHERE s.id = o.id), o.body, o.file`

func scanOutgoing(row interface{ Scan(...any) error }) (Outgoing, error) {
	var o Outgoing
	err := row.Scan(&o.Chat, &o.ID, &o.T, &o.Cancelled, &o.Failed, &o.Attempts, &o.Error, &o.Sent, &o.Body, &o.File)
	return o, err
}

// Outgoing is the send queued as id in chat. ok is false for anything this
// daemon never queued.
func (r *Reader) Outgoing(ctx context.Context, chat, id string) (Outgoing, bool, error) {
	o, err := scanOutgoing(r.db.QueryRowContext(ctx, `SELECT `+outgoingCols+` FROM outbox o
		WHERE o.chat = ? AND o.id = ? AND o.queued_t > 0`, chat, id))
	if isNoRows(err) {
		return Outgoing{}, false, nil
	}
	return o, err == nil, err
}

// Unsent is every queued send still owed, oldest first.
func (r *Reader) Unsent(ctx context.Context) ([]Outgoing, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+outgoingCols+` FROM outbox o
		WHERE o.queued_t > 0 AND o.cancelled = 0
		AND NOT EXISTS (SELECT 1 FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id AND x.final = 1)
		AND NOT EXISTS (SELECT 1 FROM msg_src s WHERE s.id = o.id)
		ORDER BY o.queued_t, o.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outgoing
	for rows.Next() {
		o, err := scanOutgoing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// queuedRows is the outbox as transcript rows: sends the log has no message
// for yet, cancelled ones gone.
func (r *Reader) queuedRows(ctx context.Context, addrs []string, cmp, order string, args []any, limit int) ([]Message, error) {
	ph := placeholders(len(addrs))
	cmp = strings.NewReplacer("m.t", "o.queued_t", "m.id", "o.id").Replace(cmp)
	rows, err := r.db.QueryContext(ctx, `SELECT `+outgoingCols+` FROM outbox o
		WHERE o.chat IN (`+ph+`) AND o.queued_t > 0 AND o.cancelled = 0 AND `+cmp+`
		AND NOT EXISTS (SELECT 1 FROM msg_src s WHERE s.id = o.id)
		ORDER BY o.queued_t `+order+`, o.id `+order+` LIMIT ?`, append(append(anys(addrs), args...), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		o, err := scanOutgoing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, queuedMessage(o))
	}
	return out, rows.Err()
}

// queuedMessage is a send still owed as a row: its body is the message.
func queuedMessage(o Outgoing) Message {
	m := Message{Chat: o.Chat, ID: o.ID, FromMe: true, T: o.T, Kind: "outbox", Queued: true, Out: o, Body: o.Body}
	if raw, _ := m.Content(); raw != nil {
		u := Unwrap(raw).Msg
		if k := Field(u); k != "" {
			m.Kind = k
		}
		m.Text = searchText(u)
		m.Reply = contextOf(u).GetStanzaID()
	}
	return m
}
