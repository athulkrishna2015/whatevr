package model

// who may say what about someone else's message: the server cannot check
// any of it, the target is inside the encryption

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

const (
	caraL  = "100000000003@lid"
	caraPN = "917770000003@s.whatsapp.net"
)

func target(chat, id, participant string) *waCommon.MessageKey {
	k := &waCommon.MessageKey{RemoteJID: proto.String(chat), ID: proto.String(id)}
	if participant != "" {
		k.Participant = proto.String(participant)
	}
	return k
}

func revokeOf(k *waCommon.MessageKey) *waE2E.Message {
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: k}}
}

func editOf(k *waCommon.MessageKey, s string, sec int) *waE2E.Message {
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: k,
		EditedMessage: text(s), TimestampMS: proto.Int64(at(sec).UnixMilli())}}
}

func pinOf(k *waCommon.MessageKey, pin bool, sec int) *waE2E.Message {
	typ := waE2E.PinInChatMessage_UNPIN_FOR_ALL
	if pin {
		typ = waE2E.PinInChatMessage_PIN_FOR_ALL
	}
	return &waE2E.Message{PinInChatMessage: &waE2E.PinInChatMessage{Key: k, Type: typ.Enum(), SenderTimestampMS: proto.Int64(at(sec).UnixMilli())}}
}

func keepOf(k *waCommon.MessageKey, keep bool, sec int) *waE2E.Message {
	typ := waE2E.KeepType_UNDO_KEEP_FOR_ALL
	if keep {
		typ = waE2E.KeepType_KEEP_FOR_ALL
	}
	return &waE2E.Message{KeepInChatMessage: &waE2E.KeepInChatMessage{Key: k, KeepType: typ.Enum(), TimestampMS: proto.Int64(at(sec).UnixMilli())}}
}

// groupOf says who is in grp and who of them is an admin, as a fetch at sec
func groupOf(sec int, admins []string, members ...string) core.Input {
	var ps []core.GroupParticipant
	for _, m := range members {
		p := core.GroupParticipant{JID: m}
		for _, a := range admins {
			p.Admin = p.Admin || a == m
		}
		ps = append(ps, p)
	}
	return in(core.KindGroupInfo, core.GroupInfoHead{JID: grp, Full: true, T: at(sec).Unix(), Participants: ps}, nil, at(sec))
}

// both feeds ins forward and backward into fresh models and hands each to check
func both(t *testing.T, ins []core.Input, check func(t *testing.T, db *core.DB, r *Reader)) {
	t.Helper()
	for name, order := range map[string][]core.Input{"forward": ins, "backward": reversed(ins)} {
		t.Run(name, func(t *testing.T) {
			db := openModel(t)
			feed(t, db, order)
			check(t, db, NewReader(db.Read()))
		})
	}
}

func one(t *testing.T, r *Reader, addrs []string, id string) Message {
	t.Helper()
	m, ok, err := r.Message(context.Background(), addrs, id)
	if err != nil || !ok {
		t.Fatalf("message %s: %v %v", id, ok, err)
	}
	return m
}

// bodyKept says the log still holds what message id said
func bodyKept(t *testing.T, db *core.DB, id string) bool {
	t.Helper()
	var n int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM inputs WHERE kind = ? AND json_extract(head, '$.id') = ? AND body IS NOT NULL`,
		core.KindMessage, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestOnlyTheAuthorOrAnAdminDeletesForEveryone(t *testing.T) {
	dm := []string{ashaL, ashaPN}
	cases := []struct {
		name    string
		ins     []core.Input
		addrs   []string
		id      string
		revoked bool
	}{
		{"a member deletes someone else's message", []core.Input{
			groupOf(5, nil, boL, caraL),
			msgIn("G1", grp, boL, boPN, false, 10, text("mine")),
			msgIn("X1", grp, caraL, caraPN, false, 11, revokeOf(target(grp, "G1", boL))),
		}, []string{grp}, "G1", false},
		{"an admin deletes it", []core.Input{
			groupOf(5, []string{caraL}, boL, caraL),
			msgIn("G1", grp, boL, boPN, false, 10, text("mine")),
			msgIn("X1", grp, caraL, caraPN, false, 11, revokeOf(target(grp, "G1", boL))),
		}, []string{grp}, "G1", true},
		{"a member made admin after the delete", []core.Input{
			groupOf(5, nil, boL, caraL),
			in(core.KindGroupInfo, core.GroupInfoHead{JID: grp, T: at(20).Unix(), Promote: []string{caraL}}, nil, at(20)),
			msgIn("G1", grp, boL, boPN, false, 10, text("mine")),
			msgIn("X1", grp, caraL, caraPN, false, 11, revokeOf(target(grp, "G1", boL))),
		}, []string{grp}, "G1", true},
		{"it waits for the group's first fetch", []core.Input{
			msgIn("G1", grp, boL, boPN, false, 10, text("mine")),
			msgIn("X1", grp, caraL, caraPN, false, 11, revokeOf(target(grp, "G1", boL))),
		}, []string{grp}, "G1", false},
		{"a group we cannot fetch trusts the server", []core.Input{
			in(core.KindGroupInfo, core.GroupInfoHead{JID: grp, T: at(20).Unix(), Error: "not participating"}, nil, at(20)),
			msgIn("G1", grp, boL, boPN, false, 10, text("mine")),
			msgIn("X1", grp, caraL, caraPN, false, 11, revokeOf(target(grp, "G1", boL))),
		}, []string{grp}, "G1", true},
		{"a fetch from after the delete has no answer for it", []core.Input{
			groupOf(20, nil, boL, caraL),
			msgIn("G1", grp, boL, boPN, false, 10, text("mine")),
			msgIn("X1", grp, caraL, caraPN, false, 11, revokeOf(target(grp, "G1", boL))),
		}, []string{grp}, "G1", true},
		{"the author deletes it under its other address", []core.Input{
			in(core.KindLIDMapping, core.LIDMappingHead{LID: boL, PN: boPN}, nil, at(1)),
			groupOf(5, nil, boL, caraL),
			msgIn("G1", grp, boL, "", false, 10, text("mine")),
			msgIn("X1", grp, boPN, "", false, 11, revokeOf(target(grp, "G1", boL))),
		}, []string{grp}, "G1", true},
		{"they delete our message in a chat with them", []core.Input{
			msgIn("D1", ashaPN, mePN, "", true, 10, text("ours")),
			msgIn("X1", ashaL, ashaL, ashaPN, false, 11, revokeOf(target(ashaL, "D1", ""))),
		}, dm, "D1", false},
		{"someone deletes a message of another chat", []core.Input{
			msgIn("D1", ashaL, ashaL, ashaPN, false, 10, text("hers")),
			msgIn("X1", caraL, caraL, caraPN, false, 11, revokeOf(target(caraL, "D1", ""))),
		}, dm, "D1", false},
		{"they delete their own", []core.Input{
			msgIn("D1", ashaL, ashaL, ashaPN, false, 10, text("hers")),
			msgIn("X1", ashaPN, ashaPN, ashaL, false, 11, revokeOf(target(ashaPN, "D1", ""))),
		}, dm, "D1", true},
		{"we delete ours from the phone", []core.Input{
			msgIn("D1", ashaPN, mePN, "", true, 10, text("ours")),
			msgIn("X1", ashaPN, mePN, "", true, 11, revokeOf(target(ashaPN, "D1", ""))),
		}, dm, "D1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			both(t, c.ins, func(t *testing.T, db *core.DB, r *Reader) {
				m := one(t, r, c.addrs, c.id)
				if m.Facts.Revoked != c.revoked {
					t.Fatalf("revoked %v, want %v", m.Facts.Revoked, c.revoked)
				}
				if kept := bodyKept(t, db, c.id); kept == c.revoked {
					t.Fatalf("body still in the log %v, want %v", kept, !c.revoked)
				}
				if c.revoked && (m.Kind != "revoked" || m.Text != "" || len(m.Body) != 0) {
					t.Fatalf("tombstone %+v", m)
				}
				if !c.revoked && m.Text == "" {
					t.Fatalf("text gone: %+v", m)
				}
			})
		})
	}
}

// a message here before its delete and one after are scrubbed alike, and a
// copy history brings later is scrubbed too
func TestADeleteScrubsEveryCopy(t *testing.T) {
	ins := []core.Input{
		msgIn("D1", ashaL, ashaL, ashaPN, false, 10, text("hers")),
		msgIn("X1", ashaL, ashaL, ashaPN, false, 11, revokeOf(target(ashaL, "D1", ""))),
		msgIn("R1", ashaL, ashaL, ashaPN, false, 12, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: target(ashaL, "D1", ""), Text: proto.String("x")}}),
		historyConv(ashaL, webMsg("D1", ashaL, false, "", 10, text("hers"))),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		m := one(t, r, []string{ashaL, ashaPN}, "D1")
		if !m.Facts.Revoked || len(m.Facts.Reactions) != 0 || m.Text != "" {
			t.Fatalf("%+v", m)
		}
		var hits int
		if err := db.Read().QueryRow(`SELECT COUNT(*) FROM inputs WHERE instr(body, 'hers') > 0`).Scan(&hits); err != nil || hits != 0 {
			t.Fatalf("the words are still in %d inputs (%v)", hits, err)
		}
	})
}

func TestOnlyTheAuthorEdits(t *testing.T) {
	ins := []core.Input{
		msgIn("G1", grp, boL, boPN, false, 10, text("first")),
		msgIn("E1", grp, boL, boPN, false, 11, editOf(target(grp, "G1", boL), "second", 11)),
		// later, from someone else: it must neither show nor push out bo's
		msgIn("E2", grp, caraL, caraPN, false, 12, editOf(target(grp, "G1", boL), "forged", 12)),
		msgIn("D1", ashaPN, mePN, "", true, 20, text("ours")),
		msgIn("E3", ashaL, ashaL, ashaPN, false, 21, editOf(target(ashaL, "D1", ""), "theirs now", 21)),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		if got := one(t, r, []string{grp}, "G1").Facts.Edit.GetConversation(); got != "second" {
			t.Fatalf("group message reads %q", got)
		}
		if e := one(t, r, []string{ashaL, ashaPN}, "D1").Facts.Edit; e != nil {
			t.Fatalf("our message was edited by them: %v", e)
		}
	})
}

func TestPinsFollowTheGroupSetting(t *testing.T) {
	locked := func(on bool, sec int) core.Input {
		return in(core.KindGroupInfo, core.GroupInfoHead{JID: grp, T: at(sec).Unix(), Locked: proto.Bool(on)}, nil, at(sec))
	}
	for _, c := range []struct {
		name   string
		ins    []core.Input
		pinned bool
	}{
		{"anyone pins in an open group", []core.Input{
			groupOf(5, nil, boL, caraL), locked(false, 6),
			msgIn("G1", grp, boL, boPN, false, 10, text("x")),
			msgIn("P1", grp, caraL, caraPN, false, 11, pinOf(target(grp, "G1", boL), true, 11)),
		}, true},
		{"a member cannot pin where only admins edit settings", []core.Input{
			groupOf(5, nil, boL, caraL), locked(true, 6),
			msgIn("G1", grp, boL, boPN, false, 10, text("x")),
			msgIn("P1", grp, caraL, caraPN, false, 11, pinOf(target(grp, "G1", boL), true, 11)),
		}, false},
		{"an admin can", []core.Input{
			groupOf(5, []string{caraL}, boL, caraL), locked(true, 6),
			msgIn("G1", grp, boL, boPN, false, 10, text("x")),
			msgIn("P1", grp, caraL, caraPN, false, 11, pinOf(target(grp, "G1", boL), true, 11)),
		}, true},
		{"a member's unpin does not undo an admin's pin", []core.Input{
			groupOf(5, []string{caraL}, boL, caraL), locked(true, 6),
			msgIn("G1", grp, boL, boPN, false, 10, text("x")),
			msgIn("P1", grp, caraL, caraPN, false, 11, pinOf(target(grp, "G1", boL), true, 11)),
			msgIn("P2", grp, boL, boPN, false, 12, pinOf(target(grp, "G1", boL), false, 12)),
		}, true},
		{"a pin said in another chat", []core.Input{
			msgIn("D1", ashaL, ashaL, ashaPN, false, 10, text("x")),
			msgIn("P1", caraL, caraL, caraPN, false, 11, pinOf(target(caraL, "D1", ""), true, 11)),
		}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			both(t, c.ins, func(t *testing.T, db *core.DB, r *Reader) {
				addrs := []string{grp}
				id := "G1"
				if c.ins[0].Kind == core.KindMessage {
					addrs, id = []string{ashaL, ashaPN}, "D1"
				}
				if got := one(t, r, addrs, id).Facts.Pinned; got != c.pinned {
					t.Fatalf("pinned %v, want %v", got, c.pinned)
				}
			})
		})
	}
}

func TestOnlyTheAuthorUnkeeps(t *testing.T) {
	k := target(grp, "G1", boL)
	for _, c := range []struct {
		name string
		ins  []core.Input
		kept bool
	}{
		{"a member keeps it", []core.Input{
			msgIn("K1", grp, caraL, caraPN, false, 11, keepOf(k, true, 11)),
		}, true},
		{"another member cannot unkeep it", []core.Input{
			msgIn("K1", grp, caraL, caraPN, false, 11, keepOf(k, true, 11)),
			msgIn("K2", grp, ashaL, ashaPN, false, 12, keepOf(k, false, 12)),
		}, true},
		{"the author unkeeps it", []core.Input{
			msgIn("K1", grp, caraL, caraPN, false, 11, keepOf(k, true, 11)),
			msgIn("K2", grp, boL, boPN, false, 12, keepOf(k, false, 12)),
		}, false},
		{"after the author unkeeps nobody else keeps it", []core.Input{
			msgIn("K1", grp, boL, boPN, false, 11, keepOf(k, false, 11)),
			msgIn("K2", grp, caraL, caraPN, false, 12, keepOf(k, true, 12)),
		}, false},
		{"until the author keeps it again", []core.Input{
			msgIn("K1", grp, boL, boPN, false, 11, keepOf(k, false, 11)),
			msgIn("K2", grp, caraL, caraPN, false, 12, keepOf(k, true, 12)),
			msgIn("K3", grp, boL, boPN, false, 13, keepOf(k, true, 13)),
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ins := append([]core.Input{msgIn("G1", grp, boL, boPN, false, 10, text("x"))}, c.ins...)
			both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
				if got := one(t, r, []string{grp}, "G1").Facts.Kept; got != c.kept {
					t.Fatalf("kept %v, want %v", got, c.kept)
				}
			})
		})
	}
}

// a reaction or receipt names a message by id alone: said in another chat
// it is about nothing here
func TestFactsStayInTheirChat(t *testing.T) {
	ins := []core.Input{
		msgIn("G1", grp, boL, boPN, false, 10, text("x")),
		msgIn("R1", caraL, caraL, caraPN, false, 11, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: target(caraL, "G1", ""), Text: proto.String("x")}}),
		in(core.KindReceipt, core.ReceiptHead{Source: core.Source{Chat: caraL, Sender: caraL}, IDs: []string{"G1"}, Type: "read", T: at(12).Unix()}, nil, at(12)),
		msgIn("R2", grp, ashaL, ashaPN, false, 13, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: target(grp, "G1", boL), Text: proto.String("y")}}),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		f := one(t, r, []string{grp}, "G1").Facts
		if len(f.Reactions) != 1 || f.Reactions[0].Emoji != "y" || len(f.Receipts) != 0 {
			t.Fatalf("%+v", f)
		}
	})
}
