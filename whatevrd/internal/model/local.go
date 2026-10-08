package model

import (
	"context"
	"encoding/json"

	"whatevrd/internal/core"
)

// local is what this daemon did on its own: the files it fetched or made for
// a message, the group an invite points at, the pictures it fetched and the
// preferences it was given. only results are logged; when to try again is
// worked out from them.
var localDomain = core.Domain{
	Name:    "local",
	Version: 1,
	Tables:  []string{"msg_local", "avatar_try", "prefs"},
	Schema: []string{
		// value is the whole head; per op the newest wins
		`CREATE TABLE msg_local (
			chat  TEXT NOT NULL,
			id    TEXT NOT NULL,
			op    TEXT NOT NULL,
			t     INTEGER NOT NULL,
			value BLOB NOT NULL,
			PRIMARY KEY (chat, id, op)
		) WITHOUT ROWID`,
		`CREATE INDEX msg_local_id ON msg_local (id)`,
		// every fetch since the newest one that worked
		`CREATE TABLE avatar_try (
			jid     TEXT NOT NULL,
			t       INTEGER NOT NULL,
			status  TEXT NOT NULL,
			picture TEXT NOT NULL DEFAULT '',
			path    TEXT NOT NULL DEFAULT '',
			error   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (jid, t, status)
		) WITHOUT ROWID`,
		`CREATE TABLE prefs (
			one   INTEGER PRIMARY KEY CHECK (one = 1),
			t     INTEGER NOT NULL,
			value BLOB NOT NULL
		)`,
	},
	Folds: map[string]core.FoldFunc{
		core.KindLocal:    foldLocal,
		core.KindAvatar:   foldAvatar,
		core.KindPrefs:    foldPrefs,
		core.KindFavorite: foldFavorite,
	},
}

// foldFavorite records one chat's favorite flag. The table lives outside the
// domain schema (see core setup's always-run statements) so existing
// databases grow it on open instead of needing a rebuild.
func foldFavorite(tx *core.Tx, in core.Input) error {
	h, err := head[core.FavoriteHead](in)
	if err != nil {
		return err
	}
	if h.Chat == "" {
		return nil
	}
	on := 0
	if h.On {
		on = 1
	}
	if _, err := tx.Exec(`INSERT INTO chat_favorite (key, on_flag, t) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET on_flag = excluded.on_flag, t = excluded.t
		WHERE excluded.t >= chat_favorite.t`, h.Chat, on, in.At.UnixMilli()); err != nil {
		return err
	}
	tx.Touch("chat", h.Chat)
	return nil
}

func foldLocal(tx *core.Tx, in core.Input) error {
	h, err := head[core.LocalHead](in)
	if err != nil {
		return err
	}
	if h.Chat == "" || h.ID == "" || h.Op == "" {
		return nil
	}
	if gone, err := targetGone(tx, h.Chat, h.ID); err != nil || gone {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO msg_local (chat, id, op, t, value) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (chat, id, op) DO UPDATE SET t = excluded.t, value = excluded.value
		WHERE (excluded.t, excluded.value) > (msg_local.t, msg_local.value)`,
		h.Chat, h.ID, h.Op, in.At.UnixMilli(), []byte(in.Head)); err != nil {
		return err
	}
	tx.Touch("message", h.Chat+":"+h.ID)
	return nil
}

// dropLocal forgets what was done for a message that is gone.
func dropLocal(tx *core.Tx, chat, id string) error {
	_, err := tx.Exec(`DELETE FROM msg_local WHERE chat = ? AND id = ?`, chat, id)
	return err
}

func foldAvatar(tx *core.Tx, in core.Input) error {
	h, err := head[core.AvatarHead](in)
	if err != nil {
		return err
	}
	j := user(h.JID)
	if j == "" || h.Status == "" {
		return nil
	}
	t := in.At.UnixMilli()
	if _, err := tx.Exec(`INSERT INTO avatar_try (jid, t, status, picture, path, error) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, j, t, h.Status, h.PictureID, h.Path, h.Error); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM avatar_try WHERE jid = ?1 AND t < (SELECT MAX(t) FROM avatar_try
		WHERE jid = ?1 AND status != ?2)`, j, core.AvatarError); err != nil {
		return err
	}
	tx.Touch("person", j)
	tx.Touch("chat", j)
	return nil
}

func foldPrefs(tx *core.Tx, in core.Input) error {
	h, err := head[core.PrefsHead](in)
	if err != nil {
		return err
	}
	if len(h.Prefs) == 0 {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO prefs (one, t, value) VALUES (1, ?, ?)
		ON CONFLICT (one) DO UPDATE SET t = excluded.t, value = excluded.value
		WHERE (excluded.t, excluded.value) > (prefs.t, prefs.value)`, in.At.UnixMilli(), []byte(h.Prefs)); err != nil {
		return err
	}
	tx.Touch("prefs", "")
	return nil
}

// Local is what this daemon did for one message.
type Local struct {
	// File is the downloaded media, or the map of a location
	File       string
	W, H       int32
	FileT      int64
	Error      string
	ErrorT     int64
	Played     bool
	Poster     string
	Waveform   []byte
	Preview    string
	PreviewW   int32
	PreviewH   int32
	DirectPath string
	Invite     json.RawMessage
	InviteErr  string
	// Asked is how many times the phone was asked to resend it
	Asked int
}

// DownloadError is the error of the newest try, "" when the file came after it.
func (l Local) DownloadError() string {
	if l.ErrorT > l.FileT {
		return l.Error
	}
	return ""
}

func (l *Local) add(op string, t int64, value []byte) {
	var h core.LocalHead
	if json.Unmarshal(value, &h) != nil {
		return
	}
	switch op {
	case core.MediaFile:
		l.File, l.W, l.H, l.FileT = h.Path, h.W, h.H, t
	case core.MediaMap:
		// a location's map is its file
		if t >= l.FileT {
			l.File, l.W, l.H, l.FileT = h.Path, h.W, h.H, t
		}
	case core.MediaError:
		l.Error, l.ErrorT = h.Error, t
	case core.MediaPlayed:
		l.Played = true
	case core.MediaPoster:
		l.Poster = h.Path
	case core.MediaWaveform:
		l.Waveform = h.Waveform
	case core.MediaPreview:
		l.Preview, l.PreviewW, l.PreviewH = h.Path, h.W, h.H
	case core.MediaDirect:
		l.DirectPath = h.DirectPath
	case core.LocalInvite:
		l.Invite, l.InviteErr = h.Invite, h.Error
	case core.LocalAsked:
		l.Asked = h.N
	}
}

// Avatar is the picture of an address as far as this daemon fetched it.
type Avatar struct {
	PictureID string
	Path      string
	// Status is the newest fetch that got an answer, "" for none yet
	Status string
	T      int64
	// Fails counts the fetches that failed since, the newest at LastTry
	Fails   int
	LastTry int64
	Error   string
}

// Avatars is the picture of each address.
func (r *Reader) Avatars(ctx context.Context, jids []string) (map[string]Avatar, error) {
	out := make(map[string]Avatar, len(jids))
	if len(jids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT jid, t, status, picture, path, error FROM avatar_try
		WHERE jid IN (SELECT value FROM json_each(?)) ORDER BY jid, t, status`, jsonList(jids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var j, status, pic, path, e string
		var t int64
		if err := rows.Scan(&j, &t, &status, &pic, &path, &e); err != nil {
			return nil, err
		}
		a := out[j]
		if status == core.AvatarError {
			a.Fails++
			a.Error = e
		} else {
			a = Avatar{PictureID: pic, Path: path, Status: status, T: t}
		}
		a.LastTry = t
		out[j] = a
	}
	return out, rows.Err()
}

// Prefs is the preferences as last set, nil for never.
func (r *Reader) Prefs(ctx context.Context) (json.RawMessage, error) {
	var v []byte
	err := r.db.QueryRowContext(ctx, `SELECT value FROM prefs WHERE one = 1`).Scan(&v)
	if isNoRows(err) {
		return nil, nil
	}
	return v, err
}

// AvatarPaths is every picture file an avatar row names.
func (r *Reader) AvatarPaths(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT path FROM avatar_try WHERE path != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}
