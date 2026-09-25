package wa

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Keep in chat: WhatsApp's answer to a disappearing message somebody wants to
// survive its timer. The control message names a target and a keep type, and it
// is not itself something anyone said, so it never becomes a row. It sets a flag
// on the message it names, which the transcript draws as a bookmark.
//
// Whether a kept message actually survives is the disappearing-message expiry
// sweep, which is not built yet. The flag and the badge are honest on their own:
// they say what the chat asked for.

// keepFlagFromType maps a keep type to the flag it sets, and reports whether the
// type meant anything at all. UNKNOWN_KEEP_TYPE is a control message we cannot
// act on, and acting on it as an un-keep would silently drop a real one.
func keepFlagFromType(keepType waE2E.KeepType) (bool, bool) {
	switch keepType {
	case waE2E.KeepType_KEEP_FOR_ALL:
		return true, true
	case waE2E.KeepType_UNDO_KEEP_FOR_ALL:
		return false, true
	default:
		return false, false
	}
}

// handleKeepInChat intercepts keep / undo-keep control messages and flags the
// message they name instead of ingesting them as chat messages. Returns false
// when the store write fails; an unknown target is consumed.
func (c *Client) handleKeepInChat(ctx context.Context, evt *events.Message, offlineSync bool) bool {
	if evt == nil || evt.Message == nil {
		return false
	}
	keep := evt.Message.GetKeepInChatMessage()
	if keep == nil || keep.GetKey() == nil {
		return false
	}

	kept, known := keepFlagFromType(keep.GetKeepType())
	if !known {
		c.log.Debugf("Ignoring keep-in-chat with unknown type %v", keep.GetKeepType())
		return true
	}

	chatID, _ := c.internalMessageIDFromInfo(ctx, evt.Info)
	targetID := strings.TrimSpace(keep.GetKey().GetID())
	if chatID == "" || targetID == "" {
		return true
	}

	return c.applyKeepInChat(ctx, internalMessageIDForChat(chatID, types.MessageID(targetID)), kept, offlineSync)
}

// applyKeepInChat sets the flag on one row and publishes the change. The target
// may be a message we never synced, which is ordinary rather than an error: the
// keep arrived, the message it names did not.
func (c *Client) applyKeepInChat(ctx context.Context, internalID string, kept bool, quiet bool) bool {
	updated, changed, err := c.store.SetMessageKept(ctx, internalID, kept)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true
		}
		c.log.Warnf("Failed to apply keep-in-chat to message %s: %v", internalID, err)
		return false
	}
	if changed && !quiet {
		c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	}
	return true
}

// historyKeepState reads the keep flag a backfilled message carries. History
// sync does not replay the control message: the phone folds its outcome into the
// kept message itself, so this is the only place a backfilled keep exists.
func historyKeepState(webMsg *waWeb.WebMessageInfo) (bool, bool) {
	keep := webMsg.GetKeepInChat()
	if keep == nil {
		return false, false
	}
	return keepFlagFromType(keep.GetKeepType())
}
