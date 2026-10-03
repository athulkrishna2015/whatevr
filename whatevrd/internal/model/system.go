package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"whatevrd/internal/core"
)

// System is a row a chat writes about itself: who joined, who left, a new
// subject, a changed security code. the types are v1's system payload types.
type System struct {
	Type string
	// Actor did it, "" when the server named nobody (a join by link)
	Actor string
	// Who it was done to
	Who     []string
	Value   string
	On      bool
	Seconds uint32
	Detail  string
}

const sysSchema = `CREATE TABLE sys_msg (
			chat    TEXT NOT NULL,
			id      TEXT NOT NULL,
			t       INTEGER NOT NULL,
			ord     INTEGER NOT NULL,
			type    TEXT NOT NULL,
			actor   TEXT NOT NULL DEFAULT '',
			who     TEXT NOT NULL DEFAULT '',
			value   TEXT NOT NULL DEFAULT '',
			on_     INTEGER NOT NULL DEFAULT 0,
			seconds INTEGER NOT NULL DEFAULT 0,
			detail  TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (chat, id)
		)`

// putSystem stores one system row. its id is its content, so the same event
// told twice is one row.
func putSystem(tx *core.Tx, chat string, t, ord int64, s System) error {
	if chat == "" || s.Type == "" {
		return nil
	}
	who := ""
	if len(s.Who) > 0 {
		b, _ := json.Marshal(s.Who)
		who = string(b)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{chat, s.Type, s.Actor, who, s.Value, fmt.Sprint(s.On), fmt.Sprint(s.Seconds), s.Detail}, "\x00")))
	id := fmt.Sprintf("system-%s-%d-%s", s.Type, t/1000, hex.EncodeToString(sum[:6]))
	if _, err := tx.Exec(`INSERT INTO sys_msg (chat, id, t, ord, type, actor, who, value, on_, seconds, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO UPDATE SET ord = excluded.ord WHERE excluded.ord < sys_msg.ord`,
		chat, id, t, ord, s.Type, s.Actor, who, s.Value, s.On, s.Seconds, s.Detail); err != nil {
		return err
	}
	tx.Touch("message", chat+":"+id)
	tx.Touch("chat", chat)
	return nil
}

// foldGroupSystem is a group notification as the rows it reads as. a fetch
// says what a group is, not what happened to it, and writes none.
func foldGroupSystem(tx *core.Tx, in core.Input) error {
	h, err := head[core.GroupInfoHead](in)
	if err != nil || h.Full || h.Error != "" || h.JID == "" {
		return err
	}
	chat := user(h.JID)
	t := ms(h.T, in)
	actor := user(h.By)
	users := func(js []string) []string {
		out := make([]string, 0, len(js))
		for _, j := range js {
			if u := user(j); u != "" {
				out = append(out, u)
			}
		}
		return out
	}
	var rows []System
	for _, c := range []struct {
		typ string
		who []string
	}{{"group_join", h.Join}, {"group_leave", h.Leave}, {"group_promote", h.Promote}, {"group_demote", h.Demote}} {
		if len(c.who) > 0 {
			rows = append(rows, System{Type: c.typ, Who: users(c.who)})
		}
	}
	if h.Name != nil {
		rows = append(rows, System{Type: "group_name", Value: strings.TrimSpace(*h.Name)})
	}
	if h.Topic != nil {
		v := strings.TrimSpace(*h.Topic)
		if h.TopicDel {
			v = ""
		}
		rows = append(rows, System{Type: "group_topic", Value: v})
	}
	if h.Locked != nil {
		rows = append(rows, System{Type: "group_locked", On: *h.Locked})
	}
	if h.Announce != nil {
		rows = append(rows, System{Type: "group_announce", On: *h.Announce})
	}
	if h.Ephemeral != nil {
		rows = append(rows, System{Type: "ephemeral", On: *h.Ephemeral > 0, Seconds: *h.Ephemeral})
	}
	if h.Approval != nil {
		rows = append(rows, System{Type: "group_approval", On: *h.Approval})
	}
	if h.InviteLink {
		rows = append(rows, System{Type: "group_invite_link"})
	}
	switch h.LinkChange {
	case "link":
		rows = append(rows, System{Type: "group_link", Value: strings.TrimSpace(h.LinkName), Detail: h.LinkType})
	case "unlink":
		rows = append(rows, System{Type: "group_unlink", Value: strings.TrimSpace(h.LinkName), Detail: h.LinkType})
	}
	if h.Deleted {
		rows = append(rows, System{Type: "group_delete", Detail: h.DeleteReason})
	}
	for _, s := range rows {
		s.Actor = actor
		if err := putSystem(tx, chat, t, arrival(in.At, -1), s); err != nil {
			return err
		}
	}
	return nil
}

// foldPictureSystem is a group's photo changing. a person's own picture is
// not something that happened in a chat.
func foldPictureSystem(tx *core.Tx, in core.Input) error {
	h, err := head[core.PictureHead](in)
	if err != nil || !IsGroup(h.JID) {
		return err
	}
	return putSystem(tx, user(h.JID), ms(h.T, in), arrival(in.At, -1), System{Type: "group_photo", Actor: user(h.Author), On: !h.Remove})
}

// foldIdentitySystem is a changed security code, written into the chat with
// that person.
func foldIdentitySystem(tx *core.Tx, in core.Input) error {
	h, err := head[core.IdentityChangeHead](in)
	if err != nil {
		return err
	}
	chat := user(h.JID)
	if chat == "" || IsGroup(chat) {
		return nil
	}
	return putSystem(tx, chat, ms(h.T, in), arrival(in.At, -1), System{Type: "identity_change", Who: []string{chat}})
}

// SystemRow is a system row as a transcript row.
func systemMessage(chat, id string, t, ord int64, s System) Message {
	return Message{Chat: chat, Home: chat, ID: id, T: t, Ord: ord, Kind: "system", Sender: chat, System: &s}
}

const sysCols = `s.chat, s.id, s.t, s.ord, s.type, s.actor, s.who, s.value, s.on_, s.seconds, s.detail`

func scanSystem(row interface{ Scan(...any) error }) (Message, error) {
	var chat, id, who string
	var t, ord int64
	var s System
	if err := row.Scan(&chat, &id, &t, &ord, &s.Type, &s.Actor, &who, &s.Value, &s.On, &s.Seconds, &s.Detail); err != nil {
		return Message{}, err
	}
	if who != "" {
		_ = json.Unmarshal([]byte(who), &s.Who)
	}
	m := systemMessage(chat, id, t, ord, s)
	if s.Actor != "" {
		m.Sender = s.Actor
	}
	return m, nil
}

// systemRows is the system rows of addrs past cmp, as transcript rows.
func (r *Reader) systemRows(ctx context.Context, addrs []string, cmp, order string, args []any, limit int) ([]Message, error) {
	ph := placeholders(len(addrs))
	rows, err := r.db.QueryContext(ctx, `SELECT `+sysCols+` FROM sys_msg s WHERE s.chat IN (`+ph+`) AND `+
		strings.ReplaceAll(cmp, "m.", "s.")+` ORDER BY s.t `+order+`, s.ord `+order+`, s.id `+order+` LIMIT ?`,
		append(append(anys(addrs), args...), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanSystem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Loud is a system row worth reordering the chat list for: one that names
// this account. a changed security code names it too, but fires whenever
// somebody changes phones, so it stays quiet.
func (w *World) Loud(s *System) bool {
	if s == nil || s.Type == "identity_change" {
		return false
	}
	for _, j := range s.Who {
		if w.IsSelf(j) {
			return true
		}
	}
	return false
}

// loudSystem adds the loud system rows of the chats in states to their last
// time, so one that names this account brings its chat up the list.
func (r *Reader) loudSystem(ctx context.Context, w *World, states map[string]*chatState, only []string) error {
	q := `SELECT ` + sysCols + ` FROM sys_msg s WHERE s.who != ''`
	var args []any
	if len(only) > 0 {
		q += ` AND s.chat IN (SELECT value FROM json_each(?))`
		args = []any{jsonList(only)}
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanSystem(rows)
		if err != nil {
			return err
		}
		if !w.Loud(m.System) {
			continue
		}
		s := states[m.Chat]
		if s == nil {
			s = &chatState{as: map[string]asRow{}}
			states[m.Chat] = s
		}
		if m.T > s.last {
			s.last = m.T
		}
	}
	return rows.Err()
}
