package wa

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// Polls, both directions.
//
// A poll arrives as a PollCreationMessage in one of five slots (V1, V2, V3, V5,
// V6; V4 is a FutureProofMessage and not a poll at all). Votes arrive as
// ordinary messages carrying a PollUpdateMessage, encrypted against the poll's
// message secret, and a decrypted vote names its choices by SHA-256 hash rather
// than by text. That is why options are stored with their hashes: it is the
// only way to match a vote to a choice, and it is what lets an option be added
// later without invalidating votes already cast.
//
// A vote message carries a voter's *entire current selection*, not a delta, so
// applying one replaces everything that voter had chosen before.

// pendingPollVoteRetention drops parked votes whose poll never turned up.
const pendingPollVoteRetention = 7 * 24 * time.Hour

// pollCreationFromMessage finds the poll in whichever slot it arrived in.
func pollCreationFromMessage(msg *waE2E.Message) *waE2E.PollCreationMessage {
	if msg == nil {
		return nil
	}
	// Deliberately not V4: that slot holds a FutureProofMessage, and reading it
	// as a poll would be a type error waiting to happen.
	for _, candidate := range []*waE2E.PollCreationMessage{
		msg.GetPollCreationMessage(),
		msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(),
		msg.GetPollCreationMessageV5(),
		msg.GetPollCreationMessageV6(),
	} {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func (c *Client) pollMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	poll := pollCreationFromMessage(evt.Message)
	if poll == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := c.mediaInputBase(ctx, evt, opts, "", poll.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.PollPayload{
		Question:        strings.TrimSpace(poll.GetName()),
		SelectableCount: int(poll.GetSelectableOptionsCount()),
		AllowAddOption:  poll.GetAllowAddOption(),
		EndsAt:          poll.GetEndTime(),
		Quiz:            poll.GetPollType() == waE2E.PollType_QUIZ,
	}
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Poll: payload})
	if err != nil {
		c.log.Warnf("Failed to encode poll payload for %s: %v", base.ID, err)
		return appstore.MediaMessageInput{}, false
	}

	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindPoll,
		PayloadSummary:   payload.Question,
	}, true
}

// savePollOptions records a stored poll's choices with the hashes a vote will
// name them by, then applies any votes that arrived before the poll did.
func (c *Client) savePollOptions(ctx context.Context, messageID string, poll *waE2E.PollCreationMessage) {
	names := make([]string, 0, len(poll.GetOptions()))
	for _, option := range poll.GetOptions() {
		names = append(names, option.GetOptionName())
	}
	if len(names) == 0 {
		return
	}
	hashes := whatsmeow.HashPollOptions(names)

	options := make([]appstore.PollOption, 0, len(names))
	for i, name := range names {
		options = append(options, appstore.PollOption{Index: i, Name: name, SHA256: hashes[i]})
	}
	if err := c.store.SavePollOptions(ctx, messageID, options); err != nil {
		c.log.Warnf("Failed to store poll options for %s: %v", messageID, err)
		return
	}
	c.drainPendingPollVotes(ctx, messageID)
}

// handlePollUpdate folds an incoming vote into its poll. Returns false when a
// store write fails; a vote is a change to the poll, not a message in the
// conversation.
func (c *Client) handlePollUpdate(ctx context.Context, evt *events.Message) bool {
	if evt == nil || evt.Message == nil {
		return false
	}
	update := evt.Message.GetPollUpdateMessage()
	if update == nil {
		return false
	}

	chatJID := c.normalizeJIDForChat(ctx, evt.Info.Chat)
	pollID := internalMessageIDForChat(chatJID.String(), update.GetPollCreationMessageKey().GetID())
	voter := senderID(evt.Info)
	votedAt := evt.Info.Timestamp.Unix()
	if ms := update.GetSenderTimestampMS(); ms > 0 {
		votedAt = ms / 1000
	}

	client := c.currentClient()
	if client == nil {
		return true
	}

	selections, err := client.DecryptPollVote(ctx, evt)
	if err != nil {
		// Almost always because the poll itself has not arrived, so its message
		// secret is not stored yet. Park the vote rather than losing it; it is
		// replayed when the poll lands.
		return c.parkPollVote(ctx, chatJID.String(), pollID, voter, update, votedAt)
	}

	applied, err := c.store.ApplyPollVote(ctx, pollID, voter, selections.GetSelectedOptions(), votedAt)
	if err != nil {
		c.log.Warnf("Failed to apply poll vote on %s: %v", pollID, err)
		return false
	}
	// A vote the store judged stale changed nothing, so there is nothing to
	// tell anybody about. Reconnects redeliver, so this is the common case.
	if applied {
		c.publishPollUpdated(ctx, pollID)
	}
	return true
}

func (c *Client) parkPollVote(ctx context.Context, chatID, pollID, voter string, update *waE2E.PollUpdateMessage, votedAt int64) bool {
	if err := c.store.ParkPollVote(ctx, appstore.PendingPollVote{
		// Keyed by poll and voter, so a voter changing their mind before the
		// poll arrives parks one row rather than a pile.
		ID:            pollID + "\x1f" + voter,
		ChatID:        chatID,
		PollMessageID: pollID,
		VoterJID:      voter,
		EncPayload:    update.GetVote().GetEncPayload(),
		EncIV:         update.GetVote().GetEncIV(),
		SenderTSMS:    votedAt,
	}); err != nil {
		c.log.Warnf("Failed to park poll vote for %s: %v", pollID, err)
		return false
	}
	return true
}

// drainPendingPollVotes replays the votes parked for a poll that has now
// arrived. They are decrypted through the same path a live vote takes, by
// reconstructing the event whatsmeow expects.
func (c *Client) drainPendingPollVotes(ctx context.Context, pollID string) {
	parked, err := c.store.ListPendingPollVotes(ctx, pollID)
	if err != nil {
		c.log.Warnf("Failed to list pending poll votes for %s: %v", pollID, err)
		return
	}
	if len(parked) == 0 {
		return
	}

	client := c.currentClient()
	if client == nil {
		return
	}
	message, err := c.store.GetMessage(ctx, pollID)
	if err != nil {
		return
	}
	chatJID, err := types.ParseJID(message.ChatID)
	if err != nil {
		return
	}
	pollSender, err := types.ParseJID(message.SenderID)
	if err != nil {
		pollSender = chatJID
	}
	_, externalPollID, found := strings.Cut(pollID, ":")
	if !found {
		return
	}

	applied := 0
	for _, vote := range parked {
		voterJID, err := types.ParseJID(vote.VoterJID)
		if err != nil {
			continue
		}
		replay := &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{
					Chat:     chatJID,
					Sender:   voterJID,
					IsFromMe: message.Direction == appstore.DirectionOutgoing && vote.VoterJID == message.SenderID,
					IsGroup:  chatJID.Server == types.GroupServer,
				},
				Timestamp: time.Unix(vote.SenderTSMS, 0),
			},
			Message: &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{
				PollCreationMessageKey: client.BuildMessageKey(chatJID, pollSender, externalPollID),
				Vote: &waE2E.PollEncValue{
					EncPayload: vote.EncPayload,
					EncIV:      vote.EncIV,
				},
			}},
		}
		selections, err := client.DecryptPollVote(ctx, replay)
		if err != nil {
			c.log.Warnf("Failed to decrypt a parked vote for %s: %v", pollID, err)
			continue
		}
		ok, err := c.store.ApplyPollVote(ctx, pollID, vote.VoterJID, selections.GetSelectedOptions(), vote.SenderTSMS)
		if err != nil {
			c.log.Warnf("Failed to apply a parked vote for %s: %v", pollID, err)
			continue
		}
		if ok {
			applied++
		}
		if err := c.store.DeletePendingPollVote(ctx, vote.ID); err != nil {
			c.log.Warnf("Failed to delete an applied poll vote for %s: %v", pollID, err)
		}
	}
	if applied > 0 {
		c.publishPollUpdated(ctx, pollID)
	}
}

// handlePollAddOption folds in an option somebody added to an existing poll.
// It arrives wrapped in a SecretEncryptedMessage rather than in the clear.
func (c *Client) handlePollAddOption(ctx context.Context, evt *events.Message) bool {
	if evt == nil || evt.Message == nil || evt.Message.GetEncReactionMessage() != nil {
		return false
	}
	if evt.Message.GetSecretEncryptedMessage() == nil && evt.Message.GetPollAddOptionMessage() == nil {
		return false
	}

	client := c.currentClient()
	if client == nil {
		return true
	}

	add := evt.Message.GetPollAddOptionMessage()
	if add == nil {
		inner, err := client.DecryptSecretEncryptedMessage(ctx, evt)
		if err != nil {
			c.log.Warnf("Failed to decrypt a secret message: %v", err)
			return true
		}
		add = inner.GetPollAddOptionMessage()
		if add == nil {
			// Some other secret-wrapped edit; not ours to handle here.
			return false
		}
	}

	chatJID := c.normalizeJIDForChat(ctx, evt.Info.Chat)
	pollID := internalMessageIDForChat(chatJID.String(), add.GetPollCreationMessageKey().GetID())
	name := strings.TrimSpace(add.GetAddOption().GetOptionName())
	if name == "" {
		return true
	}
	hashes := whatsmeow.HashPollOptions([]string{name})
	if err := c.store.AddPollOption(ctx, pollID, name, hashes[0]); err != nil {
		c.log.Warnf("Failed to add a poll option to %s: %v", pollID, err)
		return false
	}
	c.publishPollUpdated(ctx, pollID)
	return true
}

func (c *Client) publishPollUpdated(ctx context.Context, pollID string) {
	message, err := c.store.GetMessage(ctx, pollID)
	if err != nil {
		return
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(message))
}

// VotePoll casts our own vote. The selection is whole: passing fewer options
// than before is how a voter takes one back, which is exactly what the wire
// format means by a vote carrying the entire current choice.
func (c *Client) VotePoll(ctx context.Context, messageID string, optionIndexes []int) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "message_id is required")
	}

	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return app.NewCommandError(app.CommandErrorNotLoggedIn, "not logged in")
	}

	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}
	if message.MediaKind != appstore.MediaKindPoll {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "message %s is not a poll", messageID)
	}

	options, err := c.store.PollOptionHashes(ctx, messageID)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(optionIndexes))
	hashes := make([][]byte, 0, len(optionIndexes))
	for _, index := range optionIndexes {
		if index < 0 || index >= len(options) {
			return app.NewCommandError(app.CommandErrorInvalidArgument, "poll %s has no option %d", messageID, index)
		}
		names = append(names, options[index].Name)
		hashes = append(hashes, options[index].SHA256)
	}

	chatJID, err := types.ParseJID(message.ChatID)
	if err != nil {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}
	pollSender, err := types.ParseJID(message.SenderID)
	if err != nil {
		pollSender = chatJID
	}
	_, externalPollID, found := strings.Cut(messageID, ":")
	if !found {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "malformed message id %q", messageID)
	}

	pollInfo := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chatJID,
			Sender:   pollSender,
			IsFromMe: message.Direction == appstore.DirectionOutgoing,
			IsGroup:  chatJID.Server == types.GroupServer,
		},
		ID: externalPollID,
	}

	vote, err := client.BuildPollVote(ctx, pollInfo, names)
	if err != nil {
		return app.NewCommandError(app.CommandErrorRejected, "build poll vote: %v", err)
	}

	// Our own choice is recorded before the send, so the tally moves the moment
	// the tap arrives rather than one network round trip later. The send is the
	// slow part by two orders of magnitude, and it is the part nobody should
	// have to watch.
	selfJID := ""
	if own := client.Store.GetJID(); !own.IsEmpty() {
		selfJID = own.ToNonAD().String()
	}
	previous, err := c.store.PollVoteHashes(ctx, messageID, selfJID)
	if err != nil {
		return err
	}
	if selfJID != "" {
		if _, err := c.store.ApplyPollVote(ctx, messageID, selfJID, hashes, time.Now().Unix()); err != nil {
			return err
		}
		c.publishPollUpdated(ctx, messageID)
	}

	if _, err := client.SendMessage(ctx, chatJID, vote); err != nil {
		// Optimism has to be honest: a vote nobody else will ever see must not
		// keep sitting in our own tally, so the previous selection goes back.
		if selfJID != "" {
			if _, undo := c.store.ApplyPollVote(ctx, messageID, selfJID, previous, time.Now().Unix()); undo != nil {
				c.log.Warnf("Failed to undo an unsent vote on %s: %v", messageID, undo)
			}
			c.publishPollUpdated(ctx, messageID)
		}
		return app.NewCommandError(app.CommandErrorRejected, "send poll vote: %v", err)
	}
	return nil
}

// recordSelfJID tells the store our own jid, so a poll tally can mark which
// choice is ours.
func (c *Client) recordSelfJID(ctx context.Context) {
	client := c.currentClient()
	if client == nil || client.Store == nil {
		return
	}
	own := client.Store.GetJID()
	if own.IsEmpty() {
		return
	}
	if err := c.store.SetSelfJID(ctx, own.ToNonAD().String()); err != nil {
		c.log.Warnf("Failed to record our own jid: %v", err)
	}
}

// prunePendingPollVotes drops parked votes whose poll never arrived.
func (c *Client) prunePendingPollVotes(ctx context.Context) {
	if err := c.store.PrunePendingPollVotes(ctx, time.Now().Add(-pendingPollVoteRetention).Unix()); err != nil &&
		!errors.Is(err, context.Canceled) {
		c.log.Warnf("Failed to prune pending poll votes: %v", err)
	}
}
