package whatsapp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	appstore "whatevrd/internal/store"

	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
)

// systemNameLimit is how many people a pill names before it counts the rest.
// Three is what fits on one line at a readable width; the rest becomes "and 37
// others", which is the only rendering of a forty-person add that is neither a
// wall of pills nor an unbounded line.
const systemNameLimit = 3

// systemParticipant resolves one JID into the person a pill will name.
func (d *Decoder) systemParticipant(ctx context.Context, jid types.JID) appstore.SystemParticipant {
	if jid.IsEmpty() {
		return appstore.SystemParticipant{}
	}
	self := d.names.Own(jid)
	name := ""
	if !self {
		name = strings.TrimPrefix(d.names.Name(jid), "~")
	}
	return appstore.SystemParticipant{JID: jid.ToNonAD().String(), Name: name, Self: self}
}

func (d *Decoder) systemParticipants(ctx context.Context, jids []types.JID) []appstore.SystemParticipant {
	participants := make([]appstore.SystemParticipant, 0, len(jids))
	for _, jid := range jids {
		if jid.IsEmpty() {
			continue
		}
		participants = append(participants, d.systemParticipant(ctx, jid))
	}
	return participants
}

// systemActor resolves who did it. A nil sender is normal: the server reports a
// join through an invite link with no author, because nobody added them.
func (d *Decoder) systemActor(ctx context.Context, sender *types.JID) *appstore.SystemParticipant {
	if sender == nil || sender.IsEmpty() {
		return nil
	}
	actor := d.systemParticipant(ctx, *sender)
	return &actor
}

func systemNamesSelf(participants []appstore.SystemParticipant) bool {
	for _, participant := range participants {
		if participant.Self {
			return true
		}
	}
	return false
}

// elideName bounds one name so a sentence built around it stays a sentence.
func elideName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) <= systemNameMaxRunes {
		return name
	}
	return strings.TrimSpace(string(runes[:systemNameMaxRunes-1])) + "…"
}

// systemNameList renders the people a pill names: up to three of them, then a
// count. `capitalized` decides whether a leading self reads "You" or "you",
// which is the difference between "You joined" and "Ana added you".
func systemNameList(participants []appstore.SystemParticipant, capitalized bool) string {
	names := make([]string, 0, len(participants))
	for _, participant := range participants {
		switch {
		case participant.Self && capitalized && len(names) == 0:
			names = append(names, "You")
		case participant.Self:
			names = append(names, "you")
		case participant.Name != "":
			names = append(names, elideName(participant.Name))
		default:
			names = append(names, "someone")
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	if len(names) <= systemNameLimit {
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
	rest := len(names) - systemNameLimit
	return fmt.Sprintf("%s and %d others", strings.Join(names[:systemNameLimit], ", "), rest)
}

// actorName is the subject of the sentence, or "" when the event had no author.
func actorName(payload appstore.SystemPayload) string {
	if payload.Actor == nil {
		return ""
	}
	if payload.Actor.Self {
		return "You"
	}
	if payload.Actor.Name != "" {
		return elideName(payload.Actor.Name)
	}
	return ""
}

// actorDidSomethingTo builds "<actor> <verb> <people>", falling back to a
// sentence with no subject when the event named no author.
func actorDidSomethingTo(payload appstore.SystemPayload, verb, authorless string) string {
	people := systemNameList(payload.Participants, false)
	if people == "" {
		return ""
	}
	actor := actorName(payload)
	if actor == "" {
		return fmt.Sprintf("%s %s", systemNameList(payload.Participants, true), authorless)
	}
	return fmt.Sprintf("%s %s %s", actor, verb, people)
}

// actorDid builds "<actor> <did something>", with a subjectless fallback for an
// event the server reported with no author.
func actorDid(payload appstore.SystemPayload, did, authorless string) string {
	if actor := actorName(payload); actor != "" {
		return actor + " " + did
	}
	return authorless
}

// adminChangeSummary builds a promotion or demotion, which needs the people in
// the middle of the sentence rather than at the end of it.
func adminChangeSummary(payload appstore.SystemPayload, withActor, authorless string) string {
	people := systemNameList(payload.Participants, false)
	if people == "" {
		return ""
	}
	if actor := actorName(payload); actor != "" {
		return actor + " " + fmt.Sprintf(withActor, people)
	}
	return fmt.Sprintf(authorless, systemNameList(payload.Participants, true))
}

// systemSummary is the whole line a pill reads. It is also the row's preview and
// its wire `fallback`, which is why it is a full sentence rather than a label.
func systemSummary(payload appstore.SystemPayload) string {
	switch payload.Type {
	case appstore.SystemTypeGroupJoin:
		// Somebody adding themselves is a join; somebody adding other people is
		// an add. Both arrive on the same event, told apart by whether the
		// author is the person who appeared.
		if soleActorIsParticipant(payload) {
			return systemNameList(payload.Participants, true) + " joined"
		}
		return actorDidSomethingTo(payload, "added", "joined")
	case appstore.SystemTypeGroupLeave:
		if soleActorIsParticipant(payload) {
			return systemNameList(payload.Participants, true) + " left"
		}
		return actorDidSomethingTo(payload, "removed", "left")
	case appstore.SystemTypeGroupPromote:
		return adminChangeSummary(payload, "made %s an admin", "%s is now an admin")
	case appstore.SystemTypeGroupDemote:
		return adminChangeSummary(payload, "removed %s as admin", "%s is no longer an admin")
	case appstore.SystemTypeGroupName:
		if payload.Value == "" {
			return actorDid(payload, "changed the group name", "The group name changed")
		}
		return actorDid(payload,
			fmt.Sprintf("changed the group name to %q", elideName(payload.Value)),
			fmt.Sprintf("The group name changed to %q", elideName(payload.Value)))
	case appstore.SystemTypeGroupTopic:
		if payload.Value == "" {
			return actorDid(payload, "removed the group description", "The group description was removed")
		}
		return actorDid(payload, "changed the group description", "The group description changed")
	case appstore.SystemTypeGroupPhoto:
		if payload.On {
			return actorDid(payload, "changed the group photo", "The group photo changed")
		}
		return actorDid(payload, "removed the group photo", "The group photo was removed")
	case appstore.SystemTypeGroupLocked:
		if payload.On {
			return actorDid(payload, "restricted editing the group info to admins", "Only admins can edit the group info")
		}
		return actorDid(payload, "allowed everyone to edit the group info", "Everyone can edit the group info")
	case appstore.SystemTypeGroupAnnounce:
		if payload.On {
			return actorDid(payload, "restricted messages to admins", "Only admins can send messages")
		}
		return actorDid(payload, "allowed everyone to send messages", "Everyone can send messages")
	case appstore.SystemTypeGroupApproval:
		if payload.On {
			return actorDid(payload, "turned on approval for new members", "New members now need approval")
		}
		return actorDid(payload, "turned off approval for new members", "New members no longer need approval")
	case appstore.SystemTypeGroupInviteLink:
		return actorDid(payload, "reset the group invite link", "The group invite link was reset")
	case appstore.SystemTypeGroupLink:
		return systemLinkSummary(payload, true)
	case appstore.SystemTypeGroupUnlink:
		return systemLinkSummary(payload, false)
	case appstore.SystemTypeGroupDelete:
		return actorDid(payload, "deleted the group", "The group was deleted")
	case appstore.SystemTypeEphemeral:
		if payload.On {
			return actorDid(payload,
				"turned on disappearing messages ("+disappearingTimerLabel(payload.Seconds)+")",
				"Disappearing messages are on ("+disappearingTimerLabel(payload.Seconds)+")")
		}
		return actorDid(payload, "turned off disappearing messages", "Disappearing messages are off")
	case appstore.SystemTypeIdentityChange:
		who := systemNameList(payload.Participants, false)
		if who == "" {
			return "Your security code changed"
		}
		return "Your security code with " + who + " changed"
	}
	return ""
}

// soleActorIsParticipant reports the case where the author of a membership
// change is the only person it named: somebody joining or leaving under their
// own steam, rather than being added or removed by anyone.
func soleActorIsParticipant(payload appstore.SystemPayload) bool {
	if payload.Actor == nil {
		return true
	}
	return len(payload.Participants) == 1 && payload.Participants[0].JID == payload.Actor.JID
}

func systemLinkSummary(payload appstore.SystemPayload, linked bool) string {
	target := elideName(payload.Value)
	switch payload.Detail {
	case string(types.GroupLinkChangeTypeParent):
		if linked {
			return actorDid(payload, "added this group to a community", "This group joined a community")
		}
		return actorDid(payload, "removed this group from its community", "This group left its community")
	case string(types.GroupLinkChangeTypeSub):
		if target == "" {
			target = "a group"
		}
		if linked {
			return actorDid(payload, "added "+target+" to this community", target+" joined this community")
		}
		return actorDid(payload, "removed "+target+" from this community", target+" left this community")
	}
	if linked {
		return actorDid(payload, "linked this group", "This group was linked")
	}
	return actorDid(payload, "unlinked this group", "This group was unlinked")
}

// disappearingTimerLabel spells a timer the way WhatsApp offers it.
func disappearingTimerLabel(seconds uint32) string {
	switch {
	case seconds == 0:
		return "off"
	case seconds%(24*60*60) == 0:
		days := seconds / (24 * 60 * 60)
		if days == 1 {
			return "24 hours"
		}
		if days%7 == 0 {
			weeks := days / 7
			if weeks == 1 {
				return "7 days"
			}
			return fmt.Sprintf("%d days", days)
		}
		return fmt.Sprintf("%d days", days)
	case seconds%(60*60) == 0:
		hours := seconds / (60 * 60)
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	default:
		return fmt.Sprintf("%d minutes", seconds/60)
	}
}

// historyStubSystemPayload turns a backfilled group stub into the same pill the
// live path builds from events.GroupInfo. A stub carries no message, so it
// matched no builder and wrote no row: a group pulled out of backfill had no
// record of who joined, who left, or when the subject changed.
//
// Stub parameters are positional and their meaning depends on the type: JIDs
// for the participant events, the new text for a subject or description.
func (d *Decoder) historyStubSystemPayload(ctx context.Context, webMsg *waWeb.WebMessageInfo) (appstore.SystemPayload, time.Time, bool) {
	if webMsg == nil || webMsg.MessageStubType == nil {
		return appstore.SystemPayload{}, time.Time{}, false
	}

	params := webMsg.GetMessageStubParameters()
	timestamp := time.Unix(int64(webMsg.GetMessageTimestamp()), 0)

	var payload appstore.SystemPayload
	switch webMsg.GetMessageStubType() {
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD, waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE,
		waWeb.WebMessageInfo_GROUP_PARTICIPANT_LINKED_GROUP_JOIN:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupJoin, Participants: d.systemParticipants(ctx, parseStubJIDs(params))}
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_REMOVE, waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupLeave, Participants: d.systemParticipants(ctx, parseStubJIDs(params))}
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupPromote, Participants: d.systemParticipants(ctx, parseStubJIDs(params))}
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupDemote, Participants: d.systemParticipants(ctx, parseStubJIDs(params))}
	case waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT, waWeb.WebMessageInfo_GROUP_CREATE:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupName, Value: strings.TrimSpace(firstStubParam(params))}
	case waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupTopic, Value: strings.TrimSpace(firstStubParam(params))}
	case waWeb.WebMessageInfo_GROUP_CHANGE_ICON:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupPhoto, On: true}
	case waWeb.WebMessageInfo_GROUP_CHANGE_INVITE_LINK:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupInviteLink}
	case waWeb.WebMessageInfo_GROUP_CHANGE_RESTRICT:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupLocked, On: stubFlagIsOn(params)}
	case waWeb.WebMessageInfo_GROUP_CHANGE_ANNOUNCE:
		payload = appstore.SystemPayload{Type: appstore.SystemTypeGroupAnnounce, On: stubFlagIsOn(params)}
	case waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING:
		seconds := stubSeconds(params)
		payload = appstore.SystemPayload{Type: appstore.SystemTypeEphemeral, On: seconds > 0, Seconds: seconds}
	default:
		return appstore.SystemPayload{}, time.Time{}, false
	}

	if participant := strings.TrimSpace(webMsg.GetParticipant()); participant != "" {
		if jid, err := types.ParseJID(participant); err == nil {
			payload.Actor = d.systemActor(ctx, &jid)
		}
	}
	payload.AboutSelf = systemNamesSelf(payload.Participants)
	if systemSummary(payload) == "" {
		return appstore.SystemPayload{}, time.Time{}, false
	}
	return payload, timestamp, true
}

func parseStubJIDs(params []string) []types.JID {
	jids := make([]types.JID, 0, len(params))
	for _, param := range params {
		jid, err := types.ParseJID(strings.TrimSpace(param))
		if err != nil || jid.IsEmpty() {
			continue
		}
		jids = append(jids, jid)
	}
	return jids
}

func firstStubParam(params []string) string {
	if len(params) == 0 {
		return ""
	}
	return params[0]
}

// stubFlagIsOn reads the on/off parameter WhatsApp sends as the string "on" or
// "off" for the lock and announce toggles.
func stubFlagIsOn(params []string) bool {
	return strings.EqualFold(strings.TrimSpace(firstStubParam(params)), "on")
}

func stubSeconds(params []string) uint32 {
	seconds, err := strconv.ParseUint(strings.TrimSpace(firstStubParam(params)), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(seconds)
}

// one long push name must not push the rest of a system line off screen
const systemNameMaxRunes = 24
