package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// System events: the things a chat does to itself.
//
// Somebody joins, somebody is made an admin, the subject changes, the
// disappearing timer moves, a security code changes. WhatsApp delivers all of
// these as events rather than as messages, and until now every one of them
// mutated state and wrote nothing, so a group's whole history of who arrived and
// who left simply did not exist in the transcript.
//
// Each one becomes a row of kind `system`, rendered as a centered pill. The
// sentence is composed here rather than on the wire's far side: a frontend that
// had to decide whether an event says "Ana joined" or "You added Ana" would be
// reimplementing the decision, and getting a different answer per frontend.

// systemNameLimit is how many people a pill names before it counts the rest.
// Three is what fits on one line at a readable width; the rest becomes "and 37
// others", which is the only rendering of a forty-person add that is neither a
// wall of pills nor an unbounded line.
const systemNameLimit = 3

// systemNameMaxRunes elides one person's name. A pill is a fixed sentence with
// names dropped into it, so one absurdly long push name must not be able to
// push the rest of the sentence off the line.
const systemNameMaxRunes = 24

// recordSystemEvent stores one system event and publishes it, folding it into
// the pill above when that pill is the same kind of event from the same person
// moments ago.
func (c *Client) recordSystemEvent(ctx context.Context, chatJID types.JID, payload appstore.SystemPayload, timestamp time.Time) {
	if chatJID.IsEmpty() || payload.Type == "" {
		return
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	chatID := chatJID.String()

	coalesceWith, previous, folds := c.systemEventToFoldInto(ctx, chatID, payload, timestamp)
	if folds {
		payload = mergeSystemPayloads(previous, payload)
	} else {
		coalesceWith = ""
	}

	actorID := chatID
	actorName := ""
	if payload.Actor != nil {
		if payload.Actor.JID != "" {
			actorID = payload.Actor.JID
		}
		actorName = payload.Actor.Name
	}

	saved, err := c.store.SaveSystemMessage(ctx, appstore.SystemMessageInput{
		ChatID:       chatID,
		IsGroup:      chatJID.Server == types.GroupServer,
		SenderID:     actorID,
		SenderName:   actorName,
		Timestamp:    timestamp,
		Summary:      systemSummary(payload),
		Payload:      payload,
		Loud:         payload.AboutSelf,
		CoalesceWith: coalesceWith,
	})
	if err != nil {
		c.log.Warnf("Failed to store system event %s in %s: %v", payload.Type, chatID, err)
		return
	}
	if !saved.Inserted {
		// The same event again, already on the row it wrote the first time.
		return
	}
	if coalesceWith != "" {
		// The pill above changed rather than a new one appearing, so this is an
		// update: publishing it as new would move the reader's transcript.
		c.daemon.PublishMessageUpdated(toDaemonMessage(saved.Message))
		return
	}
	c.daemon.PublishNewMessage(toDaemonMessage(saved.Message), toDaemonChat(saved.Chat))
}

// systemEventToFoldInto decides whether the event in hand belongs on the pill
// above rather than on one of its own, and yields that pill's id and payload.
//
// It folds only when the newest row in the chat is a system row of the same
// type, by the same person, within the window. Requiring it to be the *newest*
// row is what keeps the transcript in order: an event that arrives after
// somebody has spoken starts a new pill, because folding it upward would move
// it above words that came first.
func (c *Client) systemEventToFoldInto(ctx context.Context, chatID string, payload appstore.SystemPayload, timestamp time.Time) (string, appstore.SystemPayload, bool) {
	newest, err := c.store.NewestSystemMessage(ctx, chatID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			c.log.Warnf("Failed to look for a system row to fold into in %s: %v", chatID, err)
		}
		return "", appstore.SystemPayload{}, false
	}
	previous := appstore.DecodePayload(newest.PayloadJSON).System
	if previous == nil || previous.Type != payload.Type {
		return "", appstore.SystemPayload{}, false
	}
	if systemActorJID(previous) != systemActorJID(&payload) {
		return "", appstore.SystemPayload{}, false
	}
	gap := timestamp.Sub(time.Unix(newest.TimestampUnix, 0))
	if gap < 0 {
		gap = -gap
	}
	if gap > appstore.SystemCoalesceWindow {
		return "", appstore.SystemPayload{}, false
	}
	return newest.ID, *previous, true
}

func systemActorJID(payload *appstore.SystemPayload) string {
	if payload == nil || payload.Actor == nil {
		return ""
	}
	return payload.Actor.JID
}

// mergeSystemPayloads folds a repeat of an event into the one already stored:
// the people are appended, and everything else is taken from the newer event, so
// a group renamed twice in a minute leaves one pill reading the final name.
func mergeSystemPayloads(previous, next appstore.SystemPayload) appstore.SystemPayload {
	merged := next
	merged.Participants = append([]appstore.SystemParticipant{}, previous.Participants...)
	for _, participant := range next.Participants {
		if !merged.NamesParticipant(participant.JID) {
			merged.Participants = append(merged.Participants, participant)
		}
	}
	merged.AboutSelf = previous.AboutSelf || next.AboutSelf
	return merged
}

// systemParticipant resolves one JID into the person a pill will name.
func (c *Client) systemParticipant(ctx context.Context, jid types.JID) appstore.SystemParticipant {
	if jid.IsEmpty() {
		return appstore.SystemParticipant{}
	}
	self := c.isSelfJID(jid)
	name := ""
	if !self {
		name = strings.TrimPrefix(c.senderName(ctx, jid), "~")
	}
	return appstore.SystemParticipant{JID: jid.ToNonAD().String(), Name: name, Self: self}
}

func (c *Client) systemParticipants(ctx context.Context, jids []types.JID) []appstore.SystemParticipant {
	participants := make([]appstore.SystemParticipant, 0, len(jids))
	for _, jid := range jids {
		if jid.IsEmpty() {
			continue
		}
		participants = append(participants, c.systemParticipant(ctx, jid))
	}
	return participants
}

// systemActor resolves who did it. A nil sender is normal: the server reports a
// join through an invite link with no author, because nobody added them.
func (c *Client) systemActor(ctx context.Context, sender *types.JID) *appstore.SystemParticipant {
	if sender == nil || sender.IsEmpty() {
		return nil
	}
	actor := c.systemParticipant(ctx, *sender)
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

// recordGroupInfoEvents turns one events.GroupInfo into the pills it deserves.
// A single event can carry several independent changes (a rename and a
// promotion arrive together on a resync), and each is its own sentence.
func (c *Client) recordGroupInfoEvents(ctx context.Context, chatJID types.JID, evt *events.GroupInfo) {
	actor := c.systemActor(ctx, evt.Sender)
	timestamp := evt.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	emit := func(payload appstore.SystemPayload) {
		payload.Actor = actor
		payload.AboutSelf = systemNamesSelf(payload.Participants)
		if systemSummary(payload) == "" {
			return
		}
		c.recordSystemEvent(ctx, chatJID, payload, timestamp)
	}

	if len(evt.Join) > 0 {
		emit(appstore.SystemPayload{
			Type:         appstore.SystemTypeGroupJoin,
			Participants: c.systemParticipants(ctx, evt.Join),
		})
	}
	if len(evt.Leave) > 0 {
		emit(appstore.SystemPayload{
			Type:         appstore.SystemTypeGroupLeave,
			Participants: c.systemParticipants(ctx, evt.Leave),
		})
	}
	if len(evt.Promote) > 0 {
		emit(appstore.SystemPayload{
			Type:         appstore.SystemTypeGroupPromote,
			Participants: c.systemParticipants(ctx, evt.Promote),
		})
	}
	if len(evt.Demote) > 0 {
		emit(appstore.SystemPayload{
			Type:         appstore.SystemTypeGroupDemote,
			Participants: c.systemParticipants(ctx, evt.Demote),
		})
	}
	if evt.Name != nil {
		emit(appstore.SystemPayload{Type: appstore.SystemTypeGroupName, Value: strings.TrimSpace(evt.Name.Name)})
	}
	if evt.Topic != nil {
		value := strings.TrimSpace(evt.Topic.Topic)
		if evt.Topic.TopicDeleted {
			value = ""
		}
		emit(appstore.SystemPayload{Type: appstore.SystemTypeGroupTopic, Value: value})
	}
	if evt.Locked != nil {
		emit(appstore.SystemPayload{Type: appstore.SystemTypeGroupLocked, On: evt.Locked.IsLocked})
	}
	if evt.Announce != nil {
		emit(appstore.SystemPayload{Type: appstore.SystemTypeGroupAnnounce, On: evt.Announce.IsAnnounce})
	}
	if evt.Ephemeral != nil {
		emit(appstore.SystemPayload{
			Type:    appstore.SystemTypeEphemeral,
			On:      evt.Ephemeral.IsEphemeral,
			Seconds: evt.Ephemeral.DisappearingTimer,
		})
	}
	if evt.MembershipApprovalMode != nil {
		emit(appstore.SystemPayload{
			Type: appstore.SystemTypeGroupApproval,
			On:   evt.MembershipApprovalMode.IsJoinApprovalRequired,
		})
	}
	if evt.NewInviteLink != nil {
		emit(appstore.SystemPayload{Type: appstore.SystemTypeGroupInviteLink})
	}
	if evt.Link != nil {
		emit(appstore.SystemPayload{
			Type:   appstore.SystemTypeGroupLink,
			Value:  strings.TrimSpace(evt.Link.Group.Name),
			Detail: string(evt.Link.Type),
		})
	}
	if evt.Unlink != nil {
		emit(appstore.SystemPayload{
			Type:   appstore.SystemTypeGroupUnlink,
			Value:  strings.TrimSpace(evt.Unlink.Group.Name),
			Detail: string(evt.Unlink.Type),
		})
	}
	if evt.Delete != nil && evt.Delete.Deleted {
		emit(appstore.SystemPayload{
			Type:   appstore.SystemTypeGroupDelete,
			Detail: evt.Delete.DeleteReason,
		})
	}
}

// handleEphemeralSetting intercepts the control message a phone sends when
// somebody changes the disappearing-message timer in a one-to-one chat. It is
// not a message anybody wrote, so it becomes a pill rather than a bubble.
//
// The group case never comes through here: a group's timer arrives on
// events.GroupInfo, which recordGroupInfoEvents already covers.
func (c *Client) handleEphemeralSetting(ctx context.Context, evt *events.Message) bool {
	if evt == nil || evt.Message == nil {
		return false
	}
	protocol := evt.Message.GetProtocolMessage()
	if protocol == nil || protocol.GetType() != waE2E.ProtocolMessage_EPHEMERAL_SETTING {
		return false
	}

	chatJID := c.normalizeJIDForChat(ctx, evt.Info.Chat)
	if chatJID.IsEmpty() {
		return true
	}
	sender := evt.Info.Sender
	seconds := protocol.GetEphemeralExpiration()
	payload := appstore.SystemPayload{
		Type:    appstore.SystemTypeEphemeral,
		Actor:   c.systemActor(ctx, &sender),
		On:      seconds > 0,
		Seconds: seconds,
	}
	c.recordSystemEvent(ctx, chatJID, payload, evt.Info.Timestamp)
	return true
}

// recordIdentityChange writes the security-code pill into the chat with the
// person whose code changed. It is always loud: it is about us by definition,
// and it is the one system event a reader may want to act on.
func (c *Client) recordIdentityChange(ctx context.Context, evt *events.IdentityChange) {
	if evt == nil || evt.JID.IsEmpty() {
		return
	}
	chatJID := c.normalizeJIDForChat(ctx, evt.JID.ToNonAD())
	if chatJID.IsEmpty() || chatJID.Server == types.GroupServer {
		return
	}
	payload := appstore.SystemPayload{
		Type:         appstore.SystemTypeIdentityChange,
		Participants: []appstore.SystemParticipant{c.systemParticipant(ctx, chatJID)},
		AboutSelf:    true,
	}
	c.recordSystemEvent(ctx, chatJID, payload, evt.Timestamp)
}

// recordGroupPhotoChange writes the pill for a changed group photo. The same
// event fires for a contact's own profile picture, which is not something that
// happened in any chat and gets no row.
func (c *Client) recordGroupPhotoChange(ctx context.Context, evt *events.Picture) {
	if evt == nil || evt.JID.IsEmpty() || evt.JID.Server != types.GroupServer {
		return
	}
	actor := c.systemActor(ctx, &evt.Author)
	c.recordSystemEvent(ctx, evt.JID, appstore.SystemPayload{
		Type:  appstore.SystemTypeGroupPhoto,
		Actor: actor,
		On:    !evt.Remove,
	}, evt.Timestamp)
}
