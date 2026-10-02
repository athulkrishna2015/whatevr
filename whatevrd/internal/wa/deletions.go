package wa

import (
	"context"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Phone-side deletion sync. These app-state events arrive when the user
// deletes a message ("delete for me"), clears a chat or deletes a chat on
// another device. Full-sync replays are skipped for the chat-level wipes: at
// first login the full app-state sync can carry historical clear/delete
// actions that would otherwise erase freshly history-synced transcripts.

func (c *Client) handleDeleteForMeEvent(ctx context.Context, evt *events.DeleteForMe) {
	chatID := c.normalizeJIDForChat(ctx, evt.ChatJID).String()
	if chatID == "" || evt.MessageID == "" {
		return
	}
	internalID := internalMessageIDForChat(chatID, types.MessageID(evt.MessageID))
	message, chat, existed, err := c.store.DeleteMessageForMe(ctx, internalID)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", internalID).Msg("delete message for phone-side delete-for-me")
		return
	}
	if !existed {
		// The deletion may reference a message we never synced; ignore quietly.
		return
	}
	zerolog.Ctx(ctx).Info().Str("msg", internalID).Msg("deleted message after phone-side delete-for-me")
	if !evt.FromFullSync {
		c.daemon.PublishMessageDeleted(message.ChatID, message.ID, toDaemonChat(chat))
	}
}

func (c *Client) handleDeleteChatEvent(ctx context.Context, evt *events.DeleteChat) {
	if evt.FromFullSync {
		return
	}
	chatID := c.normalizeJIDForChat(ctx, evt.JID).String()
	if chatID == "" {
		return
	}
	existed, err := c.store.DeleteChat(ctx, chatID)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("chat", chatID).Msg("delete chat for phone-side deletion")
		return
	}
	if !existed {
		return
	}
	zerolog.Ctx(ctx).Info().Str("chat", chatID).Msg("deleted chat after phone-side deletion")
	c.daemon.PublishChatDeleted(chatID)
}

func (c *Client) handleClearChatEvent(ctx context.Context, evt *events.ClearChat) {
	if evt.FromFullSync {
		return
	}
	chatID := c.normalizeJIDForChat(ctx, evt.JID).String()
	if chatID == "" {
		return
	}
	chat, existed, err := c.store.ClearChatMessages(ctx, chatID)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("chat", chatID).Msg("clear chat for phone-side clear")
		return
	}
	if !existed {
		return
	}
	zerolog.Ctx(ctx).Info().Str("chat", chatID).Msg("cleared chat after phone-side clear")
	c.daemon.PublishChatCleared(toDaemonChat(chat))
}
