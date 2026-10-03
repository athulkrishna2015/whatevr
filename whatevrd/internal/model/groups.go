package model

import (
	"strconv"

	"whatevrd/internal/core"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// groups keeps each group field newest by its own time: a fetch says how
// things were when it was made, a change notification when it happened.
//
// members are evidence for and against: the newest wins. a fetch is evidence
// for everyone it lists and, through the group's floor, against everyone
// older that it does not list.
var groupsDomain = core.Domain{
	Name:    "groups",
	Version: 2,
	Tables:  []string{"grp_field", "grp_member", "grp_floor", "grp_error"},
	Schema: []string{
		`CREATE TABLE grp_field (
			grp   TEXT NOT NULL,
			field TEXT NOT NULL,
			t     INTEGER NOT NULL,
			value TEXT NOT NULL,
			by    TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (grp, field)
		)`,
		`CREATE TABLE grp_member (
			grp     TEXT NOT NULL,
			jid     TEXT NOT NULL,
			in_t    INTEGER NOT NULL DEFAULT 0,
			out_t   INTEGER NOT NULL DEFAULT 0,
			snap_t  INTEGER NOT NULL DEFAULT 0,
			admin_t INTEGER NOT NULL DEFAULT 0,
			admin   INTEGER NOT NULL DEFAULT 0,
			super   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (grp, jid)
		)`,
		`CREATE INDEX grp_member_jid ON grp_member (jid)`,
		`CREATE TABLE grp_floor (
			grp TEXT PRIMARY KEY,
			t   INTEGER NOT NULL
		)`,
		// the newest refusal, until a newer fetch works
		`CREATE TABLE grp_error (
			grp   TEXT PRIMARY KEY,
			t     INTEGER NOT NULL,
			error TEXT NOT NULL
		)`,
	},
	Folds: map[string]core.FoldFunc{
		core.KindGroupInfo:           foldGroupInfo,
		core.KindHistoryConversation: foldGroupConversation,
	},
}

func setField(tx *core.Tx, grp, field, value, by string, t int64) error {
	_, err := tx.Exec(`INSERT INTO grp_field (grp, field, t, value, by) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (grp, field) DO UPDATE SET t = excluded.t, value = excluded.value, by = excluded.by
		WHERE (excluded.t, excluded.value, excluded.by) > (grp_field.t, grp_field.value, grp_field.by)`, grp, field, t, value, by)
	return err
}

func boolText(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func foldGroupInfo(tx *core.Tx, in core.Input) error {
	h, err := head[core.GroupInfoHead](in)
	if err != nil {
		return err
	}
	grp := user(h.JID)
	if grp == "" {
		return nil
	}
	t := ms(h.T, in)
	tx.Touch("chat", grp)
	tx.Touch("group", grp)
	if h.Error != "" {
		if _, err := tx.Exec(`INSERT INTO grp_error (grp, t, error) VALUES (?, ?, ?)
			ON CONFLICT (grp) DO UPDATE SET t = excluded.t, error = excluded.error
			WHERE (excluded.t, excluded.error) > (grp_error.t, grp_error.error)`, grp, t, h.Error); err != nil {
			return err
		}
		return recheckRevokes(tx, grp)
	}
	if h.Full {
		if _, err := tx.Exec(`DELETE FROM grp_error WHERE grp = ? AND t < ?`, grp, t); err != nil {
			return err
		}
	}
	by := user(h.By)
	fields := []struct {
		name, value string
		t           int64
		set         bool
	}{
		{"name", deref(h.Name), nameT(h.NameT, t), h.Name != nil},
		{"topic", deref(h.Topic), nameT(h.TopicT, t), h.Topic != nil || h.TopicDel},
		{"announce", boolText(derefB(h.Announce)), t, h.Announce != nil},
		{"locked", boolText(derefB(h.Locked)), t, h.Locked != nil},
		{"approval", boolText(derefB(h.Approval)), t, h.Approval != nil},
		{"parent", boolText(h.Parent), t, h.Full},
		{"linked_to", h.LinkedTo, t, h.Full || h.LinkedTo != ""},
		{"deleted", boolText(h.Deleted), t, h.Deleted},
		{"created", itoa(h.Created), 0, h.Created != 0},
		{"owner", user(h.Owner), 0, h.Owner != ""},
		{"addressing", h.Addressed, t, h.Addressed != ""},
	}
	if h.Ephemeral != nil {
		fields = append(fields, struct {
			name, value string
			t           int64
			set         bool
		}{"ephemeral", itoa(int64(*h.Ephemeral)), t, true})
	}
	for _, f := range fields {
		if f.set {
			if err := setField(tx, grp, f.name, f.value, by, f.t); err != nil {
				return err
			}
		}
	}
	if h.Full {
		if err := groupSnapshot(tx, grp, t, h.Participants); err != nil {
			return err
		}
	}
	for _, j := range h.Join {
		if err := member(tx, grp, j, `in_t`, t); err != nil {
			return err
		}
	}
	for _, j := range h.Leave {
		if err := member(tx, grp, j, `out_t`, t); err != nil {
			return err
		}
	}
	for _, j := range h.Promote {
		if err := memberAdmin(tx, grp, j, t, true, false); err != nil {
			return err
		}
	}
	for _, j := range h.Demote {
		if err := memberAdmin(tx, grp, j, t, false, false); err != nil {
			return err
		}
	}
	if h.Full || len(h.Promote) > 0 || len(h.Demote) > 0 {
		// who was an admin when is known further now
		return recheckRevokes(tx, grp)
	}
	return nil
}

func nameT(set, fallback int64) int64 {
	if set > 0 {
		return set * 1000
	}
	return fallback
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefB(b *bool) bool { return b != nil && *b }

func groupSnapshot(tx *core.Tx, grp string, t int64, ps []core.GroupParticipant) error {
	if _, err := tx.Exec(`INSERT INTO grp_floor (grp, t) VALUES (?, ?)
		ON CONFLICT (grp) DO UPDATE SET t = MAX(grp_floor.t, excluded.t)`, grp, t); err != nil {
		return err
	}
	for _, p := range ps {
		j := user(p.JID)
		if j == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO grp_member (grp, jid, snap_t) VALUES (?, ?, ?)
			ON CONFLICT (grp, jid) DO UPDATE SET snap_t = MAX(grp_member.snap_t, excluded.snap_t)`, grp, j, t); err != nil {
			return err
		}
		if err := memberAdmin(tx, grp, j, t, p.Admin || p.Super, p.Super); err != nil {
			return err
		}
	}
	return nil
}

func member(tx *core.Tx, grp, jid, col string, t int64) error {
	jid = user(jid)
	if jid == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO grp_member (grp, jid, `+col+`) VALUES (?, ?, ?)
		ON CONFLICT (grp, jid) DO UPDATE SET `+col+` = MAX(grp_member.`+col+`, excluded.`+col+`)`, grp, jid, t)
	return err
}

func memberAdmin(tx *core.Tx, grp, jid string, t int64, admin, super bool) error {
	jid = user(jid)
	if jid == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO grp_member (grp, jid, admin_t, admin, super) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (grp, jid) DO UPDATE SET admin_t = excluded.admin_t, admin = excluded.admin, super = excluded.super
		WHERE (excluded.admin_t, excluded.admin, excluded.super) > (grp_member.admin_t, grp_member.admin, grp_member.super)`,
		grp, jid, t, admin, super)
	return err
}

// foldGroupConversation takes a group's name from history, older than any
// fetch, as the name to show until one comes.
func foldGroupConversation(tx *core.Tx, in core.Input) error {
	c, _, err := conversationMeta(in.Body)
	if err != nil {
		return err
	}
	grp := user(c.GetID())
	if server(grp) != "g.us" {
		return nil
	}
	name := c.GetName()
	if name == "" {
		return nil
	}
	// time 1: any fetch or change beats it, and two history copies keep the
	// larger name either way
	if err := setField(tx, grp, "name", name, "", 1); err != nil {
		return err
	}
	if c.GetParentGroupID() != "" {
		if err := setField(tx, grp, "linked_to", user(c.GetParentGroupID()), "", 1); err != nil {
			return err
		}
	}
	if c.GetIsParentGroup() {
		if err := setField(tx, grp, "parent", "1", "", 1); err != nil {
			return err
		}
	}
	tx.Touch("chat", grp)
	return nil
}
