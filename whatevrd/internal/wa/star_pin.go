package wa

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

const (
	maxPinnedMessages = 3
	// Fallback pin lifetime when an incoming pin omits its duration (WhatsApp's
	// "7 days" default).
	defaultPinDurationSecs = 7 * 24 * 60 * 60
)

// SetMessageStarred stars or unstars a message, syncing the change to WhatsApp
// via a regular_high app-state mutation and mirroring it into the local store.
// Returns the reloaded target message.
func (c *Client) SetMessageStarred(ctx context.Context, messageID string, starred bool) (appstore.Message, error) {
	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return appstore.Message{}, err
	}

	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorNotLoggedIn, "WhatsApp client is not logged in")
	}

	chatJID, err := types.ParseJID(message.ChatID)
	if err != nil {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}

	fromMe := message.Direction == appstore.DirectionOutgoing
	sender := messageSenderJID(client, message, chatJID, fromMe)
	externalID := types.MessageID(appstore.ExternalMessageID(message.ChatID, message.ID))

	patch := appstate.BuildStar(chatJID, sender, externalID, fromMe, starred)
	if err := c.sendRegularHighAppState(ctx, client, patch); err != nil {
		return appstore.Message{}, err
	}

	updated, changed, err := c.store.SetMessageStarred(ctx, message.ID, starred)
	if err != nil {
		return appstore.Message{}, err
	}
	if changed {
		c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	}
	return updated, nil
}

// PinMessage pins or unpins a message for everyone in its chat. Pinning sends a
// PinInChatMessage carrying the chosen lifetime; unpinning sends UNPIN_FOR_ALL.
// The local store is updated to reflect the new pin window. Returns the reloaded
// target message.
func (c *Client) PinMessage(ctx context.Context, messageID string, pinned bool, durationSecs uint32) (appstore.Message, error) {
	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return appstore.Message{}, err
	}
	if message.IsRevoked {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorRejected, "deleted messages cannot be pinned")
	}

	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorNotLoggedIn, "WhatsApp client is not logged in")
	}

	chatJID, err := types.ParseJID(message.ChatID)
	if err != nil {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}

	now := time.Now()
	if pinned {
		alreadyPinned := message.PinnedUntil > now.Unix()
		if !alreadyPinned {
			count, err := c.store.CountActivePins(ctx, message.ChatID)
			if err != nil {
				return appstore.Message{}, err
			}
			if count >= maxPinnedMessages {
				return appstore.Message{}, app.NewCommandError(app.CommandErrorRejected, "You can only pin %d messages per chat", maxPinnedMessages)
			}
		}
		if durationSecs == 0 {
			durationSecs = defaultPinDurationSecs
		}
	}

	fromMe := message.Direction == appstore.DirectionOutgoing
	var targetSender types.JID
	if !fromMe && message.SenderID != "" && message.SenderID != "me" {
		if parsed, parseErr := types.ParseJID(message.SenderID); parseErr == nil {
			targetSender = parsed
		}
	}
	externalID := types.MessageID(appstore.ExternalMessageID(message.ChatID, message.ID))

	pinType := waE2E.PinInChatMessage_PIN_FOR_ALL
	if !pinned {
		pinType = waE2E.PinInChatMessage_UNPIN_FOR_ALL
	}
	pinMsg := &waE2E.Message{
		PinInChatMessage: &waE2E.PinInChatMessage{
			Key:               client.BuildMessageKey(chatJID, targetSender, externalID),
			Type:              pinType.Enum(),
			SenderTimestampMS: proto.Int64(now.UnixMilli()),
		},
	}
	if pinned {
		pinMsg.MessageContextInfo = &waE2E.MessageContextInfo{
			MessageAddOnDurationInSecs: proto.Uint32(durationSecs),
			MessageAddOnExpiryType:     waE2E.MessageContextInfo_STATIC.Enum(),
		}
	}
	if _, err := c.guardedSend(ctx, client, chatJID, pinMsg); err != nil {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorRejected, "send pin failed: %v", err)
	}

	var pinnedAt, pinnedUntil int64
	if pinned {
		pinnedAt = now.Unix()
		pinnedUntil = now.Unix() + int64(durationSecs)
	}
	updated, changed, err := c.store.SetMessagePinned(ctx, message.ID, pinnedAt, pinnedUntil)
	if err != nil {
		return appstore.Message{}, err
	}
	if changed {
		c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	}
	return updated, nil
}

// messageSenderJID resolves the participant JID for app-state mutations that key
// off a message: our own JID for messages we sent, the original author for
// incoming group messages, and the chat itself for incoming 1:1 messages (which
// BuildStar normalizes to the self-marker "0").
func messageSenderJID(client *whatsmeow.Client, message appstore.Message, chatJID types.JID, fromMe bool) types.JID {
	if !fromMe && message.SenderID != "" && message.SenderID != "me" {
		if parsed, err := types.ParseJID(message.SenderID); err == nil {
			return parsed
		}
	}
	if fromMe && client.Store.ID != nil {
		return *client.Store.ID
	}
	return chatJID
}

// handleStarEvent applies a star/unstar made on another device (or during app
// state sync) to the local store and publishes the change.
func (c *Client) handleStarEvent(ctx context.Context, evt *events.Star) {
	if evt == nil || evt.Action == nil || evt.ChatJID.IsEmpty() {
		return
	}
	messageID := strings.TrimSpace(evt.MessageID)
	if messageID == "" {
		return
	}

	chatJID := c.normalizeJIDForChat(ctx, evt.ChatJID)
	internalID := internalMessageIDForChat(chatJID.String(), types.MessageID(messageID))

	starred := evt.Action.GetStarred()
	updated, changed, err := c.store.SetMessageStarred(ctx, internalID, starred)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The message has not been synced yet, which on a fresh pairing is
			// every message in the account: app state arrives seconds after
			// connecting and history sync takes minutes. Park it.
			c.parkPendingStar(internalID, starred, false)
			return
		}
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", internalID).Msg("apply star")
		return
	}
	if changed && !evt.FromFullSync {
		c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	}
}

// reconcileStarsFromEvents applies the stars in a full regular_high snapshot.
//
// A full app state sync emits nothing by itself, so every star an account
// already had arrives only here, in the snapshot this fetches at connect. The
// messages they mark are still on their way at that point, which is what the
// parking below is for.
func (c *Client) reconcileStarsFromEvents(ctx context.Context, eventsToDispatch []any) {
	for _, raw := range eventsToDispatch {
		evt, ok := raw.(*events.Star)
		if !ok {
			continue
		}
		c.handleStarEvent(ctx, evt)
	}
}

// parkPendingStar holds a star until the message it marks exists. keepNewer
// leaves a star that arrived while a reconcile pass was running in place, since
// that one is the later word on the same message.
func (c *Client) parkPendingStar(internalID string, starred, keepNewer bool) {
	if internalID == "" {
		return
	}
	c.pendingStarsMu.Lock()
	defer c.pendingStarsMu.Unlock()
	if c.pendingStars == nil {
		c.pendingStars = make(map[string]bool)
	}
	if _, newer := c.pendingStars[internalID]; keepNewer && newer {
		return
	}
	c.pendingStars[internalID] = starred
}

// reconcilePendingStars applies parked stars to the messages that have arrived
// since, and runs wherever pending app state does: after every history sync
// chunk, and once with final=true when the initial sync has settled.
//
// Without it a fresh pairing loses every star the account has. The star is not
// resent: app state is a snapshot taken at connect, and a star dropped because
// its message was still in the post is a star this device never sees again.
func (c *Client) reconcilePendingStars(ctx context.Context, final bool) {
	c.pendingStarsMu.Lock()
	pending := c.pendingStars
	c.pendingStars = nil
	c.pendingStarsMu.Unlock()
	if len(pending) == 0 {
		return
	}

	for internalID, starred := range pending {
		if ctx.Err() != nil {
			return
		}
		updated, changed, err := c.store.SetMessageStarred(ctx, internalID, starred)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				zerolog.Ctx(ctx).Warn().Err(err).Str("msg", internalID).Msg("apply star")
				continue
			}
			if final {
				// The sync is over and the message never came: it is older
				// than the history this device was given, and there is nothing
				// here to star.
				zerolog.Ctx(ctx).Debug().Str("msg", internalID).Msg("dropping a star for a message that was never synced")
				continue
			}
			c.parkPendingStar(internalID, starred, true)
			continue
		}
		if changed {
			c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
		}
	}
}

// handlePinInChat intercepts pin-for-all / unpin-for-all messages and updates
// the referenced message's pin window instead of ingesting the control message
// as a chat message. Returns true when the event was a pin action.
func (c *Client) handlePinInChat(ctx context.Context, evt *events.Message, offlineSync bool) bool {
	if evt == nil || evt.Message == nil {
		return false
	}
	pin := evt.Message.GetPinInChatMessage()
	if pin == nil || pin.GetKey() == nil {
		return false
	}

	chatID, _ := c.internalMessageIDFromInfo(ctx, evt.Info)
	targetID := strings.TrimSpace(pin.GetKey().GetID())
	if chatID == "" || targetID == "" {
		return true
	}
	internalID := internalMessageIDForChat(chatID, types.MessageID(targetID))

	pinned := pin.GetType() == waE2E.PinInChatMessage_PIN_FOR_ALL
	var pinnedAt, pinnedUntil int64
	if pinned {
		pinnedAt = evt.Info.Timestamp.Unix()
		if ms := pin.GetSenderTimestampMS(); ms > 0 {
			pinnedAt = ms / 1000
		}
		duration := int64(evt.Message.GetMessageContextInfo().GetMessageAddOnDurationInSecs())
		if duration <= 0 {
			duration = defaultPinDurationSecs
		}
		pinnedUntil = pinnedAt + duration
	}

	updated, changed, err := c.store.SetMessagePinned(ctx, internalID, pinnedAt, pinnedUntil)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			zerolog.Ctx(ctx).Warn().Err(err).Str("msg", internalID).Msg("apply pin")
		}
		return true
	}
	if changed && !offlineSync {
		c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	}
	return true
}

// sendRegularHighAppState mirrors sendRegularLowAppState for the regular_high
// collection (where stars live), resyncing and retrying once on a hash
// conflict.
func (c *Client) sendRegularHighAppState(ctx context.Context, client *whatsmeow.Client, patch appstate.PatchInfo) error {
	c.appStateMu.Lock()
	defer c.appStateMu.Unlock()

	if err := client.SendAppState(ctx, patch); err != nil {
		if !isAppStateConflictError(err) {
			return err
		}
		zerolog.Ctx(ctx).Warn().Err(err).Msg("app state conflict while updating stars, resyncing regular_high and retrying")
		if _, syncErr := fetchFullRegularHighAppState(ctx, client); syncErr != nil {
			return app.NewCommandError(app.CommandErrorRejected, "WhatsApp sync conflict. Try again in a moment.")
		}
		if retryErr := client.SendAppState(ctx, patch); retryErr != nil {
			return app.NewCommandError(app.CommandErrorRejected, "WhatsApp sync conflict. Try again in a moment.")
		}
	}
	return nil
}

func fetchFullRegularHighAppState(ctx context.Context, client *whatsmeow.Client) ([]any, error) {
	oldEmit := client.EmitAppStateEventsOnFullSync
	client.EmitAppStateEventsOnFullSync = true
	defer func() { client.EmitAppStateEventsOnFullSync = oldEmit }()

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return client.DangerousInternals().FetchAppState(fetchCtx, appstate.WAPatchRegularHigh, true, false)
}
