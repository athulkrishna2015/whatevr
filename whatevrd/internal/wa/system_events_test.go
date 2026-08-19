package wa

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	appstore "whatevrd/internal/store"
)

func person(name string) appstore.SystemParticipant {
	return appstore.SystemParticipant{JID: name + "@s.whatsapp.net", Name: name}
}

func me() appstore.SystemParticipant {
	return appstore.SystemParticipant{JID: "me@s.whatsapp.net", Self: true}
}

func actor(participant appstore.SystemParticipant) *appstore.SystemParticipant {
	return &participant
}

// The sentence is the whole content of a system pill, so every type gets one and
// every type gets the right one. The cases that matter are the ones where the
// same event says two different things depending on who it named: somebody
// joining under their own steam versus being added, and anything involving us.
func TestSystemSummarySpeaksEveryEvent(t *testing.T) {
	cases := []struct {
		name    string
		payload appstore.SystemPayload
		want    string
	}{
		{
			name: "somebody joins by themselves",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupJoin,
				Actor:        actor(person("Ana")),
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "Ana joined",
		},
		{
			name: "somebody is added",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupJoin,
				Actor:        actor(person("Cy")),
				Participants: []appstore.SystemParticipant{person("Ana"), person("Bo")},
			},
			want: "Cy added Ana and Bo",
		},
		{
			name: "we are added",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupJoin,
				Actor:        actor(person("Cy")),
				Participants: []appstore.SystemParticipant{me()},
			},
			want: "Cy added you",
		},
		{
			name: "we add somebody",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupJoin,
				Actor:        actor(me()),
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "You added Ana",
		},
		{
			name: "a join through an invite link has no author",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupJoin,
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "Ana joined",
		},
		{
			name: "somebody leaves",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupLeave,
				Actor:        actor(person("Ana")),
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "Ana left",
		},
		{
			name: "somebody is removed",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupLeave,
				Actor:        actor(person("Cy")),
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "Cy removed Ana",
		},
		{
			name: "promotion names the person in the middle",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupPromote,
				Actor:        actor(person("Cy")),
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "Cy made Ana an admin",
		},
		{
			name: "demotion",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeGroupDemote,
				Actor:        actor(person("Cy")),
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "Cy removed Ana as admin",
		},
		{
			name:    "rename",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupName, Actor: actor(person("Cy")), Value: "Trip"},
			want:    `Cy changed the group name to "Trip"`,
		},
		{
			name:    "description set",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupTopic, Actor: actor(person("Cy")), Value: "Leaving at six"},
			want:    "Cy changed the group description",
		},
		{
			name:    "description removed",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupTopic, Actor: actor(person("Cy"))},
			want:    "Cy removed the group description",
		},
		{
			name:    "photo",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupPhoto, Actor: actor(person("Cy")), On: true},
			want:    "Cy changed the group photo",
		},
		{
			name:    "locked",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupLocked, Actor: actor(person("Cy")), On: true},
			want:    "Cy restricted editing the group info to admins",
		},
		{
			name:    "unlocked",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupLocked, Actor: actor(person("Cy"))},
			want:    "Cy allowed everyone to edit the group info",
		},
		{
			name:    "announce",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupAnnounce, Actor: actor(person("Cy")), On: true},
			want:    "Cy restricted messages to admins",
		},
		{
			name:    "approval",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupApproval, Actor: actor(person("Cy")), On: true},
			want:    "Cy turned on approval for new members",
		},
		{
			name:    "invite link reset",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupInviteLink, Actor: actor(person("Cy"))},
			want:    "Cy reset the group invite link",
		},
		{
			name:    "group deleted",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeGroupDelete, Actor: actor(person("Cy"))},
			want:    "Cy deleted the group",
		},
		{
			name:    "disappearing on",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeEphemeral, Actor: actor(person("Cy")), On: true, Seconds: 7 * 24 * 60 * 60},
			want:    "Cy turned on disappearing messages (7 days)",
		},
		{
			name:    "disappearing off",
			payload: appstore.SystemPayload{Type: appstore.SystemTypeEphemeral, Actor: actor(person("Cy"))},
			want:    "Cy turned off disappearing messages",
		},
		{
			name: "security code",
			payload: appstore.SystemPayload{
				Type:         appstore.SystemTypeIdentityChange,
				Participants: []appstore.SystemParticipant{person("Ana")},
			},
			want: "Your security code with Ana changed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := systemSummary(tc.payload); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

// A forty-person add is one pill. Naming everybody would be an unbounded line,
// and one pill each would be forty rows nobody reads.
func TestSystemSummaryCountsThePeopleItCannotName(t *testing.T) {
	participants := make([]appstore.SystemParticipant, 0, 40)
	for i := range 40 {
		participants = append(participants, person(fmt.Sprintf("P%02d", i)))
	}
	got := systemSummary(appstore.SystemPayload{
		Type:         appstore.SystemTypeGroupJoin,
		Actor:        actor(person("Cy")),
		Participants: participants,
	})
	if got != "Cy added P00, P01, P02 and 37 others" {
		t.Fatalf("summary = %q", got)
	}
}

// One absurd push name must not be able to push the rest of the sentence off
// the line.
func TestSystemSummaryElidesALongName(t *testing.T) {
	long := person("Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	got := systemSummary(appstore.SystemPayload{
		Type:         appstore.SystemTypeGroupJoin,
		Actor:        actor(person("Cy")),
		Participants: []appstore.SystemParticipant{long},
	})
	if len([]rune(got)) > len("Cy added ")+systemNameMaxRunes {
		t.Fatalf("summary = %q, which is not elided", got)
	}
	if got[len(got)-3:] != "…" {
		t.Fatalf("summary = %q, want it to end in an ellipsis", got)
	}
}

func TestDisappearingTimerLabel(t *testing.T) {
	cases := map[uint32]string{
		0:                  "off",
		60 * 60:            "1 hour",
		6 * 60 * 60:        "6 hours",
		24 * 60 * 60:       "24 hours",
		7 * 24 * 60 * 60:   "7 days",
		90 * 24 * 60 * 60:  "90 days",
		30 * 60:            "30 minutes",
		365 * 24 * 60 * 60: "365 days",
	}
	for seconds, want := range cases {
		if got := disappearingTimerLabel(seconds); got != want {
			t.Fatalf("disappearingTimerLabel(%d) = %q, want %q", seconds, got, want)
		}
	}
}

const systemTestChat = "12345@g.us"

func parseTestJID(t *testing.T, raw string) types.JID {
	t.Helper()
	jid, err := types.ParseJID(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return jid
}

func joinPayload(names ...string) appstore.SystemPayload {
	participants := make([]appstore.SystemParticipant, 0, len(names))
	for _, name := range names {
		participants = append(participants, person(name))
	}
	return appstore.SystemPayload{
		Type:         appstore.SystemTypeGroupJoin,
		Actor:        actor(person("Cy")),
		Participants: participants,
	}
}

func systemRows(t *testing.T, client *Client) []appstore.Message {
	t.Helper()
	messages, err := client.store.ListMessages(context.Background(), systemTestChat, 100, "")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	rows := make([]appstore.Message, 0, len(messages))
	for _, message := range messages {
		if message.MediaKind == appstore.MediaKindSystem {
			rows = append(rows, message)
		}
	}
	return rows
}

// The server delivers a bulk add as a burst of separate events. They are one
// thing that happened, so they become one pill.
func TestSystemEventsCoalesceIntoOnePill(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	chatJID := parseTestJID(t, systemTestChat)

	at := time.Unix(1_700_000_000, 0)
	client.recordSystemEvent(ctx, chatJID, joinPayload("Ana"), at)
	client.recordSystemEvent(ctx, chatJID, joinPayload("Bo"), at.Add(2*time.Second))
	client.recordSystemEvent(ctx, chatJID, joinPayload("Cy2"), at.Add(4*time.Second))

	rows := systemRows(t, client)
	if len(rows) != 1 {
		t.Fatalf("got %d system rows, want them folded into 1", len(rows))
	}
	if rows[0].PayloadSummary != "Cy added Ana, Bo and Cy2" {
		t.Fatalf("summary = %q", rows[0].PayloadSummary)
	}
}

// Far enough apart, two adds are two things that happened.
func TestSystemEventsOutsideTheWindowStayApart(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	chatJID := parseTestJID(t, systemTestChat)

	at := time.Unix(1_700_000_000, 0)
	client.recordSystemEvent(ctx, chatJID, joinPayload("Ana"), at)
	client.recordSystemEvent(ctx, chatJID, joinPayload("Bo"), at.Add(appstore.SystemCoalesceWindow+time.Second))

	if rows := systemRows(t, client); len(rows) != 2 {
		t.Fatalf("got %d system rows, want 2", len(rows))
	}
}

// Two different kinds of event never fold together, however close they land.
func TestDifferentSystemEventsNeverFold(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	chatJID := parseTestJID(t, systemTestChat)

	at := time.Unix(1_700_000_000, 0)
	client.recordSystemEvent(ctx, chatJID, joinPayload("Ana"), at)
	leave := joinPayload("Bo")
	leave.Type = appstore.SystemTypeGroupLeave
	client.recordSystemEvent(ctx, chatJID, leave, at.Add(time.Second))

	if rows := systemRows(t, client); len(rows) != 2 {
		t.Fatalf("got %d system rows, want 2", len(rows))
	}
}

// A quiet row must leave the chat list exactly as it was: no reorder, no new
// preview, no badge. Somebody joining a group four hundred messages ago is not
// a reason to move that conversation to the top.
func TestQuietSystemRowLeavesTheChatListAlone(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	chatJID := parseTestJID(t, systemTestChat)

	if _, err := client.store.SaveTextMessage(ctx, appstore.TextMessageInput{
		ID:        systemTestChat + ":m1",
		ChatID:    systemTestChat,
		SenderID:  "ana@s.whatsapp.net",
		Text:      "see you there",
		Timestamp: time.Unix(1_700_000_000, 0),
		Direction: appstore.DirectionIncoming,
		IsGroup:   true,
	}); err != nil {
		t.Fatalf("save message: %v", err)
	}
	before, err := client.store.GetChat(ctx, systemTestChat)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}

	client.recordSystemEvent(ctx, chatJID, joinPayload("Bo"), time.Unix(1_700_000_100, 0))

	after, err := client.store.GetChat(ctx, systemTestChat)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if after.LastMessage != before.LastMessage {
		t.Fatalf("preview changed to %q: a quiet row must not replace it", after.LastMessage)
	}
	if after.LastMessageTime != before.LastMessageTime {
		t.Fatalf("last message time moved to %d: a quiet row must not reorder the list", after.LastMessageTime)
	}
	if after.UnreadCount != before.UnreadCount {
		t.Fatalf("unread went from %d to %d: a quiet row must not raise a badge", before.UnreadCount, after.UnreadCount)
	}
}

// An event that named us is the exception, and the only one: being added to a
// group is something you want to be told about.
func TestSystemRowNamingUsIsLoud(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	chatJID := parseTestJID(t, systemTestChat)

	payload := joinPayload()
	payload.Participants = []appstore.SystemParticipant{me()}
	payload.AboutSelf = true
	client.recordSystemEvent(ctx, chatJID, payload, time.Unix(1_700_000_000, 0))

	chat, err := client.store.GetChat(ctx, systemTestChat)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if chat.LastMessage != "Cy added you" {
		t.Fatalf("preview = %q, want the event to lead the chat list", chat.LastMessage)
	}
	if chat.UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1", chat.UnreadCount)
	}
}

// The same event delivered twice is one pill, not two: the id is derived from
// what happened rather than from when we heard about it.
func TestRepeatedSystemEventDoesNotDouble(t *testing.T) {
	client := newMediaIngestClient(t)
	ctx := context.Background()
	chatJID := parseTestJID(t, systemTestChat)

	at := time.Unix(1_700_000_000, 0)
	client.recordSystemEvent(ctx, chatJID, joinPayload("Ana"), at)
	client.recordSystemEvent(ctx, chatJID, joinPayload("Ana"), at)

	rows := systemRows(t, client)
	if len(rows) != 1 {
		t.Fatalf("got %d system rows, want 1", len(rows))
	}
	if rows[0].PayloadSummary != "Cy added Ana" {
		t.Fatalf("summary = %q: a repeat must not name anybody twice", rows[0].PayloadSummary)
	}
}
