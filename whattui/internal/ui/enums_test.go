package ui

import (
	"strings"
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	gproto "google.golang.org/protobuf/proto"
)

// a newer daemon's enum value reads as unspecified, never as a crash or as a
// state it is not
func TestEnumValuesThisBuildNeverHeardOfReadAsUnspecified(t *testing.T) {
	a := stubApp(120, 30, 4, 6)

	setConn(a.conn, v2.ConnectionRow_builder{State: v2.ConnectionState(99), Detail: "doing something new"}.Build())
	upsert(a.syncs, v2.Upsert_builder{Sync: v2.SyncRow_builder{Type: v2.SyncType(99), Phase: v2.SyncPhase(99), Percent: 50}.Build()})
	ready(a.syncs, false)
	upsert(a.problems, v2.Upsert_builder{Id: "p", Sort: []byte("1"), Problem: v2.ProblemRow_builder{Kind: v2.ProblemKind(99), Text: "a new kind of trouble"}.Build()})
	ready(a.problems, false)
	_, m := pointAt(a, t, true)
	row := gproto.Clone(m).(*v2.MessageRow)
	row.SetStatus(v2.MessageStatus(99))
	it, _ := a.conversation.msgs.Get(m.GetId())
	putMsg(a.conversation.msgs, it.Sort, row)
	chat, _ := a.chatRow(a.conversation.chatID)
	odd := gproto.Clone(chat).(*v2.ChatRow)
	odd.SetType(v2.ChatType(99))
	putChat(a.chats, "00000000000000000000", odd)
	a.paint()

	if got := a.headerText(); !strings.Contains(got, "doing something new") || strings.Contains(got, "syncing") {
		t.Errorf("the header says %q", got)
	}
	if statusOf(row) != v2.MessageStatus_MESSAGE_STATUS_UNSPECIFIED || isGroup(odd) {
		t.Error("an unknown status or chat type read as a known one")
	}
	choices, _ := a.statusChoices(time.Now())
	var lines []string
	for _, c := range choices {
		lines = append(lines, c.Label+" | "+c.Detail)
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "connection | unknown") || !strings.Contains(got, "sync | unknown") || !strings.Contains(got, "a new kind of trouble") {
		t.Errorf("/status reads\n%s", got)
	}
	if loginState(v2.LoginRow_builder{State: v2.LoginState(99)}.Build()) != v2.LoginState_LOGIN_STATE_UNSPECIFIED {
		t.Error("an unknown login state read as a known one")
	}
}
