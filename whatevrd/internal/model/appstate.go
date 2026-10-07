package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// app state indexes the reads key on
const (
	asPin          = "pin_v1"
	asArchive      = "archive"
	asMute         = "mute"
	asMarkRead     = "markChatAsRead"
	asStar         = "star"
	asDeleteForMe  = "deleteMessageForMe"
	asDeleteChat   = "deleteChat"
	asClearChat    = "clearChat"
	asContact      = "contact"
	asLIDContact   = "lid_contact"
	asPushName     = "setting_pushName"
	asFavSticker   = "favoriteSticker"
	asRemoveRecent = "removeRecentSticker"
	asLabelEdit    = "label_edit"
	asLabelJID     = "label_jid"
	asLocale       = "setting_locale"
	asUserStatus   = "userStatusMute"
)

// destructive mutations scrubbed something the moment they landed. a
// snapshot that leaves them out cannot bring it back, so the floor keeps them
// and any order of inputs ends the same.
var destructive = map[string]bool{asDeleteForMe: true, asDeleteChat: true, asClearChat: true}

// appstate keeps the newest mutation per index, the highest patch version
// winning. a snapshot at version v is the whole collection at v, so anything
// older than the newest snapshot is gone: the floor.
//
// a few values are lifted into columns for the reads to key on; anything
// else is decoded from the input body by seq.
var appStateDomain = core.Domain{
	Name:    "appstate",
	Version: 1,
	Tables:  []string{"appstate", "appstate_floor"},
	Schema: []string{
		`CREATE TABLE appstate (
			collection TEXT NOT NULL,
			idx        TEXT NOT NULL,
			kind       TEXT NOT NULL,
			a          TEXT NOT NULL DEFAULT '',
			b          TEXT NOT NULL DEFAULT '',
			c          TEXT NOT NULL DEFAULT '',
			d          TEXT NOT NULL DEFAULT '',
			version    INTEGER NOT NULL,
			hash       BLOB NOT NULL,
			op         TEXT NOT NULL,
			seq        INTEGER NOT NULL,
			t          INTEGER NOT NULL DEFAULT 0,
			on_        INTEGER NOT NULL DEFAULT 0,
			n          INTEGER NOT NULL DEFAULT 0,
			s          TEXT NOT NULL DEFAULT '',
			s2         TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (collection, idx)
		)`,
		`CREATE INDEX appstate_kind ON appstate (kind, a)`,
		`CREATE INDEX appstate_msg ON appstate (kind, b)`,
		`CREATE TABLE appstate_floor (
			collection TEXT PRIMARY KEY,
			version    INTEGER NOT NULL
		)`,
	},
	Folds: map[string]core.FoldFunc{
		core.KindAppState:  foldAppState,
		core.KindSyncState: foldAppStateFloor,
	},
}

type asValues struct {
	t, n  int64
	on    bool
	s, s2 string
}

// lift takes the few values reads filter on out of a mutation.
func lift(kind string, v *waSyncAction.SyncActionValue) asValues {
	out := asValues{t: v.GetTimestamp()}
	switch kind {
	case asPin:
		out.on = v.GetPinAction().GetPinned()
	case asArchive:
		out.on = v.GetArchiveChatAction().GetArchived()
		out.n = v.GetArchiveChatAction().GetMessageRange().GetLastMessageTimestamp()
	case asMute:
		out.on = v.GetMuteAction().GetMuted()
		out.n = v.GetMuteAction().GetMuteEndTimestamp()
	case asMarkRead:
		out.on = v.GetMarkChatAsReadAction().GetRead()
		out.n = v.GetMarkChatAsReadAction().GetMessageRange().GetLastMessageTimestamp()
	case asStar:
		out.on = v.GetStarAction().GetStarred()
	case asDeleteForMe:
		out.on = true
		out.n = v.GetDeleteMessageForMeAction().GetMessageTimestamp()
	case asDeleteChat:
		out.on = true
		out.n = v.GetDeleteChatAction().GetMessageRange().GetLastMessageTimestamp()
	case asClearChat:
		out.on = true
		out.n = v.GetClearChatAction().GetMessageRange().GetLastMessageTimestamp()
	case asContact:
		c := v.GetContactAction()
		out.s, out.s2 = c.GetFullName(), c.GetUsername()
		if out.s == "" {
			out.s = c.GetFirstName()
		}
	case asLIDContact:
		c := v.GetLidContactAction()
		out.s, out.s2 = c.GetFullName(), c.GetUsername()
		if out.s == "" {
			out.s = c.GetFirstName()
		}
	case asPushName:
		out.s = v.GetPushNameSetting().GetName()
	case asFavSticker:
		out.on = v.GetStickerAction().GetIsFavorite()
	case asRemoveRecent:
		out.on = true
		out.n = v.GetRemoveRecentStickerAction().GetLastStickerSentTS()
	case asLabelEdit:
		l := v.GetLabelEditAction()
		out.s, out.on, out.n = l.GetName(), l.GetDeleted(), int64(l.GetColor())
	case asLabelJID:
		out.on = v.GetLabelAssociationAction().GetLabeled()
	case asLocale:
		out.s = v.GetLocaleSetting().GetLocale()
	case asUserStatus:
		out.on = v.GetUserStatusMuteAction().GetMuted()
	}
	return out
}

// jid-shaped index parts are stored without their device, the rest as is
func part(index []string, i int) string {
	if i >= len(index) {
		return ""
	}
	if u := user(index[i]); u != "" {
		return u
	}
	return index[i]
}

func foldAppState(tx *core.Tx, in core.Input) error {
	h, err := head[core.AppStateHead](in)
	if err != nil {
		return err
	}
	if len(h.Index) == 0 {
		return nil
	}
	var floor uint64
	if err := tx.QueryRow(`SELECT version FROM appstate_floor WHERE collection = ?`, h.Collection).Scan(&floor); err == nil && h.Version < floor && !destructive[h.Index[0]] {
		return nil
	}
	idx, err := json.Marshal(h.Index)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(append([]byte(h.Op+"\x00"), in.Body...))
	var oldVersion uint64
	var oldHash []byte
	switch err := tx.QueryRow(`SELECT version, hash FROM appstate WHERE collection = ? AND idx = ?`, h.Collection, string(idx)).Scan(&oldVersion, &oldHash); err {
	case nil:
		if h.Version < oldVersion || h.Version == oldVersion && bytes.Compare(sum[:], oldHash) <= 0 {
			return nil
		}
	default:
		if !isNoRows(err) {
			return err
		}
	}
	var vals asValues
	if h.Op == "set" {
		var v waSyncAction.SyncActionValue
		if err := proto.Unmarshal(in.Body, &v); err != nil {
			return err
		}
		vals = lift(h.Index[0], &v)
	}
	if _, err := tx.Exec(`INSERT INTO appstate (collection, idx, kind, a, b, c, d, version, hash, op, seq, t, on_, n, s, s2)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (collection, idx) DO UPDATE SET version = excluded.version, hash = excluded.hash, op = excluded.op,
			seq = excluded.seq, t = excluded.t, on_ = excluded.on_, n = excluded.n, s = excluded.s, s2 = excluded.s2`,
		h.Collection, string(idx), h.Index[0], part(h.Index, 1), part(h.Index, 2), part(h.Index, 3), part(h.Index, 4),
		h.Version, sum[:], h.Op, in.Seq, vals.t, vals.on, vals.n, vals.s, vals.s2); err != nil {
		return err
	}
	return appStateApplied(tx, h.Index[0], part(h.Index, 1), part(h.Index, 2), h.Op == "set", vals)
}

// appStateApplied is what a new winning mutation sets off beyond its row.
func appStateApplied(tx *core.Tx, kind, a, b string, set bool, v asValues) error {
	switch kind {
	case asPin, asArchive, asMute, asMarkRead, asDeleteChat, asClearChat:
		tx.Touch("chat", a)
	case asStar:
		tx.Touch("message", a+":"+b)
	case asContact, asLIDContact:
		tx.Touch("person", a)
	case asPushName:
		tx.Touch("person", "self")
	case asFavSticker, asRemoveRecent:
		tx.Touch("sticker", a)
	default:
		tx.Touch("appstate", kind)
	}
	if !set {
		return nil
	}
	switch kind {
	case asDeleteForMe:
		return scrubMessage(tx, a, b)
	case asDeleteChat, asClearChat:
		return scrubChat(tx, a)
	}
	return nil
}

func foldAppStateFloor(tx *core.Tx, in core.Input) error {
	h, err := head[core.SyncStateHead](in)
	if err != nil {
		return err
	}
	const prefix = "app_state:"
	if !h.Snapshot || len(h.Domain) <= len(prefix) || h.Domain[:len(prefix)] != prefix {
		return nil
	}
	collection := h.Domain[len(prefix):]
	res, err := tx.Exec(`INSERT INTO appstate_floor (collection, version) VALUES (?, ?)
		ON CONFLICT (collection) DO UPDATE SET version = excluded.version WHERE excluded.version > appstate_floor.version`,
		collection, h.Version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM appstate WHERE collection = ? AND version < ?
		AND kind NOT IN (?, ?, ?)`, collection, h.Version, asDeleteForMe, asDeleteChat, asClearChat); err != nil {
		return err
	}
	tx.TouchAll("chat")
	tx.TouchAll("message")
	tx.TouchAll("person")
	return nil
}
