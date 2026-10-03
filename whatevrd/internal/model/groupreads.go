package model

import (
	"context"
	"strconv"
)

// Group is what we know of a group's own facts.
type Group struct {
	JID       string
	Name      string
	Topic     string
	TopicBy   string
	Announce  bool
	Locked    bool
	Approval  bool
	Parent    bool
	LinkedTo  string
	Deleted   bool
	Created   int64
	Owner     string
	Ephemeral int
	// Fetched says a fetch ever described it
	Fetched bool
	// Error is why the server would not, newer than any fetch that worked
	Error string
}

// Group reads one group's fields. ok is false when nothing at all is known.
func (r *Reader) Group(ctx context.Context, grp string) (Group, bool, error) {
	g := Group{JID: grp}
	rows, err := r.db.QueryContext(ctx, `SELECT field, value, by FROM grp_field WHERE grp = ?`, grp)
	if err != nil {
		return g, false, err
	}
	found := false
	for rows.Next() {
		var field, value, by string
		if err := rows.Scan(&field, &value, &by); err != nil {
			rows.Close()
			return g, false, err
		}
		found = true
		on := value == "1"
		switch field {
		case "name":
			g.Name = value
		case "topic":
			g.Topic, g.TopicBy = value, by
		case "announce":
			g.Announce = on
		case "locked":
			g.Locked = on
		case "approval":
			g.Approval = on
		case "parent":
			g.Parent = on
		case "linked_to":
			g.LinkedTo = value
		case "deleted":
			g.Deleted = on
		case "created":
			g.Created, _ = strconv.ParseInt(value, 10, 64)
		case "owner":
			g.Owner = value
		case "ephemeral":
			n, _ := strconv.Atoi(value)
			g.Ephemeral = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, false, err
	}
	var one int
	switch err := r.db.QueryRowContext(ctx, `SELECT 1 FROM grp_floor WHERE grp = ?`, grp).Scan(&one); {
	case err == nil:
		g.Fetched, found = true, true
	case !isNoRows(err):
		return g, false, err
	}
	switch err := r.db.QueryRowContext(ctx, `SELECT error FROM grp_error WHERE grp = ?`, grp).Scan(&g.Error); {
	case err == nil:
		found = true
	case !isNoRows(err):
		return g, false, err
	}
	return g, found, nil
}

// Member is one address a group has heard of, in or out.
type Member struct {
	JID   string
	In    bool
	Admin bool
	Super bool
}

// Members is everyone a group ever listed, joined or named in a change,
// with whether the newest evidence says they are in. a fetch that did not
// list someone it could have is evidence they are out.
func (r *Reader) Members(ctx context.Context, grp string) ([]Member, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT m.jid, m.in_t, m.out_t, m.snap_t, m.admin_t, m.admin, m.super,
		COALESCE((SELECT t FROM grp_floor f WHERE f.grp = m.grp), 0)
		FROM grp_member m WHERE m.grp = ? ORDER BY m.jid`, grp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		var in, outT, snap, adminT, floor int64
		if err := rows.Scan(&m.JID, &in, &outT, &snap, &adminT, &m.Admin, &m.Super, &floor); err != nil {
			return nil, err
		}
		// a promotion or demotion says they were there
		pro := max(in, snap, adminT)
		con := outT
		if snap < floor {
			con = max(con, floor)
		}
		m.In = pro > con
		if !m.In {
			m.Admin, m.Super = false, false
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Blocked is every address the newest evidence says is blocked: a change
// newer than the last full list, or the last full list itself.
func (r *Reader) Blocked(ctx context.Context) ([]string, error) {
	return strs(ctx, r.db, `SELECT jid FROM block WHERE blocked = 1
		AND (snap_t >= COALESCE((SELECT t FROM block_floor), 0) OR t > COALESCE((SELECT t FROM block_floor), 0))
		ORDER BY jid`)
}

// Privacy is every privacy setting whatsapp told us, by its name.
func (r *Reader) Privacy(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name, value FROM privacy`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, rows.Err()
}
