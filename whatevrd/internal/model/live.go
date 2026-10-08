package model

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// a live share opens with a live location and goes on as bare updates that
// name no share. an update belongs to the share of the sender's live row
// before it, when that row is at most liveGap older; then it is no row of its
// own, only a point of that share. hiding only ever grows as rows arrive, so
// any order folds the same.
const (
	liveGap = int64(15 * time.Minute / time.Millisecond)
	// liveWindow is how long a share runs when nothing stops it first: the
	// longest whatsapp offers, the opener does not say
	liveWindow = int64(8 * time.Hour / time.Millisecond)
)

var liveSchema = []string{
	// every live position, shown or not. lat and lng are NULL once the
	// message is deleted
	`CREATE TABLE live (
		chat    TEXT NOT NULL,
		id      TEXT NOT NULL,
		sender  TEXT NOT NULL,
		t       INTEGER NOT NULL,
		opener  INTEGER NOT NULL,
		lat     REAL,
		lng     REAL,
		acc     INTEGER NOT NULL DEFAULT 0,
		speed   REAL NOT NULL DEFAULT 0,
		heading INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (chat, id)
	) WITHOUT ROWID`,
	`CREATE INDEX live_sender ON live (chat, sender, t, id)`,
}

// putLive logs r's position when it is part of a live share, hides the
// updates it is now the row before, and says whether r itself is hidden.
func putLive(tx *core.Tx, r row, m *waE2E.Message) (bool, error) {
	var opener bool
	// nil for a marker, see liveMarker
	var lat, lng *float64
	var acc, heading uint32
	var speed float32
	if x := m.GetLocationMessage(); x.GetIsLive() {
		opener, lat, lng, acc = true, x.DegreesLatitude, x.DegreesLongitude, x.GetAccuracyInMeters()
		speed, heading = x.GetSpeedInMps(), x.GetDegreesClockwiseFromMagneticNorth()
	} else if x := m.GetLiveLocationMessage(); x != nil {
		lat, lng, acc = x.DegreesLatitude, x.DegreesLongitude, x.GetAccuracyInMeters()
		speed, heading = x.GetSpeedInMps(), x.GetDegreesClockwiseFromMagneticNorth()
	} else {
		return false, nil
	}
	if lat == nil || lng == nil {
		lat, lng = nil, nil
	}
	if _, err := tx.Exec(`INSERT INTO live (chat, id, sender, t, opener, lat, lng, acc, speed, heading)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		r.chat, r.id, r.sender, r.t, opener, lat, lng, acc, speed, heading); err != nil {
		return false, err
	}
	rows, err := tx.Query(`SELECT id FROM live WHERE chat = ? AND sender = ? AND opener = 0
		AND (t, id) > (?, ?) AND t <= ?`, r.chat, r.sender, r.t, r.id, r.t+liveGap)
	if err != nil {
		return false, err
	}
	var later []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		later = append(later, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	for _, id := range later {
		res, err := tx.Exec(`DELETE FROM msg WHERE chat = ? AND id = ?`, r.chat, id)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			tx.Touch("message", r.chat+":"+id)
			tx.Touch("chat", r.chat)
		}
	}
	// the share's own row shows the newest point
	if share, ok, err := liveShare(tx, r.chat, r.sender, r.t, r.id); err != nil {
		return false, err
	} else if ok && share != r.id {
		tx.Touch("message", r.chat+":"+share)
	}
	tx.Touch("live", r.chat)
	if opener {
		return false, nil
	}
	return liveHidden(tx, r.chat, r.id)
}

// liveHidden says the live update id in chat is a point of an earlier row's
// share, not a row.
func liveHidden(tx *core.Tx, chat, id string) (bool, error) {
	var hidden bool
	err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM live p WHERE p.chat = u.chat AND p.sender = u.sender
			AND (p.t, p.id) < (u.t, u.id) AND p.t >= u.t - ?)
		FROM live u WHERE u.chat = ? AND u.id = ? AND u.opener = 0`, liveGap, chat, id).Scan(&hidden)
	if isNoRows(err) {
		return false, nil
	}
	return hidden, err
}

// liveShare is the row of the share the live position at (t, id) belongs to:
// the newest row at or before it that is not hidden.
func liveShare(tx *core.Tx, chat, sender string, t int64, id string) (string, bool, error) {
	var share string
	err := tx.QueryRow(`SELECT l.id FROM live l JOIN msg m ON m.chat = l.chat AND m.id = l.id
		WHERE l.chat = ? AND l.sender = ? AND (l.t, l.id) <= (?, ?) ORDER BY l.t DESC, l.id DESC LIMIT 1`,
		chat, sender, t, id).Scan(&share)
	if isNoRows(err) {
		return "", false, nil
	}
	return share, err == nil, err
}

// liveMarker is all a deleted live location keeps: that it was one, so the
// updates after it stay hidden on a rebuild. nil for anything else.
func liveMarker(m *waE2E.Message) *waE2E.Message {
	switch {
	case m.GetLocationMessage().GetIsLive():
		return &waE2E.Message{LocationMessage: &waE2E.LocationMessage{IsLive: proto.Bool(true)}}
	case m.GetLiveLocationMessage() != nil:
		return &waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{}}
	}
	return nil
}

// forgetLive drops where a deleted message said its sender was. its row stays,
// so the updates after it stay hidden.
func forgetLive(tx *core.Tx, cs chats, id string) error {
	_, err := tx.Exec(`UPDATE live SET lat = NULL, lng = NULL, acc = 0, speed = 0, heading = 0
		WHERE id = ? AND `+cs.in("chat"), flat(id, cs)...)
	return err
}

// Live is a share as its row shows it.
type Live struct {
	// Start is the share's first position, Updated its newest
	Start, Updated int64
	Points         int
	Lat, Lng       float64
	Acc            uint32
	Speed          float64
	Heading        int32
	// Known is false when every position of it was deleted
	Known bool
}

// Active says the share still runs at now: inside its window and not gone
// quiet.
func (l Live) Active(now int64) bool {
	return now < l.Start+liveWindow && now <= l.Updated+liveGap
}

// Ends is when the share runs out at the latest.
func (l Live) Ends() int64 { return l.Start + liveWindow }

// LivePoint is one position of a share.
type LivePoint struct {
	T        int64
	Lat, Lng float64
}

// live is the share whose row is (chat, id): its own position and every
// update up to the next row of the sender's.
func (r *Reader) live(ctx context.Context, chat, id string) (Live, []LivePoint, bool, error) {
	var sender string
	var t int64
	err := r.db.QueryRowContext(ctx, `SELECT sender, t FROM live WHERE chat = ? AND id = ?`, chat, id).Scan(&sender, &t)
	if isNoRows(err) {
		return Live{}, nil, false, nil
	}
	if err != nil {
		return Live{}, nil, false, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT l.id, l.t, l.opener, l.lat, l.lng, l.acc, l.speed, l.heading,
			EXISTS (SELECT 1 FROM msg m WHERE m.chat = l.chat AND m.id = l.id)
		FROM live l WHERE l.chat = ? AND l.sender = ? AND (l.t, l.id) >= (?, ?) ORDER BY l.t, l.id`, chat, sender, t, id)
	if err != nil {
		return Live{}, nil, false, err
	}
	defer rows.Close()
	var out Live
	var pts []LivePoint
	prev := t
	for rows.Next() {
		var pid string
		var pt, acc int64
		var opener, shown bool
		var lat, lng *float64
		var speed float64
		var heading int32
		if err := rows.Scan(&pid, &pt, &opener, &lat, &lng, &acc, &speed, &heading, &shown); err != nil {
			return Live{}, nil, false, err
		}
		if pid != id && (opener || shown || pt > prev+liveGap) {
			break
		}
		prev = pt
		if out.Points == 0 {
			out.Start = pt
		}
		out.Points++
		out.Updated = pt
		if lat != nil && lng != nil {
			out.Known = true
			out.Lat, out.Lng, out.Acc, out.Speed, out.Heading = *lat, *lng, uint32(acc), speed, heading
			pts = append(pts, LivePoint{T: pt, Lat: *lat, Lng: *lng})
		}
	}
	return out, pts, out.Points > 0, rows.Err()
}

// LiveTrail is every position of the share whose row is (chat, id), oldest
// first.
func (r *Reader) LiveTrail(ctx context.Context, chat, id string) ([]LivePoint, error) {
	_, pts, _, err := r.live(ctx, chat, id)
	return pts, err
}

// LiveShare is a share still running.
type LiveShare struct {
	Chat, ID, Sender string
	Live             Live
}

// LiveShares is every share running at now in the chat at addrs, or in
// every chat for nil addrs, newest first.
func (r *Reader) LiveShares(ctx context.Context, addrs []string, now int64) ([]LiveShare, error) {
	in := `l.chat IN (SELECT value FROM json_each(?1))`
	if addrs == nil {
		in = `?1 IS NULL`
	}
	var list any
	if addrs != nil {
		list = jsonList(addrs)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT l.chat, l.id, l.sender FROM live l
		JOIN msg m ON m.chat = l.chat AND m.id = l.id
		WHERE `+in+` AND l.t > ?2 ORDER BY l.t DESC, l.id DESC`, list, now-liveWindow)
	if err != nil {
		return nil, err
	}
	var cands []LiveShare
	for rows.Next() {
		var s LiveShare
		if err := rows.Scan(&s.Chat, &s.ID, &s.Sender); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []LiveShare
	for _, s := range cands {
		l, _, ok, err := r.live(ctx, s.Chat, s.ID)
		if err != nil {
			return nil, err
		}
		if ok && l.Known && l.Active(now) {
			s.Live = l
			out = append(out, s)
		}
	}
	return out, nil
}
