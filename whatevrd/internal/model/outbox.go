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
	Version: 5,
	Tables:  []string{"outbox", "outbox_try"},
	Schema: []string{
		// queued_t 0 is a cancel seen before its queue. body and file are
		// the queue's, see OutboxHead
		`CREATE TABLE outbox (
			chat      TEXT NOT NULL,
			id        TEXT NOT NULL,
			queued_t  INTEGER NOT NULL DEFAULT 0,
			ord       INTEGER NOT NULL DEFAULT 0,
			-- the queue's place in the log: what was queued first goes first
			seq       INTEGER NOT NULL DEFAULT 0,
			cancelled INTEGER NOT NULL DEFAULT 0,
			body      BLOB NOT NULL DEFAULT x'',
			file      TEXT NOT NULL DEFAULT '',
			key       TEXT NOT NULL DEFAULT '',
			params    TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (chat, id)
		)`,
		`CREATE INDEX outbox_key ON outbox (key) WHERE key != ''`,
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
		_, err = tx.Exec(`INSERT INTO outbox (chat, id, queued_t, ord, seq, body, file, key, params) VALUES (?, ?, ?, ?, ?, COALESCE(?, x''), ?, ?, ?)
			ON CONFLICT (chat, id) DO UPDATE SET queued_t = excluded.queued_t, ord = excluded.ord, seq = excluded.seq,
				body = excluded.body, file = excluded.file, key = excluded.key, params = excluded.params
			WHERE outbox.queued_t = 0 OR (excluded.queued_t, excluded.body, excluded.file, excluded.key, excluded.params) <
				(outbox.queued_t, outbox.body, outbox.file, outbox.key, outbox.params)`,
			h.Chat, h.ID, t, arrival(in.At, -1), in.Seq, in.Body, h.File, h.Key, h.Params)
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
	Chat, ID string
	T, Ord   int64
	// Seq is where the log queued it, after T in the queue's order
	Seq       int64
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

const outgoingCols = `o.chat, o.id, o.queued_t, o.ord, o.seq, o.cancelled,
	EXISTS (SELECT 1 FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id AND x.final = 1),
	(SELECT COUNT(*) FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id),
	COALESCE((SELECT x.error FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id ORDER BY x.t DESC, x.error DESC LIMIT 1), ''),
	EXISTS (SELECT 1 FROM msg_src s WHERE ` + sentCopy + `), o.body, o.file`

// sentCopy is the sql for "s is the copy of queued send o in the log": ours,
// under its chat or, for a person, the chat's other address. an incoming
// message reusing the id elsewhere is not it.
const sentCopy = `s.id = o.id AND s.sender = '` + Me + `' AND (s.chat = o.chat
	OR (s.chat LIKE '%@lid' OR s.chat LIKE '%@s.whatsapp.net') AND (o.chat LIKE '%@lid' OR o.chat LIKE '%@s.whatsapp.net'))`

func scanOutgoing(row interface{ Scan(...any) error }) (Outgoing, error) {
	var o Outgoing
	err := row.Scan(&o.Chat, &o.ID, &o.T, &o.Ord, &o.Seq, &o.Cancelled, &o.Failed, &o.Attempts, &o.Error, &o.Sent, &o.Body, &o.File)
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

// Keyed is the sends queued under a frontend's key, in the order they were
// queued, and the params digest the first came with.
func (r *Reader) Keyed(ctx context.Context, key string) ([]Outgoing, string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+outgoingCols+`, o.params FROM outbox o
		WHERE o.key = ? AND o.queued_t > 0 ORDER BY o.seq, o.id`, key)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []Outgoing
	var params string
	for rows.Next() {
		var o Outgoing
		var p string
		if err := rows.Scan(&o.Chat, &o.ID, &o.T, &o.Ord, &o.Seq, &o.Cancelled, &o.Failed, &o.Attempts, &o.Error, &o.Sent, &o.Body, &o.File, &p); err != nil {
			return nil, "", err
		}
		if len(out) == 0 {
			params = p
		}
		out = append(out, o)
	}
	return out, params, rows.Err()
}

// owed is the sql for "queued send o is still owed"
const owed = `o.queued_t > 0 AND o.cancelled = 0
	AND NOT EXISTS (SELECT 1 FROM outbox_try x WHERE x.chat = o.chat AND x.id = o.id AND x.final = 1)
	AND NOT EXISTS (SELECT 1 FROM msg_src s WHERE ` + sentCopy + `)`

// Unsent is up to limit queued sends still owed, oldest first, after the
// one at after or from the start when it is nil.
func (r *Reader) Unsent(ctx context.Context, after *Outgoing, limit int) ([]Outgoing, error) {
	var at Outgoing
	if after != nil {
		at = *after
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+outgoingCols+` FROM outbox o
		WHERE `+owed+` AND (o.queued_t, o.seq, o.id) > (?, ?, ?)
		ORDER BY o.queued_t, o.seq, o.id LIMIT ?`, at.T, at.Seq, at.ID, limit)
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

// UnsentCount is how many queued sends are still owed.
func (r *Reader) UnsentCount(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox o WHERE `+owed).Scan(&n)
	return n, err
}

// queuedRows is the outbox as transcript rows: sends the log has no message
// for yet, cancelled ones gone.
func (r *Reader) queuedRows(ctx context.Context, addrs []string, cmp, order string, args []any, limit int) ([]Message, error) {
	ph := placeholders(len(addrs))
	cmp = strings.NewReplacer("m.t", "o.queued_t", "m.ord", "o.ord", "m.id", "o.id").Replace(cmp)
	rows, err := r.db.QueryContext(ctx, `SELECT `+outgoingCols+` FROM outbox o
		WHERE o.chat IN (`+ph+`) AND o.queued_t > 0 AND o.cancelled = 0 AND `+cmp+`
		AND NOT EXISTS (SELECT 1 FROM msg_src s WHERE `+sentCopy+`)
		ORDER BY o.queued_t `+order+`, o.ord `+order+`, o.id `+order+` LIMIT ?`, append(append(anys(addrs), args...), limit)...)
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
	m := Message{Chat: o.Chat, Home: o.Chat, ID: o.ID, FromMe: true, T: o.T, Ord: o.Ord, Kind: "outbox", Queued: true, Out: o, Body: o.Body}
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
