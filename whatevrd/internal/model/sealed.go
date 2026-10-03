package model

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// what a sealed fact is, named as whatsapp's key derivation names it
const (
	useVote      = "Poll Vote"
	useEvent     = "Event Response"
	useReaction  = "Enc Reaction"
	useEdit      = "Message Edit"
	useEventEdit = "Event Edit"
)

// putEnc stores a fact sealed with its target's message secret, or one that
// came open (plain), and opens it if the target is already here.
func putEnc(tx *core.Tx, f fact, key *waCommon.MessageKey, use string, iv, payload, plain []byte) error {
	target := key.GetID()
	if gone, err := targetGone(tx, f.chat, target); err != nil || gone || target == "" {
		return err
	}
	orig := ""
	switch {
	case key.GetFromMe():
		// the target's sender sealed it, so the sealer's addresses are its
	case isPerson(f.chat):
		orig = user(key.GetRemoteJID())
	default:
		orig = user(key.GetParticipant())
	}
	mods := strings.Join(nonEmpty(f.raw, f.rawAlt), ",")
	sum := sha256.Sum256(bytes.Join([][]byte{iv, payload, plain}, []byte{0}))
	if _, err := tx.Exec(`INSERT INTO f_enc (chat, target, sender, use, t, hash, mod_jids, orig_jid, iv, payload, plain)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat, target, sender, use) DO UPDATE SET t = excluded.t, hash = excluded.hash,
			mod_jids = excluded.mod_jids, orig_jid = excluded.orig_jid, iv = excluded.iv,
			payload = excluded.payload, plain = excluded.plain
		WHERE (excluded.t, excluded.hash) > (f_enc.t, f_enc.hash)`,
		f.chat, target, f.sender, use, f.t, sum[:], mods, orig, iv, payload, plain); err != nil {
		return err
	}
	tx.Touch("message", f.chat+":"+target)
	return openSealed(tx, f.chat, target)
}

// sealedEdit is the new content an opened edit carries, and when it was
// made.
func sealedEdit(plain []byte) (body []byte, ms int64, ok bool) {
	var m waE2E.Message
	if proto.Unmarshal(plain, &m) != nil {
		return nil, 0, false
	}
	p := m.GetProtocolMessage()
	if p == nil {
		p = m.GetEditedMessage().GetMessage().GetProtocolMessage()
	}
	if p.GetEditedMessage() == nil {
		return nil, 0, false
	}
	body, err := marshal.Marshal(p.GetEditedMessage())
	if err != nil {
		return nil, 0, false
	}
	return body, p.GetTimestampMS(), true
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" && s != Me {
			out = append(out, s)
		}
	}
	return out
}

// openSealed opens what is sealed against chat/id now that the secret may
// be here.
func openSealed(tx *core.Tx, chat, id string) error {
	// nearly every message has a secret and almost none has anything
	// sealed against it: look for that first
	rows, err := tx.Query(`SELECT chat, sender, use, mod_jids, orig_jid, iv, payload FROM f_enc
		WHERE target = ? AND plain IS NULL AND payload IS NOT NULL AND `+sameChatSQL("chat"), id, chat, chat, chat)
	if err != nil {
		return err
	}
	type sealed struct {
		chat, sender, use, mods, orig string
		iv, payload                   []byte
	}
	var todo []sealed
	for rows.Next() {
		var s sealed
		if err := rows.Scan(&s.chat, &s.sender, &s.use, &s.mods, &s.orig, &s.iv, &s.payload); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(todo) == 0 {
		return err
	}
	var secret []byte
	var sender, senderAlt string
	err = tx.QueryRow(`SELECT secret, sender, sender_alt FROM msg WHERE id = ? AND secret IS NOT NULL AND `+sameChatSQL("chat")+`
		ORDER BY chat LIMIT 1`, id, chat, chat, chat).Scan(&secret, &sender, &senderAlt)
	if isNoRows(err) || len(secret) == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	for _, s := range todo {
		mods := strings.Split(s.mods, ",")
		origs := nonEmpty(s.orig, sender, senderAlt)
		if s.orig == "" {
			origs = append(mods, origs...)
		}
		plain := unseal(s.use, id, origs, mods, secret, s.iv, s.payload)
		if plain == nil {
			continue
		}
		if _, err := tx.Exec(`UPDATE f_enc SET plain = ? WHERE chat = ? AND target = ? AND sender = ? AND use = ?`,
			plain, s.chat, id, s.sender, s.use); err != nil {
			return err
		}
		tx.Touch("message", s.chat+":"+id)
		if s.use == useEdit {
			var t int64
			if err := tx.QueryRow(`SELECT t FROM f_enc WHERE chat = ? AND target = ? AND sender = ? AND use = ?`,
				s.chat, id, s.sender, s.use).Scan(&t); err != nil {
				return err
			}
			// what opens is a whole message: the edit's protocol message,
			// whose edited message is the new content
			body, ms, ok := sealedEdit(plain)
			if !ok {
				continue
			}
			f := fact{chat: s.chat, sender: s.sender, t: t, seq: -1}
			if err := putEdit(tx, f.at(ms), id, body); err != nil {
				return err
			}
		}
	}
	return nil
}

// unseal tries each pair of addresses the sender and the original sender
// may have used: lid and pn copies of one person derive different keys.
func unseal(use, id string, origs, mods []string, secret, iv, payload []byte) []byte {
	for _, o := range origs {
		for _, m := range mods {
			if o == "" || m == "" {
				continue
			}
			key := hkdfutil.SHA256(secret, nil, []byte(id+o+m+use), 32)
			var ad []byte
			if use == useVote || use == useEvent {
				ad = fmt.Appendf(nil, "%s\x00%s", id, m)
			}
			if plain, err := gcmutil.Decrypt(key, iv, payload, ad); err == nil {
				return plain
			}
		}
	}
	return nil
}

// Vote is a decoded vote: the sha256 of each option picked.
func Vote(plain []byte) [][]byte {
	var v waE2E.PollVoteMessage
	if proto.Unmarshal(plain, &v) != nil {
		return nil
	}
	return v.GetSelectedOptions()
}
