package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waServerSync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

var (
	asha  = types.NewJID("917770000001", types.DefaultUserServer)
	ashaL = types.NewJID("100000000001", types.HiddenUserServer)
	at    = time.Unix(1700000000, 0)
)

func head[T any](t *testing.T, in core.Input) T {
	t.Helper()
	var h T
	if err := json.Unmarshal(in.Head, &h); err != nil {
		t.Fatalf("%s head %s: %v", in.Kind, in.Head, err)
	}
	return h
}

func only(t *testing.T, evt any) core.Input {
	t.Helper()
	ins, err := inputsFor(evt)
	if err != nil || len(ins) != 1 {
		t.Fatalf("%T: %d inputs, %v", evt, len(ins), err)
	}
	if ins[0].V != headVersion {
		t.Fatalf("%s v%d", ins[0].Kind, ins[0].V)
	}
	return ins[0]
}

func message(text string) *events.Message {
	raw := &waE2E.Message{Conversation: proto.String(text)}
	return (&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: asha, Sender: ashaL, SenderAlt: asha, AddressingMode: types.AddressingModeLID},
			ID:            "M1", Timestamp: at, PushName: "Asha", Type: "text",
		},
		RawMessage: raw,
	}).UnwrapRaw()
}

func TestAMessageKeepsItsPlaintextExactly(t *testing.T) {
	evt := message("hi")
	evt.Plaintext = append(must(proto.Marshal(evt.RawMessage)), 0xfa, 0x01, 0x00) // a field nobody knows yet
	in := only(t, evt)
	h := head[core.MessageHead](t, in)
	if in.Kind != core.KindMessage || h.ID != "M1" || h.Chat != asha.String() || h.Sender != ashaL.String() ||
		h.SenderAlt != asha.String() || h.T != at.Unix() || h.PushName != "Asha" || h.Addressing != "lid" || !h.Exact {
		t.Fatalf("head %s", in.Head)
	}
	if !bytes.Equal(in.Body, evt.Plaintext) {
		t.Fatal("body is not the plaintext")
	}
}

func TestAMessageWithoutPlaintextIsReencoded(t *testing.T) {
	in := only(t, message("from history"))
	var back waE2E.Message
	if err := proto.Unmarshal(in.Body, &back); err != nil || back.GetConversation() != "from history" {
		t.Fatalf("body %x: %v", in.Body, err)
	}
	if head[core.MessageHead](t, in).Exact {
		t.Fatal("a re-encoding claims to be exact")
	}
}

func TestAHistoryNotificationIsItsOwnKind(t *testing.T) {
	evt := message("")
	evt.RawMessage = &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{HistorySyncNotification: &waE2E.HistorySyncNotification{
		SyncType: waE2E.HistorySyncType_RECENT.Enum(), ChunkOrder: proto.Uint32(3), Progress: proto.Uint32(40),
	}}}
	evt.UnwrapRaw()
	in := only(t, evt)
	h := head[core.HistoryNotificationHead](t, in)
	if in.Kind != core.KindHistoryNotification || h.SyncType != "RECENT" || h.ChunkOrder != 3 || h.Progress != 40 || h.ID != "M1" {
		t.Fatalf("%s %s", in.Kind, in.Head)
	}
	var n waE2E.HistorySyncNotification
	if err := proto.Unmarshal(in.Body, &n); err != nil || n.GetChunkOrder() != 3 {
		t.Fatalf("body: %v", err)
	}
}

func TestEveryOtherKindKeepsItsFacts(t *testing.T) {
	src := types.MessageSource{Chat: asha, Sender: asha}
	cases := []struct {
		evt   any
		kind  string
		check func(core.Input) bool
	}{
		{&events.Receipt{MessageSource: src, MessageIDs: []string{"A", "B"}, Type: types.ReceiptTypeRead, Timestamp: at}, core.KindReceipt, func(in core.Input) bool {
			h := head[core.ReceiptHead](t, in)
			return len(h.IDs) == 2 && h.Type == "read" && h.T == at.Unix() && h.Chat == asha.String()
		}},
		{&events.UndecryptableMessage{Info: types.MessageInfo{MessageSource: src, ID: "U", Timestamp: at}, IsUnavailable: true, UnavailableType: events.UnavailableTypeViewOnce}, core.KindUndecryptable, func(in core.Input) bool {
			h := head[core.UndecryptableHead](t, in)
			return h.ID == "U" && h.Unavailable && h.UnavailableType == "view_once"
		}},
		{&events.PushName{JID: ashaL, JIDAlt: asha, Message: &types.MessageInfo{ID: "M", Timestamp: at}, OldPushName: "A", NewPushName: "Asha"}, core.KindPushName, func(in core.Input) bool {
			h := head[core.PushNameHead](t, in)
			return h.JID == ashaL.String() && h.JIDAlt == asha.String() && h.New == "Asha" && h.Old == "A" && h.ID == "M" && h.T == at.Unix()
		}},
		{&events.BusinessName{JID: asha, NewBusinessName: "Shop"}, core.KindBusinessName, func(in core.Input) bool {
			h := head[core.BusinessNameHead](t, in)
			return h.New == "Shop" && h.T == 0
		}},
		{&events.IdentityChange{JID: asha, Timestamp: at, Implicit: true}, core.KindIdentityChange, func(in core.Input) bool {
			h := head[core.IdentityChangeHead](t, in)
			return h.Implicit && h.T == at.Unix()
		}},
		{&events.Picture{JID: asha, Author: asha, Timestamp: at, PictureID: "42"}, core.KindPicture, func(in core.Input) bool {
			return head[core.PictureHead](t, in).PictureID == "42"
		}},
		{&events.Blocklist{DHash: "d", Changes: []events.BlocklistChange{{JID: asha, Action: events.BlocklistChangeActionBlock}}}, core.KindBlocklist, func(in core.Input) bool {
			h := head[core.BlocklistHead](t, in)
			return len(h.Changes) == 1 && h.Changes[0].Action == "block" && h.Changes[0].JID == asha.String()
		}},
		{&events.PrivacySettings{NewSettings: types.PrivacySettings{LastSeen: types.PrivacySettingContacts, Online: types.PrivacySettingMatchLastSeen}, LastSeenChanged: true}, core.KindPrivacy, func(in core.Input) bool {
			h := head[core.PrivacyHead](t, in)
			return h.Settings["last"] == "contacts" && h.Settings["online"] == "match_last_seen" && len(h.Changed) == 1 && h.Changed[0] == "last"
		}},
	}
	for _, c := range cases {
		in := only(t, c.evt)
		if in.Kind != c.kind || !c.check(in) {
			t.Errorf("%T: %s %s", c.evt, in.Kind, in.Head)
		}
	}
	for _, evt := range []any{&events.Connected{}, &events.ChatPresence{}, &events.Presence{}} {
		if ins, err := inputsFor(evt); len(ins) != 0 || err != nil {
			t.Errorf("%T logged: %v %v", evt, ins, err)
		}
	}
}

func TestAppStateIsOneInputPerMutationAndWhereTheCollectionGotTo(t *testing.T) {
	pin := &waSyncAction.SyncActionValue{PinAction: &waSyncAction.PinAction{Pinned: proto.Bool(true)}}
	ins, err := appStateInputs(appstate.WAPatchRegularLow, 7, []appstate.Mutation{
		{Operation: waServerSync.SyncdMutation_SET, Index: []string{"pin_v1", asha.String()}, Version: 5, Action: pin, PatchVersion: 6},
		{Operation: waServerSync.SyncdMutation_REMOVE, Index: []string{"pin_v1", ashaL.String()}, Version: 5, PatchVersion: 7},
	}, false)
	if err != nil || len(ins) != 3 {
		t.Fatalf("%d inputs: %v", len(ins), err)
	}
	set := head[core.AppStateHead](t, ins[0])
	if set.Collection != "regular_low" || set.Version != 6 || set.Op != "set" || set.Index[0] != "pin_v1" || set.ActionVersion != 5 {
		t.Fatalf("set %s", ins[0].Head)
	}
	var back waSyncAction.SyncActionValue
	if err := proto.Unmarshal(ins[0].Body, &back); err != nil || !back.GetPinAction().GetPinned() {
		t.Fatalf("body: %v", err)
	}
	if head[core.AppStateHead](t, ins[1]).Op != "remove" {
		t.Fatalf("remove %s", ins[1].Head)
	}
	st := head[core.SyncStateHead](t, ins[2])
	if ins[2].Kind != core.KindSyncState || st.Domain != "app_state:regular_low" || st.Version != 7 || st.Count != 2 {
		t.Fatalf("sync state %s", ins[2].Head)
	}
}

type fakeLog struct {
	fail error
	got  [][]core.Input
}

func (f *fakeLog) AppendBatch(_ context.Context, ins []core.Input) ([]int64, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.got = append(f.got, ins)
	return make([]int64, len(ins)), nil
}

func TestAFailedAppendIsNotAcked(t *testing.T) {
	log := &fakeLog{fail: errors.New("disk full")}
	g := New(context.Background(), log)
	if g.handle(message("hi")) {
		t.Fatal("acked a message the log refused")
	}
	if err := g.appState(context.Background(), appstate.WAPatchRegularLow, 1, nil, false); err == nil {
		t.Fatal("app state the log refused was taken")
	}
	if !g.handle(&events.Connected{}) {
		t.Fatal("an event the log does not want was refused")
	}
	log.fail = nil
	if !g.handle(message("hi")) || len(log.got) != 1 {
		t.Fatal("a logged message was not acked")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
