package model

import (
	"context"
	"database/sql"

	"whatevrd/internal/core"
)

// a delete for everyone is end to end encrypted: the server sees that a
// stanza revokes something, never what. so any member can name anyone's
// message, and only the author, or an admin in a group, may. a revoke waits
// in f_revoke (ok 0) until it is proven, then the message is scrubbed down to
// a tombstone, which a rebuild makes again from msg_src and the revoke.

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// linked says a and b are one person's addresses, or the same address.
func linked(ctx context.Context, q rowQuerier, a, b string) (bool, error) {
	if a == "" || b == "" {
		return false, nil
	}
	if a == b {
		return true, nil
	}
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM id_map WHERE lid = ? AND pn = ? OR lid = ? AND pn = ? LIMIT 1`, a, b, b, a).Scan(&one)
	if isNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

// anyLinked is linked over every pair of xs and ys.
func anyLinked(ctx context.Context, q rowQuerier, xs, ys []string) (bool, error) {
	for _, x := range xs {
		for _, y := range ys {
			if ok, err := linked(ctx, q, x, y); err != nil || ok {
				return ok, err
			}
		}
	}
	return false, nil
}

// chatMatch says a fact said in chat a can be about a message in chat b: one
// group, or two addresses of one person.
func chatMatch(ctx context.Context, q rowQuerier, a, b string) (bool, error) {
	if a == b {
		return true, nil
	}
	if !isPerson(a) || !isPerson(b) {
		return false, nil
	}
	return linked(ctx, q, a, b)
}

// adminAt says whether one of addrs was an admin of grp at t, and whether
// what we know of the group reaches back to t. a member row keeps only its
// newest admin change, so a change after t hides what came before it.
func adminAt(ctx context.Context, q rowQuerier, grp string, addrs []string, t int64) (admin, known bool, err error) {
	later := false
	for _, a := range addrs {
		if a == "" {
			continue
		}
		var on bool
		var at int64
		err := q.QueryRowContext(ctx, `SELECT admin, admin_t FROM grp_member WHERE grp = ? AND jid = ?`, grp, a).Scan(&on, &at)
		if isNoRows(err) {
			continue
		}
		if err != nil {
			return false, false, err
		}
		switch {
		case at == 0:
		case at <= t:
			if on {
				return true, true, nil
			}
			known = true
		default:
			later = true
		}
	}
	if known || later {
		return false, known && !later, nil
	}
	// nobody we know of: a fetch at or before t that did not list them as an
	// admin says they were not, a later one says nothing about t
	var floor int64
	switch err := q.QueryRowContext(ctx, `SELECT t FROM grp_floor WHERE grp = ?`, grp).Scan(&floor); {
	case isNoRows(err):
		return false, false, nil
	case err != nil:
		return false, false, err
	}
	return false, floor <= t, nil
}

// groupSeen says the group was fetched, or a fetch said why it cannot be:
// before that, what we know of its admins is nothing yet, not an answer.
func groupSeen(ctx context.Context, q rowQuerier, grp string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM grp_floor WHERE grp = ? UNION ALL SELECT 1 FROM grp_error WHERE grp = ? LIMIT 1`, grp, grp).Scan(&one)
	if isNoRows(err) {
		return false, nil
	}
	return err == nil, err
}

// addrsOf is a and b with every address id_map joins to either.
func addrsOf(ctx context.Context, tx *core.Tx, a, b string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, x := range []string{a, b} {
		if x == "" || x == Me {
			continue
		}
		for _, y := range aliases(tx, x) {
			if !seen[y] {
				seen[y] = true
				out = append(out, y)
			}
		}
	}
	return out
}

type author struct{ chat, sender, alt string }

// authors is who wrote chat/id, from every copy of it here: bodies, scrubbed
// bodies and placeholders for one that did not decrypt.
func authors(tx *core.Tx, chat, id string) ([]author, error) {
	rows, err := tx.Query(`SELECT chat, sender, alt FROM msg_src WHERE id = ? AND sender != '' AND `+sameChatSQL("chat")+`
		UNION SELECT chat, sender, '' FROM msg_wait WHERE id = ? AND `+sameChatSQL("chat"),
		id, chat, chat, chat, id, chat, chat, chat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []author
	for rows.Next() {
		var a author
		if err := rows.Scan(&a.chat, &a.sender, &a.alt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type revoke struct {
	chat, by, alt string
	t             int64
}

// revokeValid says r may delete a message the authors wrote.
func revokeValid(tx *core.Tx, r revoke, as []author) (bool, error) {
	ctx := tx.Context()
	for _, a := range as {
		if ok, err := chatMatch(ctx, tx, r.chat, a.chat); err != nil || !ok {
			if err != nil {
				return false, err
			}
			continue
		}
		if r.by == Me {
			// another device of ours: ours to delete, or ours as an admin
			if a.sender == Me || IsGroup(a.chat) {
				return true, nil
			}
			continue
		}
		if a.sender != Me {
			if ok, err := anyLinked(ctx, tx, nonEmpty(r.by, r.alt), nonEmpty(a.sender, a.alt)); err != nil || ok {
				return ok, err
			}
		}
		if IsGroup(a.chat) && r.chat == a.chat {
			admin, known, err := adminAt(ctx, tx, a.chat, addrsOf(ctx, tx, r.by, r.alt), r.t)
			if err != nil {
				return false, err
			}
			if admin {
				return true, nil
			}
			if !known {
				// a scrub is for good, so it waits for the group's first fetch;
				// where that has no answer for this time either, the server
				// checked the admin revoke it carried
				seen, err := groupSeen(ctx, tx, a.chat)
				if err != nil || seen {
					return seen, err
				}
			}
		}
	}
	return false, nil
}

// checkRevokes proves what revokes of chat/id it can and, once one holds,
// scrubs the message to its tombstone. it says whether the message is one.
func checkRevokes(tx *core.Tx, chat, id string) (bool, error) {
	rows, err := tx.Query(`SELECT chat, by, by_alt, t, ok FROM f_revoke WHERE target = ? AND `+sameChatSQL("chat"), id, chat, chat, chat)
	if err != nil {
		return false, err
	}
	var pending []revoke
	done := false
	for rows.Next() {
		var r revoke
		var ok bool
		if err := rows.Scan(&r.chat, &r.by, &r.alt, &r.t, &ok); err != nil {
			rows.Close()
			return false, err
		}
		if ok {
			done = true
		} else {
			pending = append(pending, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(pending) > 0 {
		as, err := authors(tx, chat, id)
		if err != nil {
			return false, err
		}
		for _, r := range pending {
			ok, err := revokeValid(tx, r, as)
			if err != nil {
				return false, err
			}
			if !ok {
				continue
			}
			if _, err := tx.Exec(`UPDATE f_revoke SET ok = 1 WHERE chat = ? AND target = ? AND by = ?`, r.chat, id, r.by); err != nil {
				return false, err
			}
			done = true
		}
	}
	if !done {
		return false, nil
	}
	return true, tombstone(tx, chat, id)
}

// tombstone scrubs a message deleted for everyone: every body it came in and
// every edit of it is blanked in the log, what was said about it goes, and a
// row stays with who sent it and when.
func tombstone(tx *core.Tx, chat, id string) error {
	rows, err := tx.Query(`SELECT chat, sender, alt, t, seq, off, len FROM msg_src WHERE id = ? AND `+sameChatSQL("chat")+`
		ORDER BY chat, seq, off`, id, chat, chat, chat)
	if err != nil {
		return err
	}
	type src struct {
		chat, sender, alt string
		t, seq            int64
		off, len          int
	}
	var srcs []src
	for rows.Next() {
		var s src
		if err := rows.Scan(&s.chat, &s.sender, &s.alt, &s.t, &s.seq, &s.off, &s.len); err != nil {
			rows.Close()
			return err
		}
		srcs = append(srcs, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	type stone struct {
		sender, alt string
		t, seq      int64
	}
	stones := map[string]*stone{}
	for _, s := range srcs {
		if err := scrubBody(tx, s.seq, s.off, s.len); err != nil {
			return err
		}
		if s.sender == "" {
			continue
		}
		if x := stones[s.chat]; x == nil {
			stones[s.chat] = &stone{s.sender, s.alt, s.t, s.seq}
		} else {
			x.t, x.seq = min(x.t, s.t), max(x.seq, s.seq)
			if x.alt == "" {
				x.alt = s.alt
			}
		}
	}
	waits, err := tx.Query(`SELECT chat, sender, t FROM msg_wait WHERE id = ? AND `+sameChatSQL("chat"), id, chat, chat, chat)
	if err != nil {
		return err
	}
	for waits.Next() {
		var c, sender string
		var t int64
		if err := waits.Scan(&c, &sender, &t); err != nil {
			waits.Close()
			return err
		}
		if x := stones[c]; x == nil {
			stones[c] = &stone{sender: sender, t: t}
		} else {
			x.t = min(x.t, t)
		}
	}
	waits.Close()
	if _, err := tx.Exec(`DELETE FROM msg_wait WHERE id = ? AND `+sameChatSQL("chat"), id, chat, chat, chat); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM msg_local WHERE id = ? AND `+sameChatSQL("chat"), id, chat, chat, chat); err != nil {
		return err
	}
	edits, err := tx.Query(`SELECT seq FROM f_edit WHERE target = ? AND `+sameChatSQL("chat"), id, chat, chat, chat)
	if err != nil {
		return err
	}
	var seqs []int64
	for edits.Next() {
		var s int64
		if edits.Scan(&s) == nil && s > 0 {
			seqs = append(seqs, s)
		}
	}
	edits.Close()
	for _, s := range seqs {
		if err := scrubBody(tx, s, -1, -1); err != nil {
			return err
		}
	}
	for _, t := range []string{"f_reaction", "f_edit", "f_enc", "f_pin", "f_keep"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE target = ? AND `+sameChatSQL("chat"), id, chat, chat, chat); err != nil {
			return err
		}
	}
	for c, s := range stones {
		if _, err := tx.Exec(`INSERT INTO msg (chat, id, sender, sender_alt, from_me, t, kind, src, hash, seq)
			VALUES (?, ?, ?, ?, ?, ?, 'revoked', ?, x'', ?)
			ON CONFLICT (chat, id) DO UPDATE SET sender = excluded.sender, sender_alt = excluded.sender_alt,
				from_me = excluded.from_me, t = excluded.t, kind = 'revoked', text = '', reply = '', album = '', secret = NULL,
				status = 0, view_once = 0, src = excluded.src, hash = x'', seq = excluded.seq, off = NULL, len = NULL, body = NULL`,
			c, id, s.sender, s.alt, s.sender == Me, s.t, srcHistory, s.seq); err != nil {
			return err
		}
		tx.Touch("message", c+":"+id)
		tx.Touch("chat", c)
	}
	return nil
}

// recheckRevokes tries the waiting revokes that what was just learned may
// prove: those by or about one of addrs, or every waiting one in grp.
func recheckRevokes(tx *core.Tx, grp string, addrs ...string) error {
	var rows *sql.Rows
	var err error
	if grp != "" {
		rows, err = tx.Query(`SELECT DISTINCT chat, target FROM f_revoke WHERE ok = 0 AND chat = ?`, grp)
	} else {
		in := jsonList(addrs)
		rows, err = tx.Query(`SELECT DISTINCT r.chat, r.target FROM f_revoke r WHERE r.ok = 0 AND (
			r.by IN (SELECT value FROM json_each(?1)) OR r.by_alt IN (SELECT value FROM json_each(?1))
			OR r.chat IN (SELECT value FROM json_each(?1))
			OR EXISTS (SELECT 1 FROM msg_src s WHERE s.id = r.target AND (s.sender IN (SELECT value FROM json_each(?1))
				OR s.alt IN (SELECT value FROM json_each(?1)))))`, in)
	}
	if err != nil {
		return err
	}
	type key struct{ chat, id string }
	var todo []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.chat, &k.id); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range todo {
		if _, err := checkRevokes(tx, k.chat, k.id); err != nil {
			return err
		}
	}
	return nil
}
