package model

import (
	"whatevrd/internal/core"
)

// account holds the facts about the account and the people around it that
// are not messages: blocklist, privacy, pictures, security code changes,
// calls and channels.
var accountDomain = core.Domain{
	Name:    "account",
	Version: 2,
	Tables:  []string{"block", "block_floor", "privacy", "picture", "id_change", "call", "newsletter"},
	Schema: []string{
		// blocklist changes carry no time of their own: receive time orders them
		`CREATE TABLE block (
			jid     TEXT PRIMARY KEY,
			t       INTEGER NOT NULL,
			blocked INTEGER NOT NULL,
			snap_t  INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE block_floor (
			one INTEGER PRIMARY KEY CHECK (one = 1),
			t   INTEGER NOT NULL
		)`,
		`CREATE TABLE privacy (
			name  TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			t     INTEGER NOT NULL
		)`,
		`CREATE TABLE picture (
			jid     TEXT PRIMARY KEY,
			t       INTEGER NOT NULL,
			id      TEXT NOT NULL,
			removed INTEGER NOT NULL,
			author  TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE id_change (
			jid TEXT NOT NULL,
			t   INTEGER NOT NULL,
			PRIMARY KEY (jid, t)
		)`,
		`CREATE TABLE call (
			id      TEXT NOT NULL,
			event   TEXT NOT NULL,
			from_   TEXT NOT NULL,
			t       INTEGER NOT NULL,
			video   INTEGER NOT NULL DEFAULT 0,
			grp     TEXT NOT NULL DEFAULT '',
			reason  TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (id, event)
		)`,
		`CREATE TABLE newsletter (
			jid         TEXT PRIMARY KEY,
			t           INTEGER NOT NULL,
			event        TEXT NOT NULL,
			role        TEXT NOT NULL DEFAULT '',
			mute        TEXT NOT NULL DEFAULT '',
			name        TEXT NOT NULL DEFAULT '',
			followers   INTEGER NOT NULL DEFAULT 0,
			description TEXT NOT NULL DEFAULT '',
			verified    INTEGER NOT NULL DEFAULT 0
		)`,
	},
	Folds: map[string]core.FoldFunc{
		core.KindBlocklist:      foldBlocklist,
		core.KindPrivacy:        foldPrivacy,
		core.KindPicture:        foldPicture,
		core.KindIdentityChange: foldIdentityChange,
		core.KindCall:           foldCall,
		core.KindNewsletter:     foldNewsletter,
	},
}

// BlocklistFull is the action of a blocklist input holding the whole list,
// as a fetch returns it.
const BlocklistFull = "full"

func foldBlocklist(tx *core.Tx, in core.Input) error {
	h, err := head[core.BlocklistHead](in)
	if err != nil {
		return err
	}
	t := in.At.UnixMilli()
	tx.Touch("blocklist", "")
	if h.Action == BlocklistFull {
		if _, err := tx.Exec(`INSERT INTO block_floor (one, t) VALUES (1, ?)
			ON CONFLICT (one) DO UPDATE SET t = MAX(block_floor.t, excluded.t)`, t); err != nil {
			return err
		}
	}
	for _, c := range h.Changes {
		j := user(c.JID)
		if j == "" {
			continue
		}
		blocked := c.Action == "block"
		snap := int64(0)
		if h.Action == BlocklistFull {
			snap = t
		}
		if _, err := tx.Exec(`INSERT INTO block (jid, t, blocked, snap_t) VALUES (?, ?, ?, ?)
			ON CONFLICT (jid) DO UPDATE SET
				blocked = CASE WHEN (excluded.t, excluded.blocked) > (block.t, block.blocked) THEN excluded.blocked ELSE block.blocked END,
				t = MAX(block.t, excluded.t),
				snap_t = MAX(block.snap_t, excluded.snap_t)`, j, t, blocked, snap); err != nil {
			return err
		}
	}
	return nil
}

func foldPrivacy(tx *core.Tx, in core.Input) error {
	h, err := head[core.PrivacyHead](in)
	if err != nil {
		return err
	}
	t := in.At.UnixMilli()
	for name, value := range h.Settings {
		if _, err := tx.Exec(`INSERT INTO privacy (name, value, t) VALUES (?, ?, ?)
			ON CONFLICT (name) DO UPDATE SET value = excluded.value, t = excluded.t
			WHERE (excluded.t, excluded.value) > (privacy.t, privacy.value)`, name, value, t); err != nil {
			return err
		}
	}
	tx.Touch("privacy", "")
	return nil
}

func foldPicture(tx *core.Tx, in core.Input) error {
	h, err := head[core.PictureHead](in)
	if err != nil {
		return err
	}
	j := user(h.JID)
	if j == "" {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO picture (jid, t, id, removed, author) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET t = excluded.t, id = excluded.id, removed = excluded.removed, author = excluded.author
		WHERE (excluded.t, excluded.id, excluded.removed) > (picture.t, picture.id, picture.removed)`,
		j, ms(h.T, in), h.PictureID, h.Remove, user(h.Author)); err != nil {
		return err
	}
	tx.Touch("person", j)
	tx.Touch("chat", j)
	return nil
}

func foldIdentityChange(tx *core.Tx, in core.Input) error {
	h, err := head[core.IdentityChangeHead](in)
	if err != nil {
		return err
	}
	j := user(h.JID)
	if j == "" || h.Implicit {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO id_change (jid, t) VALUES (?, ?) ON CONFLICT DO NOTHING`, j, ms(h.T, in)); err != nil {
		return err
	}
	tx.Touch("chat", j)
	return nil
}

func foldCall(tx *core.Tx, in core.Input) error {
	h, err := head[core.CallHead](in)
	if err != nil {
		return err
	}
	if h.ID == "" {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO call (id, event, from_, t, video, grp, reason) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id, event) DO UPDATE SET t = MIN(call.t, excluded.t)`,
		h.ID, h.Event, user(h.From), ms(h.T, in), h.Video, user(h.Group), h.Reason); err != nil {
		return err
	}
	tx.Touch("call", h.ID)
	return nil
}

func foldNewsletter(tx *core.Tx, in core.Input) error {
	h, err := head[core.NewsletterHead](in)
	if err != nil {
		return err
	}
	j := user(h.JID)
	if j == "" {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO newsletter (jid, t, event, role, mute, name, followers, description, verified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET t = excluded.t, event = excluded.event, role = excluded.role, mute = excluded.mute,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE newsletter.name END,
			followers = CASE WHEN excluded.event = 'directory' THEN excluded.followers ELSE newsletter.followers END,
			description = CASE WHEN excluded.event = 'directory' THEN excluded.description ELSE newsletter.description END,
			verified = CASE WHEN excluded.event = 'directory' THEN excluded.verified ELSE newsletter.verified END
		WHERE (excluded.t, excluded.event) > (newsletter.t, newsletter.event)`,
		j, in.At.UnixMilli(), h.Event, h.Role, h.Mute, h.Name, h.Followers, h.Description, h.Verified); err != nil {
		return err
	}
	tx.Touch("chat", j)
	tx.Touch("channels", "")
	return nil
}
