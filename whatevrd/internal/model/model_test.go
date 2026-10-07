package model

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

const (
	mePN   = "919000000000@s.whatsapp.net"
	meLID  = "200000000000@lid"
	ashaPN = "917770000001@s.whatsapp.net"
	ashaL  = "100000000001@lid"
	boPN   = "917770000002@s.whatsapp.net"
	boL    = "100000000002@lid"
	grp    = "120363000000000001@g.us"
)

var base = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }

func in(kind string, h any, body []byte, when time.Time) core.Input {
	raw, err := json.Marshal(h)
	if err != nil {
		panic(err)
	}
	return core.Input{Kind: kind, V: 1, At: when, Head: raw, Body: body}
}

func pb(m proto.Message) []byte {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

func msgIn(id, chat, sender, alt string, fromMe bool, t int, m *waE2E.Message) core.Input {
	return in(core.KindMessage, core.MessageHead{
		Source: core.Source{Chat: chat, Sender: sender, SenderAlt: alt, FromMe: fromMe, Group: strings.HasSuffix(chat, "g.us")},
		ID:     id, T: base.Unix() + int64(t), PushName: map[bool]string{false: "Asha"}[fromMe], Exact: true,
	}, pb(m), at(t))
}

// boIn is bo writing in the group
func boIn(id string, t int, m *waE2E.Message) core.Input {
	return in(core.KindMessage, core.MessageHead{
		Source: core.Source{Chat: grp, Sender: boL, SenderAlt: boPN, Group: true},
		ID:     id, T: base.Unix() + int64(t), PushName: "Bobby", Exact: true,
	}, pb(m), at(t))
}

func text(s string) *waE2E.Message { return &waE2E.Message{Conversation: proto.String(s)} }

func appState(collection string, version uint64, op string, index []string, v *waSyncAction.SyncActionValue, t int) core.Input {
	var body []byte
	if v != nil {
		body = pb(v)
	}
	return in(core.KindAppState, core.AppStateHead{Collection: collection, Version: version, Index: index, Op: op}, body, at(t))
}

func seal(use, id, orig, mod string, secret, plain []byte) (iv, payload []byte) {
	key := hkdfutil.SHA256(secret, nil, []byte(id+orig+mod+use), 32)
	iv = make([]byte, 12)
	payload, err := gcmutil.Encrypt(key, iv, plain, fmt.Appendf(nil, "%s\x00%s", id, mod))
	if err != nil {
		panic(err)
	}
	return iv, payload
}

func historyConv(id string, msgs ...*waWeb.WebMessageInfo) core.Input {
	c := &waHistorySync.Conversation{ID: proto.String(id), Name: proto.String("History " + id), UnreadCount: proto.Uint32(1),
		ConversationTimestamp: proto.Uint64(uint64(base.Unix()))}
	for _, m := range msgs {
		c.Messages = append(c.Messages, &waHistorySync.HistorySyncMsg{Message: m})
	}
	return in(core.KindHistoryConversation, core.HistoryConversationHead{Notification: "N1", SyncType: "FULL", ID: id}, pb(c), at(50))
}

func webMsg(id, chat string, fromMe bool, participant string, t int, m *waE2E.Message) *waWeb.WebMessageInfo {
	w := &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chat), FromMe: proto.Bool(fromMe), ID: proto.String(id)},
		Message:          m,
		MessageTimestamp: proto.Uint64(uint64(base.Unix() + int64(t))),
	}
	if participant != "" {
		w.Key.Participant = proto.String(participant)
	}
	return w
}

// scenario is a bit of everything the folds have to agree on in any order.
func scenario() []core.Input {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	poll := &waE2E.Message{
		PollCreationMessage: &waE2E.PollCreationMessage{Name: proto.String("dinner?"), Options: []*waE2E.PollCreationMessage_Option{
			{OptionName: proto.String("pizza")}, {OptionName: proto.String("dosa")},
		}},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: secret},
	}
	pizza := sha256.Sum256([]byte("pizza"))
	iv, payload := seal(useVote, "P1", mePN, ashaPN, secret, pb(&waE2E.PollVoteMessage{SelectedOptions: [][]byte{pizza[:]}}))
	vote := &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{
		PollCreationMessageKey: &waCommon.MessageKey{RemoteJID: proto.String(grp), FromMe: proto.Bool(false), ID: proto.String("P1"), Participant: proto.String(mePN)},
		Vote:                   &waE2E.PollEncValue{EncIV: iv, EncPayload: payload},
		SenderTimestampMS:      proto.Int64(at(31).UnixMilli()),
	}}
	key := func(chat, id string, fromMe bool) *waCommon.MessageKey {
		return &waCommon.MessageKey{RemoteJID: proto.String(chat), ID: proto.String(id), FromMe: proto.Bool(fromMe)}
	}
	reply := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("yes"),
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("M1"), Participant: proto.String(ashaL)}}}
	return []core.Input{
		in(core.KindLIDMapping, core.LIDMappingHead{LID: meLID, PN: mePN, Self: true}, nil, at(0)),
		in(core.KindLIDMapping, core.LIDMappingHead{LID: boL, PN: boPN}, nil, at(1)),
		msgIn("M1", ashaL, ashaL, ashaPN, false, 10, text("hi")),
		msgIn("M2", ashaPN, mePN, "", true, 11, reply),
		msgIn("R1", ashaL, ashaL, ashaPN, false, 12, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: key(ashaPN, "M2", false), Text: proto.String("👍"), SenderTimestampMS: proto.Int64(at(12).UnixMilli())}}),
		msgIn("R2", ashaL, ashaL, ashaPN, false, 13, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: key(ashaPN, "M2", false), Text: proto.String("❤️"), SenderTimestampMS: proto.Int64(at(13).UnixMilli())}}),
		msgIn("E1", ashaPN, mePN, "", true, 14, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: key(ashaPN, "M2", true), EditedMessage: text("yes!"), TimestampMS: proto.Int64(at(14).UnixMilli())}}),
		msgIn("M3", ashaL, ashaL, ashaPN, false, 15, text("oops")),
		msgIn("X1", ashaL, ashaL, ashaPN, false, 16, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: key(ashaL, "M3", true)}}),
		msgIn("P1", grp, mePN, "", true, 30, poll),
		in(core.KindMessage, core.MessageHead{Source: core.Source{Chat: grp, Sender: ashaPN, Group: true}, ID: "V1", T: at(31).Unix(), Exact: true}, pb(vote), at(31)),
		msgIn("G1", grp, boL, boPN, false, 32, text("in the group")),
		msgIn("U1", ashaL, ashaL, ashaPN, false, 40, text("late")),
		in(core.KindUndecryptable, core.UndecryptableHead{Source: core.Source{Chat: ashaL, Sender: ashaL}, ID: "U1", T: at(40).Unix()}, nil, at(40)),
		in(core.KindUndecryptable, core.UndecryptableHead{Source: core.Source{Chat: ashaL, Sender: ashaL}, ID: "U2", T: at(41).Unix(), Unavailable: true, UnavailableType: "view_once"}, nil, at(41)),
		historyConv(ashaPN,
			webMsg("M1", ashaPN, false, "", 10, text("hi")),
			webMsg("H1", ashaPN, false, "", 5, text("older")),
			webMsg("H2", ashaPN, true, "", 6, text("delete me")),
			webMsg("H3", ashaPN, false, "", 7, text("before the clear")),
		),
		historyConv(grp, webMsg("H4", grp, false, boPN, 3, text("group history"))),
		in(core.KindHistoryExtra, core.HistoryExtraHead{Notification: "N1", SyncType: "FULL", ChunkOrder: 1, Progress: 100, Conversations: 2},
			pb(&waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_FULL.Enum(),
				Pushnames:                []*waHistorySync.Pushname{{ID: proto.String(boPN), Pushname: proto.String("Bo")}},
				PhoneNumberToLidMappings: []*waHistorySync.PhoneNumberToLIDMapping{{PnJID: proto.String(ashaPN), LidJID: proto.String(ashaL)}},
				InlineContacts:           []*waHistorySync.InlineContact{{PnJID: proto.String(ashaPN), LidJID: proto.String(ashaL), FullName: proto.String("Asha Rao")}},
			}), at(51)),
		in(core.KindHistoryNotification, core.HistoryNotificationHead{ID: "N1", SyncType: "FULL", ChunkOrder: 1, Progress: 100}, nil, at(49)),
		appState("regular_low", 3, "set", []string{"pin_v1", ashaPN}, &waSyncAction.SyncActionValue{PinAction: &waSyncAction.PinAction{Pinned: proto.Bool(true)}}, 60),
		appState("regular_low", 4, "set", []string{"pin_v1", ashaPN}, &waSyncAction.SyncActionValue{PinAction: &waSyncAction.PinAction{Pinned: proto.Bool(false)}}, 61),
		appState("regular_low", 5, "set", []string{"pin_v1", ashaPN}, &waSyncAction.SyncActionValue{PinAction: &waSyncAction.PinAction{Pinned: proto.Bool(true)}}, 62),
		appState("regular_low", 2, "set", []string{"archive", grp}, &waSyncAction.SyncActionValue{ArchiveChatAction: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}}, 63),
		appState("regular_high", 7, "set", []string{"star", ashaPN, "M2", "1", "0"}, &waSyncAction.SyncActionValue{StarAction: &waSyncAction.StarAction{Starred: proto.Bool(true)}}, 64),
		appState("regular_high", 8, "set", []string{"deleteMessageForMe", ashaL, "H2", "1", "0"}, &waSyncAction.SyncActionValue{DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{}}, 65),
		appState("regular_high", 9, "set", []string{"clearChat", grp, "1", "0"}, &waSyncAction.SyncActionValue{ClearChatAction: &waSyncAction.ClearChatAction{
			MessageRange: &waSyncAction.SyncActionMessageRange{LastMessageTimestamp: proto.Int64(base.Unix() + 4)}}}, 66),
		appState("critical_unblock_low", 2, "set", []string{"contact", boPN}, &waSyncAction.SyncActionValue{ContactAction: &waSyncAction.ContactAction{FullName: proto.String("Bo Contact"), LidJID: proto.String(boL)}}, 67),
		in(core.KindPushName, core.PushNameHead{JID: boL, JIDAlt: boPN, New: "Bobby", T: at(70).Unix()}, nil, at(70)),
		in(core.KindReceipt, core.ReceiptHead{Source: core.Source{Chat: ashaL, Sender: ashaL}, IDs: []string{"M2"}, Type: "read", T: at(80).Unix()}, nil, at(80)),
		in(core.KindReceipt, core.ReceiptHead{Source: core.Source{Chat: ashaL, Sender: ashaL}, IDs: []string{"M2"}, Type: "read", T: at(81).Unix()}, nil, at(81)),
		in(core.KindGroupInfo, core.GroupInfoHead{JID: grp, Full: true, T: at(90).Unix(), Name: proto.String("Friends"), NameT: at(20).Unix(),
			Participants: []core.GroupParticipant{{JID: boL, PN: boPN, Admin: true}, {JID: ashaL, PN: ashaPN}}}, nil, at(90)),
		in(core.KindGroupInfo, core.GroupInfoHead{JID: grp, T: at(95).Unix(), Name: proto.String("Old friends"), NameT: at(95).Unix(), Leave: []string{ashaL}}, nil, at(95)),
		in(core.KindGroupInfo, core.GroupInfoHead{JID: grp, T: at(85).Unix(), Join: []string{"100000000009@lid"}}, nil, at(85)),
		in(core.KindPrivacy, core.PrivacyHead{Settings: map[string]string{"last": "contacts"}}, nil, at(100)),
		in(core.KindBlocklist, core.BlocklistHead{Changes: []core.BlocklistChange{{JID: "917770000005@s.whatsapp.net", Action: "block"}}}, nil, at(101)),
		in(core.KindSyncState, core.SyncStateHead{Domain: "app_state:regular_low", Version: 5, Count: 3}, nil, at(102)),
		in(core.KindLocal, core.LocalHead{Chat: ashaL, ID: "M1", Op: core.MediaError, Error: "timeout"}, nil, at(110)),
		in(core.KindLocal, core.LocalHead{Chat: ashaL, ID: "M1", Op: core.MediaFile, Path: "/m/M1.jpg", W: 4, H: 3}, nil, at(111)),
		in(core.KindLocal, core.LocalHead{Chat: ashaL, ID: "M1", Op: core.MediaFile, Path: "/m/M1b.jpg"}, nil, at(112)),
		in(core.KindLocal, core.LocalHead{Chat: ashaL, ID: "M3", Op: core.MediaFile, Path: "/m/M3.jpg"}, nil, at(113)),
		in(core.KindAvatar, core.AvatarHead{JID: boL, Status: core.AvatarError, Error: "500"}, nil, at(114)),
		in(core.KindAvatar, core.AvatarHead{JID: boL, Status: core.AvatarOK, PictureID: "p1", Path: "/a/bo"}, nil, at(115)),
		in(core.KindAvatar, core.AvatarHead{JID: boL, Status: core.AvatarError, Error: "timeout"}, nil, at(116)),
		in(core.KindPrefs, core.PrefsHead{Prefs: json.RawMessage(`{"notify":true}`)}, nil, at(117)),
		in(core.KindPrefs, core.PrefsHead{Prefs: json.RawMessage(`{"notify":false}`)}, nil, at(118)),
		// bo shares where he is, and deletes one point of it
		boIn("LV0", 120, liveOpen(28)),
		boIn("LV1", 121, liveAt(28.1, 1)),
		boIn("LV2", 122, liveAt(28.2, 2)),
		boIn("LVX", 123, revokeOf(target(grp, "LV1", boL))),
		// the sticker picker
		recentsIn("N9", 124, recentMeta(1, 11, -100, 2)),
		recentsIn("N8", 125, recentMeta(1, 11, -90, 1)),
		stickerIn(core.StickerHead{Op: core.StickerFile, Key: hx(1), Enc: hx(11), Path: "/s/1.webp"}, nil, 126),
		stickerIn(core.StickerHead{Op: core.StickerFile, Key: hx(1), Enc: hx(11), Path: "/s/1b.webp"}, nil, 127),
		stickerIn(core.StickerHead{Op: core.StickerInstalled, Key: "P1", On: true}, nil, 128),
	}
}

func openModel(t *testing.T) *core.DB {
	t.Helper()
	tr := &tracker{}
	db, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"),
		core.Options{Domains: Domains(), Log: zerolog.Nop(), OnChange: tr.change})
	if err != nil {
		t.Fatal(err)
	}
	trackers.Store(db, tr)
	t.Cleanup(func() { db.Close(); trackers.Delete(db) })
	return db
}

func feed(t *testing.T, db *core.DB, ins []core.Input) {
	t.Helper()
	ctx := context.Background()
	var last int64
	for _, x := range ins {
		seq, err := db.Append(ctx, x)
		if err != nil {
			t.Fatal(err)
		}
		last = seq
	}
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := db.WaitFolded(wctx, last); err != nil {
		t.Fatal(err)
	}
	if f, err := db.FoldFailures(ctx); err != nil || len(f) > 0 {
		t.Fatalf("fold failures %v %v", f, err)
	}
	checkSummary(t, db.Read())
	checkSearchIndex(t, db)
	if tr, ok := trackers.Load(db); ok {
		tr.(*tracker).checkPatch(t, db)
	}
}

var seqCol = regexp.MustCompile(`\bseq=(\d+)`)

// normalized is the derived dump with every seq swapped for which input of
// the scenario it names, so two orders compare.
func normalized(t *testing.T, db *core.DB, scen []core.Input) map[string][]string {
	t.Helper()
	ctx := context.Background()
	ins, err := db.Inputs(ctx, 0, 100000)
	if err != nil {
		t.Fatal(err)
	}
	ident := map[string]int{}
	for i, x := range scen {
		ident[x.Kind+string(x.Head)] = i
	}
	bySeq := map[string]string{}
	for _, x := range ins {
		bySeq[fmt.Sprint(x.Seq)] = fmt.Sprint("#", ident[x.Kind+string(x.Head)])
	}
	d, err := db.DumpDerived(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for table, lines := range d {
		for i, l := range lines {
			lines[i] = seqCol.ReplaceAllStringFunc(l, func(m string) string {
				return "seq=" + bySeq[m[4:]]
			})
		}
		sortStrings(lines)
		// a duplicate input is the same input of the scenario twice
		var uniq []string
		for i, l := range lines {
			if i == 0 || l != lines[i-1] {
				uniq = append(uniq, l)
			}
		}
		d[table] = uniq
	}
	return d
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestEveryOrderFoldsTheSame(t *testing.T) {
	scen := scenario()
	ref := openModel(t)
	feed(t, ref, scen)
	want := normalized(t, ref, scen)

	r := rand.New(rand.NewPCG(1, 2))
	orders := [][]core.Input{reversed(scen)}
	for range 12 {
		p := append([]core.Input(nil), scen...)
		r.Shuffle(len(p), func(i, j int) { p[i], p[j] = p[j], p[i] })
		orders = append(orders, p)
	}
	// every input twice
	orders = append(orders, append(append([]core.Input(nil), scen...), scen...))
	for i, order := range orders {
		db := openModel(t)
		feed(t, db, order)
		got := normalized(t, db, scen)
		if !reflect.DeepEqual(got, want) {
			for table := range want {
				if !reflect.DeepEqual(got[table], want[table]) {
					t.Errorf("order %d, table %s:\n got  %q\n want %q", i, table, got[table], want[table])
				}
			}
			t.FailNow()
		}
	}
}

func reversed(s []core.Input) []core.Input {
	out := make([]core.Input, len(s))
	for i, x := range s {
		out[len(s)-1-i] = x
	}
	return out
}

func query(t *testing.T, db *core.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := db.Read().Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			parts[i] = fmt.Sprint(v)
		}
		out = append(out, strings.Join(parts, "|"))
	}
	return out
}

func expect(t *testing.T, db *core.DB, want []string, q string, args ...any) {
	t.Helper()
	if got := query(t, db, q, args...); !reflect.DeepEqual(got, want) {
		t.Errorf("%s\n got  %q\n want %q", q, got, want)
	}
}

func TestTheScenarioFoldsRight(t *testing.T) {
	db := openModel(t)
	feed(t, db, scenario())

	expect(t, db, []string{
		"100000000001@lid|M1|hi", "100000000001@lid|M3|", "100000000001@lid|U1|late",
		"120363000000000001@g.us|G1|in the group", "120363000000000001@g.us|LV0|", "120363000000000001@g.us|P1|dinner?\npizza\ndosa",
		"917770000001@s.whatsapp.net|H1|older", "917770000001@s.whatsapp.net|H3|before the clear",
		"917770000001@s.whatsapp.net|M1|hi", "917770000001@s.whatsapp.net|M2|yes",
	}, `SELECT chat, id, text FROM msg ORDER BY chat, id`)
	expect(t, db, []string{"M1"}, `SELECT reply FROM msg WHERE id = 'M2'`)
	expect(t, db, []string{"100000000001@lid|M2|100000000001@lid|❤️"}, `SELECT chat, target, sender, emoji FROM f_reaction`)
	expect(t, db, []string{"M2"}, `SELECT target FROM f_edit`)
	expect(t, db, []string{"LV1|100000000002@lid", "M3|100000000001@lid"}, `SELECT target, by FROM f_revoke ORDER BY target`)
	expect(t, db, []string{"LV0|1|28", "LV1|0|<nil>", "LV2|0|28.2"}, `SELECT id, opener, lat FROM live ORDER BY id`)
	expect(t, db, []string{"Poll Vote|1"}, `SELECT use, plain IS NOT NULL FROM f_enc WHERE target = 'P1'`)
	// facts keep the chat address they came with, reads join them
	expect(t, db, []string{"100000000001@lid|M2|100000000001@lid|read"}, `SELECT chat, id, who, type FROM f_receipt`)
	expect(t, db, []string{"100000000001@lid|U2|view_once"}, `SELECT chat, id, unavailable FROM msg_wait WHERE id NOT IN (SELECT id FROM msg)`)
	expect(t, db, []string{"100000000001@lid|H2", "917770000001@s.whatsapp.net|H2", "120363000000000001@g.us|H4"}, `SELECT chat, id FROM msg_gone ORDER BY id, chat`)

	expect(t, db, []string{"1|5"}, `SELECT on_, version FROM appstate WHERE kind = 'pin_v1'`)
	expect(t, db, []string{"917770000001@s.whatsapp.net|100000000001@lid", "917770000002@s.whatsapp.net|100000000002@lid",
		"919000000000@s.whatsapp.net|200000000000@lid"},
		`SELECT pn, lid FROM id_owner ORDER BY pn`)
	expect(t, db, []string{"200000000000@lid", "919000000000@s.whatsapp.net"}, `SELECT jid FROM id_self ORDER BY jid`)
	expect(t, db, []string{
		"100000000001@lid|inline|Asha Rao", "100000000001@lid|push|Asha",
		"100000000002@lid|push|Bobby", "917770000001@s.whatsapp.net|inline|Asha Rao",
		"917770000001@s.whatsapp.net|push|Asha", "917770000002@s.whatsapp.net|push|Bobby",
	}, `SELECT jid, source, name FROM id_name ORDER BY jid, source`)

	expect(t, db, []string{"name|Old friends"}, `SELECT field, value FROM grp_field WHERE field = 'name'`)
	expect(t, db, []string{"100000000001@lid|90000|95000", "100000000002@lid|90000|0", "100000000009@lid|0|0"},
		`SELECT jid, MAX(snap_t - ?, 0), MAX(out_t - ?, 0) FROM grp_member ORDER BY jid`, at(0).UnixMilli(), at(0).UnixMilli())
	expect(t, db, []string{"917770000001@s.whatsapp.net|History 917770000001@s.whatsapp.net|1"}, `SELECT chat, name, unread FROM hist_chat WHERE chat LIKE '%whatsapp%'`)
	expect(t, db, []string{"N1||100", "N8||0", "N9||0"}, `SELECT notification, error, progress FROM hist_blob ORDER BY notification`)
	// the newest file and the history's newest use win
	expect(t, db, []string{"file|" + hx(1) + "|/s/1b.webp", "installed|P1|<nil>"}, `SELECT op, key, json_extract(value, '$.path') FROM stk ORDER BY op`)
	expect(t, db, []string{hx(1) + "|0|2"}, `SELECT plain, used - ?, weight FROM stk_recent`, at(-90).Unix())
}

// a delete blanks what it took in the log, and a rebuild from that log ends
// where folding it live did.
func TestDeletesScrubTheLogAndRebuildAgrees(t *testing.T) {
	rebuildAgrees(t, scenario(), func(ins []core.Input) {
		for _, x := range ins {
			if x.Kind == core.KindHistoryConversation && strings.Contains(string(x.Body), "delete me") {
				t.Error("a deleted history message is still in the log")
			}
			if x.Kind == core.KindHistoryConversation && strings.Contains(string(x.Body), "group history") {
				t.Error("a cleared message is still in the log")
			}
		}
	})
}

// rebuildAgrees folds scen, hands the log as it was left to check, and
// folds that log again from nothing to the same tables.
func rebuildAgrees(t *testing.T, scen []core.Input, check func([]core.Input)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "core.db")
	ctx := context.Background()
	db, err := core.Open(ctx, path, core.Options{Domains: Domains(), Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	feed(t, db, scen)
	want := normalized(t, db, scen)
	ins, err := db.Inputs(ctx, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	check(ins)
	db.Close()

	bumped := Domains()
	bumped[0].Version += 1000
	db, err = core.Open(ctx, path, core.Options{Domains: bumped, Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, appended := db.Progress()
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := db.WaitFolded(wctx, appended); err != nil {
		t.Fatal(err)
	}
	if got := normalized(t, db, scen); !reflect.DeepEqual(got, want) {
		for table := range want {
			if !reflect.DeepEqual(got[table], want[table]) {
				t.Errorf("table %s after rebuild:\n got  %q\n want %q", table, got[table], want[table])
			}
		}
	}
}

func TestReadsPutTheScenarioTogether(t *testing.T) {
	db := openModel(t)
	feed(t, db, scenario())
	r := NewReader(db.Read())
	ctx := context.Background()

	chats, err := r.Chats(ctx, ChatFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Chat{}
	for _, c := range chats {
		got[c.Key] = c
	}
	asha, ok := got[ashaL]
	if !ok || len(chats) != 1 || chats[0].Key != ashaL {
		t.Fatalf("chats %+v", chats)
	}
	// the pn copy and the lid copy are one chat, pinned, named from the
	// address book history sent
	if !asha.Pinned || asha.Name != "Asha Rao" || len(asha.Addrs) != 2 || asha.PN != ashaPN {
		t.Fatalf("asha %+v", asha)
	}
	// our reply at t=11 read everything before it; of M3 and U1 after it, M3
	// was deleted
	if asha.Unread != 1 {
		t.Fatalf("asha unread %d", asha.Unread)
	}
	archived, err := r.Chats(ctx, ChatFilter{Archived: true})
	if err != nil || len(archived) != 1 || archived[0].Name != "Old friends" || !archived[0].Group {
		t.Fatalf("archived %+v %v", archived, err)
	}

	ms, err := r.Messages(ctx, asha.Addrs, Cursor{}, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "U2,U1,M3,M2,M1,H3,H1" {
		t.Fatalf("transcript %v", ids)
	}
	byID := map[string]Message{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	m2 := byID["M2"]
	if len(m2.Facts.Reactions) != 1 || m2.Facts.Reactions[0].Emoji != "❤️" || !m2.Facts.Starred ||
		m2.Facts.Edit.GetConversation() != "yes!" || len(m2.Facts.Receipts) != 1 {
		t.Fatalf("M2 facts %+v", m2.Facts)
	}
	if !byID["M3"].Facts.Revoked || !byID["U2"].Waiting || byID["U2"].Wait != "view_once" || byID["U1"].Waiting {
		t.Fatalf("M3 %+v U2 %+v U1 %+v", byID["M3"].Facts, byID["U2"], byID["U1"])
	}
	if c, _ := byID["H1"].Content(); c.GetConversation() != "older" {
		t.Fatalf("history body %v", c)
	}
	if c, _ := byID["M1"].Content(); c.GetConversation() != "hi" || byID["M1"].Src != srcExact {
		t.Fatalf("M1 should be the live copy: %+v", byID["M1"])
	}

	page, err := r.Messages(ctx, asha.Addrs, Cursor{T: byID["M2"].T, ID: "M2"}, 2, false)
	if err != nil || len(page) != 2 || page[0].ID != "M1" || page[1].ID != "H3" {
		t.Fatalf("page %+v %v", page, err)
	}
	all, err := r.Messages(ctx, archived[0].Addrs, Cursor{}, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	// the group's notifications are rows of their own, newest first
	var g []Message
	var sys []string
	for _, m := range all {
		if m.System != nil {
			sys = append(sys, m.System.Type+" "+m.System.Value+strings.Join(m.System.Who, ","))
			continue
		}
		g = append(g, m)
	}
	if want := []string{"group_name Old friends", "group_leave " + ashaL, "group_join 100000000009@lid"}; !reflect.DeepEqual(sys, want) {
		t.Fatalf("system rows %q, want %q", sys, want)
	}
	if len(g) != 3 || len(g[2].Facts.Votes) != 1 {
		t.Fatalf("group %+v %v", g, err)
	}
	// the share is its opener at its newest point, the deleted one skipped
	if l := g[0].Facts.Live; g[0].ID != "LV0" || l == nil || l.Points != 3 || l.Lat != 28.2 {
		t.Fatalf("share %+v", g[0])
	}
	if v := Vote(g[2].Facts.Votes[0].Plain); len(v) != 1 {
		t.Fatalf("vote %v", v)
	}
	w, err := r.World(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := w.Name(boL); n != "Bo Contact" {
		t.Fatalf("bo %q", n)
	}
	if n, src := w.Name("917770000003@s.whatsapp.net"); n != "+91 77700 00003" || src != "phone" {
		t.Fatalf("stranger %q %q", n, src)
	}
}

// checkSearchIndex holds the trigram index against the texts it was built
// from. the check is a write, the read pool cannot run it.
func checkSearchIndex(t *testing.T, db *core.DB) {
	t.Helper()
	var seq int
	var name, file string
	if err := db.Read().QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file); err != nil {
		t.Fatal(err)
	}
	rw, err := sql.Open("sqlite3", "file:"+file+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	if _, err := rw.Exec(`INSERT INTO msg_text (msg_text, rank) VALUES ('integrity-check', 1)`); err != nil {
		t.Fatalf("msg_text out of step with msg: %v", err)
	}
}
