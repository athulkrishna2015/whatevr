package model

import (
	"encoding/json"
	"fmt"
	"math"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// name sources, best first for a person
const (
	NameContact  = "contact"
	NameBusiness = "business"
	NamePush     = "push"
	NameUsername = "username"
	// what the phone's address book said in a history blob, standing in for
	// a contact name until app state brings one
	NameInline = "inline"
)

const (
	tMin = math.MinInt64
	tMax = math.MaxInt64
)

// identity knows which addresses are one human and what each is called.
//
// a person is never stored: it is every lid and pn joined by id_map edges,
// read at query time with the lid as its anchor. a pn mapped to two lids (a
// recycled number) belongs to each from the time that mapping was first seen,
// which id_owner spells out as intervals so a read is one index seek.
var identityDomain = core.Domain{
	Name:    "identity",
	Version: 2,
	Tables:  []string{"id_map", "id_owner", "id_name", "id_self"},
	Schema: []string{
		`CREATE TABLE id_map (
			lid   TEXT NOT NULL,
			pn    TEXT NOT NULL,
			since INTEGER NOT NULL,
			PRIMARY KEY (lid, pn)
		)`,
		`CREATE INDEX id_map_pn ON id_map (pn)`,
		`CREATE TABLE id_owner (
			pn     TEXT NOT NULL,
			from_t INTEGER NOT NULL,
			to_t   INTEGER NOT NULL,
			lid    TEXT NOT NULL,
			PRIMARY KEY (pn, from_t)
		)`,
		`CREATE INDEX id_owner_lid ON id_owner (lid)`,
		// newest (t, name) per address and source
		`CREATE TABLE id_name (
			jid    TEXT NOT NULL,
			source TEXT NOT NULL,
			name   TEXT NOT NULL,
			t      INTEGER NOT NULL,
			PRIMARY KEY (jid, source)
		)`,
		`CREATE TABLE id_self (
			jid  TEXT PRIMARY KEY
		)`,
	},
	Folds: map[string]core.FoldFunc{
		core.KindLIDMapping:          foldLIDMapping,
		core.KindMessage:             foldIdentityMessage,
		core.KindReceipt:             foldIdentitySource[core.ReceiptHead],
		core.KindUndecryptable:       foldIdentitySource[core.UndecryptableHead],
		core.KindPushName:            foldPushName,
		core.KindBusinessName:        foldBusinessName,
		core.KindHistoryConversation: foldIdentityConversation,
		core.KindHistoryExtra:        foldIdentityExtra,
		core.KindGroupInfo:           foldIdentityGroup,
		core.KindAppState:            foldIdentityAppState,
	},
}

func ms(unix int64, in core.Input) int64 {
	if unix == 0 {
		return in.At.UnixMilli()
	}
	return unix * 1000
}

func head[T any](in core.Input) (T, error) {
	var h T
	if err := json.Unmarshal(in.Head, &h); err != nil {
		return h, fmt.Errorf("%s head: %w", in.Kind, err)
	}
	return h, nil
}

// mapLID records that lid and pn are one human, seen at t.
func mapLID(tx *core.Tx, lid, pn string, t int64) error {
	lid, pn = user(lid), user(pn)
	if !isLID(lid) || !isPN(pn) {
		return nil
	}
	var since int64
	err := tx.QueryRow(`SELECT since FROM id_map WHERE lid = ? AND pn = ?`, lid, pn).Scan(&since)
	if err == nil && since <= t {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO id_map (lid, pn, since) VALUES (?, ?, ?)
		ON CONFLICT (lid, pn) DO UPDATE SET since = MIN(since, excluded.since)`, lid, pn, t); err != nil {
		return err
	}
	if err := owners(tx, pn); err != nil {
		return err
	}
	tx.Touch("person", lid)
	tx.Touch("person", pn)
	tx.Touch("chat", lid)
	tx.Touch("chat", pn)
	// a clear under one address now reaches the messages under the other
	cleared, err := exists(tx, `SELECT 1 FROM appstate WHERE kind IN (?, ?) AND a IN (?, ?) LIMIT 1`, asClearChat, asDeleteChat, lid, pn)
	if err != nil || !cleared {
		return err
	}
	return scrubChat(tx, lid)
}

// owners lays pn's lids out in time: each holds it from its first sighting
// until the next one's, the first also holds everything before.
func owners(tx *core.Tx, pn string) error {
	rows, err := tx.Query(`SELECT lid, since FROM id_map WHERE pn = ? ORDER BY since, lid`, pn)
	if err != nil {
		return err
	}
	type m struct {
		lid   string
		since int64
	}
	var ms []m
	for rows.Next() {
		var x m
		if err := rows.Scan(&x.lid, &x.since); err != nil {
			rows.Close()
			return err
		}
		ms = append(ms, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM id_owner WHERE pn = ?`, pn); err != nil {
		return err
	}
	for i, x := range ms {
		from, to := x.since, int64(tMax)
		if i == 0 {
			from = tMin
		}
		if i+1 < len(ms) {
			to = ms[i+1].since
		}
		// one batch stamps all its inputs alike: a lid replaced the moment it
		// came never owned the number
		if from >= to {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO id_owner (pn, from_t, to_t, lid) VALUES (?, ?, ?, ?)`, pn, from, to, x.lid); err != nil {
			return err
		}
	}
	return nil
}

// setName keeps the newest name per address and source; equal times go to
// the larger name so any order lands the same.
func setName(tx *core.Tx, jid, source, name string, t int64) error {
	jid = user(jid)
	if jid == "" || name == "" {
		return nil
	}
	res, err := tx.Exec(`INSERT INTO id_name (jid, source, name, t) VALUES (?, ?, ?, ?)
		ON CONFLICT (jid, source) DO UPDATE SET name = excluded.name, t = excluded.t
		WHERE (excluded.t, excluded.name) > (id_name.t, id_name.name)`, jid, source, name, t)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		tx.Touch("person", jid)
	}
	return nil
}

func foldLIDMapping(tx *core.Tx, in core.Input) error {
	h, err := head[core.LIDMappingHead](in)
	if err != nil {
		return err
	}
	if h.Self {
		for _, j := range []string{h.LID, h.PN} {
			if j = user(j); j != "" {
				if _, err := tx.Exec(`INSERT INTO id_self (jid) VALUES (?) ON CONFLICT DO NOTHING`, j); err != nil {
					return err
				}
			}
		}
		tx.Touch("person", "self")
	}
	return mapLID(tx, h.LID, h.PN, in.At.UnixMilli())
}

// sourcePairs is every lid/pn pair a message source gives away.
func sourcePairs(tx *core.Tx, s core.Source, in core.Input) error {
	t := in.At.UnixMilli()
	if lid, pn, ok := pair(s.Sender, s.SenderAlt); ok {
		if err := mapLID(tx, lid, pn, t); err != nil {
			return err
		}
	}
	if lid, pn, ok := pair(s.Chat, s.RecipientAlt); ok {
		if err := mapLID(tx, lid, pn, t); err != nil {
			return err
		}
	}
	return nil
}

type sourced interface {
	core.ReceiptHead | core.UndecryptableHead
}

func foldIdentitySource[H sourced](tx *core.Tx, in core.Input) error {
	h, err := head[H](in)
	if err != nil {
		return err
	}
	switch h := any(h).(type) {
	case core.ReceiptHead:
		return sourcePairs(tx, h.Source, in)
	case core.UndecryptableHead:
		return sourcePairs(tx, h.Source, in)
	}
	return nil
}

func foldIdentityMessage(tx *core.Tx, in core.Input) error {
	h, err := head[core.MessageHead](in)
	if err != nil {
		return err
	}
	if err := sourcePairs(tx, h.Source, in); err != nil {
		return err
	}
	if h.FromMe {
		return nil
	}
	t := ms(h.T, in)
	if err := setName(tx, h.Sender, NamePush, h.PushName, t); err != nil {
		return err
	}
	if h.SenderAlt != "" {
		if err := setName(tx, h.SenderAlt, NamePush, h.PushName, t); err != nil {
			return err
		}
	}
	return setName(tx, h.Sender, NameBusiness, h.VerifiedName, t)
}

func foldPushName(tx *core.Tx, in core.Input) error {
	h, err := head[core.PushNameHead](in)
	if err != nil {
		return err
	}
	if lid, pn, ok := pair(h.JID, h.JIDAlt); ok {
		if err := mapLID(tx, lid, pn, in.At.UnixMilli()); err != nil {
			return err
		}
	}
	t := ms(h.T, in)
	for _, j := range []string{h.JID, h.JIDAlt} {
		if err := setName(tx, j, NamePush, h.New, t); err != nil {
			return err
		}
	}
	return nil
}

func foldBusinessName(tx *core.Tx, in core.Input) error {
	h, err := head[core.BusinessNameHead](in)
	if err != nil {
		return err
	}
	return setName(tx, h.JID, NameBusiness, h.New, ms(h.T, in))
}

func foldIdentityConversation(tx *core.Tx, in core.Input) error {
	c, _, err := conversationMeta(in.Body)
	if err != nil {
		return err
	}
	t := in.At.UnixMilli()
	if err := mapLID(tx, c.GetLidJID(), c.GetPnJID(), t); err != nil {
		return err
	}
	if lid, pn, ok := pair(c.GetID(), c.GetNewJID()); ok {
		if err := mapLID(tx, lid, pn, t); err != nil {
			return err
		}
	}
	if isPerson(c.GetID()) {
		if err := setName(tx, c.GetID(), NameUsername, c.GetUsername(), t); err != nil {
			return err
		}
	}
	return nil
}

func foldIdentityExtra(tx *core.Tx, in core.Input) error {
	if len(in.Body) == 0 {
		return nil
	}
	var h waHistorySync.HistorySync
	if err := proto.Unmarshal(in.Body, &h); err != nil {
		return err
	}
	t := in.At.UnixMilli()
	for _, m := range h.GetPhoneNumberToLidMappings() {
		if err := mapLID(tx, m.GetLidJID(), m.GetPnJID(), t); err != nil {
			return err
		}
	}
	for _, p := range h.GetPushnames() {
		if err := setName(tx, p.GetID(), NamePush, p.GetPushname(), t); err != nil {
			return err
		}
	}
	for _, c := range h.GetInlineContacts() {
		if err := mapLID(tx, c.GetLidJID(), c.GetPnJID(), t); err != nil {
			return err
		}
		name := c.GetFullName()
		if name == "" {
			name = c.GetFirstName()
		}
		for _, j := range []string{c.GetLidJID(), c.GetPnJID()} {
			if err := setName(tx, j, NameInline, name, t); err != nil {
				return err
			}
			if err := setName(tx, j, NameUsername, c.GetUsername(), t); err != nil {
				return err
			}
		}
	}
	return nil
}

func foldIdentityGroup(tx *core.Tx, in core.Input) error {
	h, err := head[core.GroupInfoHead](in)
	if err != nil {
		return err
	}
	t := in.At.UnixMilli()
	if lid, pn, ok := pair(h.By, h.ByPN); ok {
		if err := mapLID(tx, lid, pn, t); err != nil {
			return err
		}
	}
	for _, p := range h.Participants {
		if err := mapLID(tx, p.LID, p.PN, t); err != nil {
			return err
		}
		if lid, pn, ok := pair(p.JID, p.PN); ok {
			if err := mapLID(tx, lid, pn, t); err != nil {
				return err
			}
		}
	}
	return nil
}

// foldIdentityAppState takes the pairs contact entries give away. their
// names are app state facts, read from the appstate domain.
func foldIdentityAppState(tx *core.Tx, in core.Input) error {
	h, err := head[core.AppStateHead](in)
	if err != nil {
		return err
	}
	if len(h.Index) < 2 || h.Index[0] != "contact" || h.Op != "set" {
		return nil
	}
	var v waSyncAction.SyncActionValue
	if err := proto.Unmarshal(in.Body, &v); err != nil {
		return err
	}
	c := v.GetContactAction()
	t := in.At.UnixMilli()
	if err := mapLID(tx, c.GetLidJID(), h.Index[1], t); err != nil {
		return err
	}
	return mapLID(tx, c.GetLidJID(), c.GetPnJID(), t)
}
