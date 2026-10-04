package model

import (
	"bytes"
	"crypto/sha256"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// history keeps what the phone said about each chat when it sent history,
// and how far each history blob got.
//
// a chat's history row is a snapshot of the phone at one moment; app state
// overrides it wherever app state has a row, since app state is the live
// truth and history only what was true when the blob was cut.
var historyDomain = core.Domain{
	Name:    "history",
	Version: 1,
	Tables:  []string{"hist_chat", "hist_blob", "hist_note"},
	Schema: []string{
		`CREATE TABLE hist_chat (
			chat           TEXT PRIMARY KEY,
			t              INTEGER NOT NULL,
			hash           BLOB NOT NULL,
			name           TEXT NOT NULL DEFAULT '',
			archived       INTEGER NOT NULL DEFAULT 0,
			pinned         INTEGER NOT NULL DEFAULT 0,
			mute_end       INTEGER NOT NULL DEFAULT 0,
			unread         INTEGER NOT NULL DEFAULT 0,
			marked_unread  INTEGER NOT NULL DEFAULT 0,
			ephemeral      INTEGER NOT NULL DEFAULT 0,
			read_only      INTEGER NOT NULL DEFAULT 0,
			ended          INTEGER NOT NULL DEFAULT 0,
			end_type       INTEGER NOT NULL DEFAULT -1,
			seq            INTEGER NOT NULL
		)`,
		// one row per downloaded (or given up on) blob
		`CREATE TABLE hist_blob (
			notification  TEXT PRIMARY KEY,
			sync_type     TEXT NOT NULL,
			chunk         INTEGER NOT NULL,
			progress      INTEGER NOT NULL,
			conversations INTEGER NOT NULL,
			error         TEXT NOT NULL DEFAULT '',
			at            INTEGER NOT NULL
		)`,
		`CREATE TABLE hist_note (
			notification TEXT PRIMARY KEY,
			sync_type    TEXT NOT NULL,
			chunk        INTEGER NOT NULL,
			progress     INTEGER NOT NULL,
			seq          INTEGER NOT NULL,
			at           INTEGER NOT NULL
		)`,
	},
	Folds: map[string]core.FoldFunc{
		core.KindHistoryConversation: foldHistoryChat,
		core.KindHistoryExtra:        foldHistoryExtra,
		core.KindHistoryNotification: foldHistoryNote,
	},
}

// conversationMeta is the conversation without its messages, which can be
// megabytes the chat row has no use for.
func conversationMeta(body []byte) (*waHistorySync.Conversation, []byte, error) {
	var meta []byte
	b := body
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, nil, protowire.ParseError(n)
		}
		m := protowire.ConsumeFieldValue(num, typ, b[n:])
		if m < 0 {
			return nil, nil, protowire.ParseError(m)
		}
		if num != 2 {
			meta = append(meta, b[:n+m]...)
		}
		b = b[n+m:]
	}
	var c waHistorySync.Conversation
	if err := proto.Unmarshal(meta, &c); err != nil {
		return nil, nil, err
	}
	return &c, meta, nil
}

func foldHistoryChat(tx *core.Tx, in core.Input) error {
	if h, err := head[core.HistoryConversationHead](in); err != nil || h.Offset > 0 {
		return err
	}
	c, meta, err := conversationMeta(in.Body)
	if err != nil {
		return err
	}
	chat := user(c.GetID())
	if chat == "" {
		return nil
	}
	t := int64(c.GetConversationTimestamp())
	if t == 0 {
		t = int64(c.GetLastMsgTimestamp())
	}
	t *= 1000
	sum := sha256.Sum256(meta)
	var oldT int64
	var oldHash []byte
	switch err := tx.QueryRow(`SELECT t, hash FROM hist_chat WHERE chat = ?`, chat).Scan(&oldT, &oldHash); {
	case err == nil:
		if t < oldT || t == oldT && bytes.Compare(sum[:], oldHash) <= 0 {
			return nil
		}
	case !isNoRows(err):
		return err
	}
	name := c.GetName()
	if name == "" {
		name = c.GetDisplayName()
	}
	if _, err := tx.Exec(`INSERT INTO hist_chat (chat, t, hash, name, archived, pinned, mute_end, unread, marked_unread,
			ephemeral, read_only, ended, end_type, seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat) DO UPDATE SET t = excluded.t, hash = excluded.hash, name = excluded.name,
			archived = excluded.archived, pinned = excluded.pinned, mute_end = excluded.mute_end,
			unread = excluded.unread, marked_unread = excluded.marked_unread, ephemeral = excluded.ephemeral,
			read_only = excluded.read_only, ended = excluded.ended, end_type = excluded.end_type, seq = excluded.seq`,
		chat, t, sum[:], name, c.GetArchived(), c.GetPinned(), int64(c.GetMuteEndTime()), c.GetUnreadCount(),
		c.GetMarkedAsUnread(), c.GetEphemeralExpiration(), c.GetReadOnly(), c.GetEndOfHistoryTransfer(), endType(c), in.Seq); err != nil {
		return err
	}
	tx.Touch("chat", chat)
	return nil
}

func foldHistoryExtra(tx *core.Tx, in core.Input) error {
	h, err := head[core.HistoryExtraHead](in)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO hist_blob (notification, sync_type, chunk, progress, conversations, error, at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (notification) DO UPDATE SET error = MIN(hist_blob.error, excluded.error),
			at = MIN(hist_blob.at, excluded.at)`,
		h.Notification, h.SyncType, h.ChunkOrder, h.Progress, h.Conversations, h.Error, in.At.UnixMilli()); err != nil {
		return err
	}
	tx.Touch("sync", h.SyncType)
	return nil
}

func foldHistoryNote(tx *core.Tx, in core.Input) error {
	h, err := head[core.HistoryNotificationHead](in)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO hist_note (notification, sync_type, chunk, progress, seq, at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (notification) DO UPDATE SET seq = MIN(hist_note.seq, excluded.seq), at = MIN(hist_note.at, excluded.at)`,
		h.ID, h.SyncType, h.ChunkOrder, h.Progress, in.Seq, in.At.UnixMilli()); err != nil {
		return err
	}
	tx.Touch("sync", h.SyncType)
	return nil
}

func endType(c *waHistorySync.Conversation) int {
	if c.EndOfHistoryTransferType == nil {
		return -1
	}
	return int(c.GetEndOfHistoryTransferType())
}
