package wa

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// Group invites.
//
// A GroupInviteMessage carries the sender's own snapshot of a group: its jid, a
// one-shot code, an expiry, a name and a thumbnail. That snapshot is whatever
// their client happened to hold when they hit share, so the name can be stale
// and the picture can be gone.
//
// The daemon therefore resolves the code through GetGroupInfoFromInvite and
// keeps both: the sender's copy so the card always has something to show, and
// the resolved facts (subject, topic, member count, and whether we are already
// a member) because they are the truth. The membership answer is the one that
// changes what the card *does*: for a group we are already in there is nothing
// to join, and the button opens the chat instead.

// groupInviteResolveTimeout bounds the lookup. It runs off the ingest path, so
// a slow answer costs nothing but a card that stays on the sender's copy for a
// moment; a hung one must not pin a goroutine for the session's lifetime.
const groupInviteResolveTimeout = 20 * time.Second

// groupInviteJoinTimeout bounds the join. Shorter than the resolve, because
// somebody is watching this one: a button that has not answered in fifteen
// seconds has failed, whatever the server eventually thinks.
const groupInviteJoinTimeout = 15 * time.Second

func (c *Client) groupInviteMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	invite := evt.Message.GetGroupInviteMessage()
	if invite == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := c.mediaInputBase(ctx, evt, opts, invite.GetCaption(), invite.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.GroupInvitePayload{
		GroupJID:  strings.TrimSpace(invite.GetGroupJID()),
		Code:      strings.TrimSpace(invite.GetInviteCode()),
		ExpiresAt: invite.GetInviteExpiration(),
		Name:      strings.TrimSpace(invite.GetGroupName()),
		Caption:   strings.TrimSpace(invite.GetCaption()),
		// The thumbnail is saved with the group-invite suffix rather than the
		// generic one so it cannot collide with a row's media thumbnail if this
		// message id is ever reused for another kind.
		PhotoPath: c.saveMessageThumbnailWithExtension(chatID, base.ID, invite.GetJPEGThumbnail(), ".invite.jpg"),
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{GroupInvite: payload})
	if err != nil {
		c.log.Warnf("Failed to encode group invite payload for %s: %v", base.ID, err)
		return appstore.MediaMessageInput{}, false
	}

	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindGroupInvite,
		PayloadSummary:   payload.DisplayName(),
	}, true
}

// maybeResolveGroupInvite starts the lookup that turns the sender's snapshot
// into the real group, in the background: ingest must not wait on a round trip.
//
// An expired invite is skipped, which is also what bounds this. A code cannot
// be resolved once it has lapsed, so a history sync full of old invites asks
// WhatsApp nothing at all, and only the handful that are still live cost a
// query.
func (c *Client) maybeResolveGroupInvite(ctx context.Context, message appstore.Message) {
	payload := appstore.DecodePayload(message.PayloadJSON).GroupInvite
	if payload == nil || payload.Code == "" || payload.GroupJID == "" {
		return
	}
	if groupInviteExpired(payload, time.Now()) {
		return
	}
	c.log.Debugf("Resolving group invite %s for %s", message.ID, payload.GroupJID)
	c.spawn(func(ctx context.Context) { c.resolveGroupInvite(ctx, message.ID) })
}

// resolveGroupInvite asks WhatsApp what the invite code actually points at and
// folds the answer back into the row.
func (c *Client) resolveGroupInvite(ctx context.Context, messageID string) {
	// Every way out of this prologue says why. A background job that gives up
	// in silence is a card stuck on the sender's copy with nothing anywhere to
	// explain it, which is exactly how long this took to diagnose the once.
	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		c.log.Debugf("Not resolving group invite %s: not connected", messageID)
		return
	}
	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		c.log.Warnf("Failed to read group invite %s back: %v", messageID, err)
		return
	}
	payload := appstore.DecodePayload(message.PayloadJSON).GroupInvite
	if payload == nil {
		c.log.Warnf("Group invite %s stored no invite payload", messageID)
		return
	}
	groupJID, inviter, ok := c.groupInviteParties(message, payload)
	if !ok {
		c.log.Debugf("Group invite %s names no resolvable group or inviter", messageID)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, groupInviteResolveTimeout)
	defer cancel()

	// Two different queries describe the group, and which one applies depends
	// on which side of the membership line we are on. Plain group info is the
	// members' query and is the one that answers "am I in this": it is refused
	// for a group we are not in. The invite query is the outsiders' one.
	//
	// The order matters and is not a preference. WhatsApp does not reply to an
	// add_request for a group the asker is already in: not an error, no reply
	// at all, so asking that first parks the lookup until it times out and the
	// card sits on the sender's copy for twenty seconds before giving up on a
	// question the other query answers at once.
	if info, err := client.GetGroupInfo(ctx, groupJID); err == nil && info != nil {
		c.log.Debugf("Group invite %s is for %s, which we are already in", messageID, groupJID)
		payload.Joined = true
		applyResolvedGroupInfo(payload, info)
		c.saveGroupInvitePayload(ctx, messageID, payload)
		return
	}

	c.log.Debugf("Asking WhatsApp about invite %s to %s from %s", payload.Code, groupJID, inviter)
	info, err := client.GetGroupInfoFromInvite(ctx, groupJID, inviter, payload.Code, payload.ExpiresAt)
	if err != nil {
		if ctx.Err() != nil {
			c.log.Debugf("Gave up resolving group invite %s: %v", messageID, ctx.Err())
			return
		}
		// A revoked or already-used code is the common failure and is worth
		// saying: the card can tell the reader the door is closed rather than
		// offering a button that will fail.
		c.log.Debugf("Failed to resolve group invite %s: %v", messageID, err)
		payload.ResolveError = err.Error()
		c.saveGroupInvitePayload(ctx, messageID, payload)
		return
	}
	if info == nil {
		c.log.Debugf("Group invite %s resolved to nothing", messageID)
		return
	}

	payload.ResolveError = ""
	payload.Joined = groupInfoIncludesUs(info, c.ownParticipantJIDs())
	applyResolvedGroupInfo(payload, info)
	c.saveGroupInvitePayload(ctx, messageID, payload)
}

// applyResolvedGroupInfo folds a resolved group into the invite's payload. It
// deliberately does not touch Joined: which query the info came from is what
// says whether we are a member, and only the caller knows that.
func applyResolvedGroupInfo(payload *appstore.GroupInvitePayload, info *types.GroupInfo) {
	payload.ResolvedAt = time.Now().Unix()
	payload.Subject = strings.TrimSpace(info.Name)
	payload.Topic = strings.TrimSpace(info.Topic)
	payload.MemberCount = len(info.Participants)
	if payload.MemberCount == 0 {
		payload.MemberCount = info.ParticipantCount
	}
}

// groupInviteParties resolves the two jids the whatsmeow calls need: the group
// being offered, and whoever offered it. The inviter for a message we sent
// ourselves is us, which is what makes an invite we forwarded resolvable.
func (c *Client) groupInviteParties(message appstore.Message, payload *appstore.GroupInvitePayload) (types.JID, types.JID, bool) {
	groupJID, err := types.ParseJID(payload.GroupJID)
	if err != nil || groupJID.Server != types.GroupServer {
		return types.JID{}, types.JID{}, false
	}

	senderID := message.SenderID
	if message.Direction == appstore.DirectionOutgoing || senderID == "me" || senderID == "" {
		client := c.currentClient()
		if client == nil {
			return types.JID{}, types.JID{}, false
		}
		own := client.Store.GetJID()
		if own.IsEmpty() {
			return types.JID{}, types.JID{}, false
		}
		return groupJID, own.ToNonAD(), true
	}
	inviter, err := types.ParseJID(senderID)
	if err != nil {
		return types.JID{}, types.JID{}, false
	}
	return groupJID, inviter.ToNonAD(), true
}

func (c *Client) saveGroupInvitePayload(ctx context.Context, messageID string, payload *appstore.GroupInvitePayload) {
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{GroupInvite: payload})
	if err != nil {
		c.log.Warnf("Failed to encode resolved group invite for %s: %v", messageID, err)
		return
	}
	message, err := c.store.UpdateMessagePayload(ctx, messageID, encoded, payload.DisplayName())
	if err != nil {
		c.log.Warnf("Failed to store resolved group invite for %s: %v", messageID, err)
		return
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(message))
}

// alreadyInGroup answers from what we already know, with no round trip: the
// participant list the daemon keeps for a group it has had reason to look at.
// A false answer means "not as far as we know", not "no": the list is filled
// lazily, so a group we have never needed the members of has none stored.
func (c *Client) alreadyInGroup(ctx context.Context, groupJID types.JID) bool {
	participants, err := c.store.ListGroupParticipants(ctx, groupJID.String())
	if err != nil || len(participants) == 0 {
		return false
	}
	own := c.ownParticipantJIDs()
	for _, participant := range participants {
		if own[participant] {
			return true
		}
	}
	return false
}

// groupInfoIncludesUs looks for the local account in a resolved member list.
// Both jid forms are checked because a group addresses its members by phone
// number or by LID depending on how it was created.
func groupInfoIncludesUs(info *types.GroupInfo, own map[string]bool) bool {
	if info == nil || len(own) == 0 {
		return false
	}
	for _, participant := range info.Participants {
		if own[participant.JID.ToNonAD().String()] {
			return true
		}
		if !participant.PhoneNumber.IsEmpty() && own[participant.PhoneNumber.ToNonAD().String()] {
			return true
		}
		if !participant.LID.IsEmpty() && own[participant.LID.ToNonAD().String()] {
			return true
		}
	}
	return false
}

// groupInviteExpired reports whether the code has lapsed. An invite with no
// stated expiry never does.
func groupInviteExpired(payload *appstore.GroupInvitePayload, now time.Time) bool {
	return payload != nil && payload.ExpiresAt > 0 && now.Unix() >= payload.ExpiresAt
}

// JoinGroupInvite accepts an invite and answers with the chat to open.
//
// Joining a group we are already in is not an error and not a network call: it
// is the Open case, and the honest answer is the chat id we already have. That
// is why the command returns one either way.
func (c *Client) JoinGroupInvite(ctx context.Context, messageID string) (string, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", app.NewCommandError(app.CommandErrorInvalidArgument, "message_id is required")
	}

	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return "", app.NewCommandError(app.CommandErrorNotLoggedIn, "not logged in")
	}

	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return "", err
	}
	if message.MediaKind != appstore.MediaKindGroupInvite {
		return "", app.NewCommandError(app.CommandErrorInvalidArgument, "message %s is not a group invite", messageID)
	}
	payload := appstore.DecodePayload(message.PayloadJSON).GroupInvite
	if payload == nil || payload.Code == "" {
		return "", app.NewCommandError(app.CommandErrorInvalidArgument, "message %s carries no invite code", messageID)
	}
	groupJID, inviter, ok := c.groupInviteParties(message, payload)
	if !ok {
		return "", app.NewCommandError(app.CommandErrorInvalidArgument, "invite %s names no group", messageID)
	}

	if payload.Joined || c.alreadyInGroup(ctx, groupJID) {
		if !payload.Joined {
			payload.Joined = true
			c.saveGroupInvitePayload(ctx, messageID, payload)
		}
		return groupJID.String(), nil
	}
	if groupInviteExpired(payload, time.Now()) {
		return "", app.NewCommandError(app.CommandErrorRejected, "this invite has expired")
	}

	// Bounded, because a code WhatsApp does not recognise is answered with
	// silence rather than a refusal, and an unbounded join would leave the
	// button spinning for the rest of the session waiting for a reply that is
	// never coming.
	joinCtx, cancel := context.WithTimeout(ctx, groupInviteJoinTimeout)
	defer cancel()
	if err := client.JoinGroupWithInvite(joinCtx, groupJID, inviter, payload.Code, payload.ExpiresAt); err != nil {
		if joinCtx.Err() != nil && ctx.Err() == nil {
			return "", app.NewCommandError(app.CommandErrorRejected,
				"WhatsApp did not answer; this invite may have been revoked")
		}
		return "", app.NewCommandError(app.CommandErrorRejected, "join group: %v", err)
	}

	// The card settles into its joined state now rather than when the
	// JoinedGroup event eventually arrives, because the button the user just
	// pressed has to stop offering to do the thing it already did.
	payload.Joined = true
	payload.ResolveError = ""
	c.saveGroupInvitePayload(ctx, messageID, payload)

	// The chat row and its member list come from the JoinedGroup event, but
	// nothing guarantees it beats this reply to the frontend, and a chat that
	// is not there yet cannot be opened. Fetching the group now makes the row
	// exist before we hand back its id.
	c.refreshGroupParticipants(ctx, groupJID)
	c.ensureOrUpdateGroupName(ctx, groupJID, payload.DisplayName())

	return groupJID.String(), nil
}

// groupInviteSummary is the detail on the one-line rendering. The name is the
// whole point of the invite, so a chat list reads "👥 Group invite: Wow3".
func groupInviteSummary(invite *waE2E.GroupInviteMessage) string {
	if invite == nil {
		return ""
	}
	if name := strings.TrimSpace(invite.GetGroupName()); name != "" {
		return name
	}
	return strings.TrimSpace(invite.GetGroupJID())
}
