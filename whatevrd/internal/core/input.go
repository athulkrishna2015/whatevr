package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Input is one thing the daemon took in. Head holds the small fields a fold
// keys on (ids, jids, whatsapp times) as json, Body the protobuf bytes exactly
// as whatsapp sent them, when there are any.
type Input struct {
	Seq  int64
	Kind string
	// V is the version of this kind's head and body layout, so a fold can
	// still read inputs an older daemon wrote.
	V int
	// At is when the daemon took it in, from the clock, at millisecond
	// precision. a fold reads it only where whatsapp gives no time of its own.
	At   time.Time
	Head json.RawMessage
	Body []byte
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// Inputs reads up to limit inputs after seq, in order.
func (db *DB) Inputs(ctx context.Context, after int64, limit int) ([]Input, error) {
	return readInputs(ctx, db.read, after, limit)
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func readInputs(ctx context.Context, q querier, after int64, limit int) ([]Input, error) {
	rows, err := q.QueryContext(ctx, `SELECT seq, kind, v, at, head, body FROM inputs
		WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Input
	for rows.Next() {
		var in Input
		var at int64
		var head sql.NullString
		if err := rows.Scan(&in.Seq, &in.Kind, &in.V, &at, &head, &in.Body); err != nil {
			return nil, err
		}
		in.At = time.UnixMilli(at)
		if head.Valid {
			in.Head = json.RawMessage(head.String)
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
