package whatsapp

import (
	"fmt"
	"testing"
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
