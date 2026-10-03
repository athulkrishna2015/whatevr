package model

import (
	"context"
	"sort"
	"strconv"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// factsQuery is every fact table for the ids in ?1, as (what, chat, id, by,
// alt, a, b, t, blob, phone) rows. a revoke is here once proven.
const factsQuery = `WITH ids(id) AS (SELECT value FROM json_each(?1))
	SELECT 'reaction', chat, target, sender, '', emoji, '', t, NULL, 0 FROM f_reaction WHERE target IN ids
	UNION ALL SELECT 'sealed', chat, target, sender, '', use, '', t, plain, 0 FROM f_enc WHERE target IN ids AND plain IS NOT NULL
	UNION ALL SELECT 'edit', chat, target, by, by_alt, '', '', t, body, 0 FROM f_edit WHERE target IN ids
	UNION ALL SELECT 'revoke', chat, target, by, by_alt, '', '', t, NULL, 0 FROM f_revoke WHERE target IN ids AND ok = 1
	UNION ALL SELECT 'pin', chat, target, by, by_alt, CAST(pinned AS TEXT), CAST(secs AS TEXT), t, NULL, phone FROM f_pin WHERE target IN ids
	UNION ALL SELECT 'keep', chat, target, by, by_alt, CAST(keep AS TEXT), '', t, NULL, phone FROM f_keep WHERE target IN ids
	UNION ALL SELECT 'star', a, b, '', '', CAST(on_ AS TEXT), '', 0, NULL, 0 FROM appstate WHERE kind = 'star' AND op = 'set' AND b IN ids
	UNION ALL SELECT 'receipt', chat, id, who, '', type, '', t, NULL, 0 FROM f_receipt WHERE id IN ids`

type factRow struct {
	what, chat, id, by, alt, a, b string
	t                             int64
	blob                          []byte
	phone                         bool
}

// facts attaches what was said about each message, all of it in one query:
// each statement costs more than the rows it reads. a fact counts only for a
// message in its own chat, and an edit, pin or keep only from someone who
// may make it.
func (r *Reader) facts(ctx context.Context, ms []Message) error {
	if len(ms) == 0 {
		return nil
	}
	idx := map[string][]int{}
	ids := make([]string, 0, len(ms))
	for i, m := range ms {
		if _, ok := idx[m.ID]; !ok {
			ids = append(ids, m.ID)
		}
		idx[m.ID] = append(idx[m.ID], i)
	}
	rows, err := r.db.QueryContext(ctx, factsQuery, jsonList(ids))
	if err != nil {
		return err
	}
	var fs []factRow
	for rows.Next() {
		var f factRow
		if err := rows.Scan(&f.what, &f.chat, &f.id, &f.by, &f.alt, &f.a, &f.b, &f.t, &f.blob, &f.phone); err != nil {
			rows.Close()
			return err
		}
		fs = append(fs, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	ck := &checker{ctx: ctx, r: r, links: map[[2]string]bool{}}
	// a reaction may come plain or sealed; per sender the newest wins
	type rkey struct {
		i      int
		sender string
	}
	reactions := map[rkey]Reaction{}
	react := func(i int, x Reaction) {
		k := rkey{i, x.Sender}
		if old, ok := reactions[k]; !ok || x.T > old.T {
			reactions[k] = x
		}
	}
	keeps := map[int][]keepEvent{}
	type receipt struct {
		i int
		x Receipt
	}
	var receipts []receipt
	for _, f := range fs {
		for _, i := range idx[f.id] {
			m := &ms[i]
			if ok, err := ck.chatMatch(f.chat, m.Chat); err != nil || !ok {
				if err != nil {
					return err
				}
				continue
			}
			x := &m.Facts
			switch f.what {
			case "reaction":
				react(i, Reaction{Sender: f.by, Emoji: f.a, T: f.t})
			case "sealed":
				s := Sealed{Sender: f.by, T: f.t, Plain: f.blob}
				switch f.a {
				case useVote:
					x.Votes = append(x.Votes, s)
				case useEvent:
					x.Events = append(x.Events, s)
				case useReaction:
					var rm waE2E.ReactionMessage
					if proto.Unmarshal(f.blob, &rm) == nil {
						react(i, Reaction{Sender: f.by, Emoji: rm.GetText(), T: f.t})
					}
				}
			case "edit":
				if f.t <= x.EditT {
					continue
				}
				if ok, err := ck.author(f, m); err != nil || !ok {
					if err != nil {
						return err
					}
					continue
				}
				var e waE2E.Message
				if proto.Unmarshal(f.blob, &e) == nil {
					x.Edit, x.EditT = &e, f.t
				}
			case "revoke":
				if !x.Revoked || f.t < x.RevokeT {
					x.Revoked, x.RevokeBy, x.RevokeT = true, f.by, f.t
				}
			case "pin":
				if f.t < x.PinT {
					continue
				}
				if ok, err := ck.mayPin(f, m); err != nil || !ok {
					if err != nil {
						return err
					}
					continue
				}
				secs, _ := strconv.ParseInt(f.b, 10, 64)
				if secs <= 0 {
					secs = defaultPinSecs
				}
				x.Pinned, x.PinT, x.PinEnd = f.a == "1", f.t, f.t+secs*1000
			case "keep":
				party, err := ck.party(f, m)
				if err != nil {
					return err
				}
				if !party {
					continue
				}
				author, err := ck.author(f, m)
				if err != nil {
					return err
				}
				keeps[i] = append(keeps[i], keepEvent{t: f.t, keep: f.a == "1", author: author || f.phone})
			case "star":
				x.Starred = x.Starred || f.a == "1"
			case "receipt":
				receipts = append(receipts, receipt{i, Receipt{Who: f.by, Type: f.a, T: f.t}})
			}
		}
	}
	keys := make([]rkey, 0, len(reactions))
	for k := range reactions {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := reactions[keys[i]], reactions[keys[j]]
		if a.T != b.T {
			return a.T < b.T
		}
		return keys[i].sender < keys[j].sender
	})
	for _, k := range keys {
		if x := reactions[k]; x.Emoji != "" {
			ms[k.i].Facts.Reactions = append(ms[k.i].Facts.Reactions, x)
		}
	}
	for i, es := range keeps {
		ms[i].Facts.Kept = kept(es)
	}
	sort.Slice(receipts, func(i, j int) bool {
		a, b := receipts[i].x, receipts[j].x
		if a.T != b.T {
			return a.T < b.T
		}
		if a.Who != b.Who {
			return a.Who < b.Who
		}
		return a.Type < b.Type
	})
	for _, r := range receipts {
		ms[r.i].Facts.Receipts = append(ms[r.i].Facts.Receipts, r.x)
	}
	return nil
}

type keepEvent struct {
	t      int64
	keep   bool
	author bool
}

// kept plays a message's keeps in time order: anyone may keep it, only its
// author may unkeep it, and after the author unkeeps nobody else may keep it
// until the author does.
func kept(es []keepEvent) bool {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].t != es[j].t {
			return es[i].t < es[j].t
		}
		// at one instant the author's word is the last
		return !es[i].author && es[j].author
	})
	on, barred := false, false
	for _, e := range es {
		switch {
		case e.author:
			on, barred = e.keep, !e.keep
		case e.keep && !barred:
			on = true
		}
	}
	return on
}

// checker answers who may say what about a message, remembering the
// addresses it already joined.
type checker struct {
	ctx   context.Context
	r     *Reader
	links map[[2]string]bool
}

// chatMatch is chatMatch with the checker's memory.
func (c *checker) chatMatch(a, b string) (bool, error) {
	if a == b {
		return true, nil
	}
	if !isPerson(a) || !isPerson(b) {
		return false, nil
	}
	return c.linked(a, b)
}

func (c *checker) linked(a, b string) (bool, error) {
	k := [2]string{a, b}
	if b < a {
		k = [2]string{b, a}
	}
	if ok, seen := c.links[k]; seen {
		return ok, nil
	}
	ok, err := linked(c.ctx, c.r.db, a, b)
	if err == nil {
		c.links[k] = ok
	}
	return ok, err
}

func (c *checker) anyLinked(xs, ys []string) (bool, error) {
	for _, x := range xs {
		for _, y := range ys {
			if ok, err := c.linked(x, y); err != nil || ok {
				return ok, err
			}
		}
	}
	return false, nil
}

// author says f came from the person who wrote m.
func (c *checker) author(f factRow, m *Message) (bool, error) {
	if f.by == Me || m.FromMe {
		return f.by == Me && m.FromMe, nil
	}
	return c.anyLinked(nonEmpty(f.by, f.alt), nonEmpty(m.Sender, m.SenderAlt))
}

// party says f came from someone in m's chat: in a group anyone the server
// let send there, in a chat with a person that person or us.
func (c *checker) party(f factRow, m *Message) (bool, error) {
	if f.phone || f.by == Me || IsGroup(m.Chat) {
		return true, nil
	}
	return c.anyLinked(nonEmpty(f.by, f.alt), []string{m.Chat})
}

// mayPin says f may pin or unpin m: either side of a chat with a person, and
// in a group anyone, unless the group lets only admins edit its settings.
// where our group data has no answer for that time, the pin stands.
func (c *checker) mayPin(f factRow, m *Message) (bool, error) {
	if f.phone || f.by == Me {
		return true, nil
	}
	if !IsGroup(m.Chat) {
		return c.party(f, m)
	}
	var value string
	var at int64
	err := c.r.db.QueryRowContext(c.ctx, `SELECT value, t FROM grp_field WHERE grp = ? AND field = 'locked'`, m.Chat).Scan(&value, &at)
	if isNoRows(err) || err == nil && (at > f.t || value != "1") {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	admin, known, err := adminAt(c.ctx, c.r.db, m.Chat, c.addrs(f.by, f.alt), f.t)
	return admin || !known, err
}

// addrs is a and b with every address id_map joins to either.
func (c *checker) addrs(a, b string) []string {
	out := nonEmpty(a, b)
	for _, x := range nonEmpty(a, b) {
		rows, err := c.r.db.QueryContext(c.ctx, `SELECT pn FROM id_map WHERE lid = ? UNION SELECT lid FROM id_map WHERE pn = ?`, x, x)
		if err != nil {
			continue
		}
		for rows.Next() {
			var y string
			if rows.Scan(&y) == nil {
				out = append(out, y)
			}
		}
		rows.Close()
	}
	return out
}
