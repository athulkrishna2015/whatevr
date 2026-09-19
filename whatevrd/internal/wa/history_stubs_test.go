package wa

import (
	"context"
	"path/filepath"
	"testing"

	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// A stub carries no message, so backfill parsed it into nothing and a group
// pulled out of history had no record of who joined or left. Each stub has to
// become the same pill the live path writes.
func TestHistoryStubSystemPayload(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}

	stub := func(kind waWeb.WebMessageInfo_StubType, params ...string) *waWeb.WebMessageInfo {
		return &waWeb.WebMessageInfo{
			MessageStubType:       kind.Enum(),
			MessageStubParameters: params,
			MessageTimestamp:      proto.Uint64(1700000000),
			Participant:           proto.String("111@s.whatsapp.net"),
		}
	}

	cases := []struct {
		name string
		msg  *waWeb.WebMessageInfo
		want string
	}{
		{"add", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD, "222@s.whatsapp.net"), appstore.SystemTypeGroupJoin},
		{"leave", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE, "222@s.whatsapp.net"), appstore.SystemTypeGroupLeave},
		{"promote", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE, "222@s.whatsapp.net"), appstore.SystemTypeGroupPromote},
		{"demote", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE, "222@s.whatsapp.net"), appstore.SystemTypeGroupDemote},
		{"subject", stub(waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT, "Weekend plans"), appstore.SystemTypeGroupName},
		{"announce", stub(waWeb.WebMessageInfo_GROUP_CHANGE_ANNOUNCE, "on"), appstore.SystemTypeGroupAnnounce},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, ts, ok := client.historyStubSystemPayload(ctx, tc.msg)
			if !ok {
				t.Fatalf("%s produced no pill", tc.name)
			}
			if payload.Type != tc.want {
				t.Fatalf("type = %q, want %q", payload.Type, tc.want)
			}
			if ts.Unix() != 1700000000 {
				t.Fatalf("timestamp = %d, want the stub's own", ts.Unix())
			}
			if payload.Actor == nil {
				t.Fatal("the pill lost who did it")
			}
		})
	}

	if got := mustStubPayload(t, client, ctx, stub(waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT, "Weekend plans")); got.Value != "Weekend plans" {
		t.Fatalf("subject value = %q, want the new subject", got.Value)
	}
	if got := mustStubPayload(t, client, ctx, stub(waWeb.WebMessageInfo_GROUP_CHANGE_ANNOUNCE, "off")); got.On {
		t.Fatal("an announce-off stub read as on")
	}

	// A message that is not a stub must stay on the ordinary ingest path.
	if _, _, ok := client.historyStubSystemPayload(ctx, &waWeb.WebMessageInfo{}); ok {
		t.Fatal("a message with no stub type became a pill")
	}
}

func mustStubPayload(t *testing.T, c *Client, ctx context.Context, msg *waWeb.WebMessageInfo) appstore.SystemPayload {
	t.Helper()
	payload, _, ok := c.historyStubSystemPayload(ctx, msg)
	if !ok {
		t.Fatal("expected a pill")
	}
	return payload
}
