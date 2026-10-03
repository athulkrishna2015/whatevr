package v1

import (
	"context"
	"encoding/json"
	"time"

	"whatevrd/internal/core"
	"whatevrd/internal/protocol"
	"whatevrd/internal/store"
	"whatevrd/internal/wa"
)

// Commands is the command surface: the old client's, with every answer that
// is a read coming from the new core instead.
type Commands struct {
	*wa.Client
	a *Adapter
}

var _ protocol.CommandActions = Commands{}

func (a *Adapter) Commands() Commands { return Commands{Client: a.wa, a: a} }

func (c Commands) SearchChats(ctx context.Context, query string, limit int) ([]store.Chat, error) {
	return c.a.SearchChats(ctx, query, limit)
}

func (c Commands) SearchMessages(ctx context.Context, query, chatID string, limit int, before string) ([]store.MessageSearchResult, error) {
	return c.a.SearchMessages(ctx, query, chatID, limit, before)
}

// MarkChatReadUpTo sends the receipts the old way, then logs that this
// device read the chat to there: nothing else would tell the core, the
// server never echoes our own reads back.
func (c Commands) MarkChatReadUpTo(ctx context.Context, chatID, upTo string) (store.Chat, error) {
	if _, err := c.Client.MarkChatReadUpTo(ctx, chatID, upTo); err != nil {
		return store.Chat{}, err
	}
	if _, id, ok := SplitMessageID(upTo); ok {
		if err := c.a.logRead(ctx, chatID, id); err != nil {
			c.a.log.Warn().Err(err).Str("chat", chatID).Msg("v1: own read not logged")
		}
	}
	return c.a.GetChat(ctx, chatID)
}

func (c Commands) SetChatPinned(ctx context.Context, chatID string, pinned bool) (store.Chat, error) {
	if _, err := c.Client.SetChatPinned(ctx, chatID, pinned); err != nil {
		return store.Chat{}, err
	}
	return c.a.settled(ctx, chatID)
}

func (c Commands) SetChatArchived(ctx context.Context, chatID string, archived bool) (store.Chat, error) {
	if _, err := c.Client.SetChatArchived(ctx, chatID, archived); err != nil {
		return store.Chat{}, err
	}
	return c.a.settled(ctx, chatID)
}

func (c Commands) SetChatMuted(ctx context.Context, chatID string, muted bool, d time.Duration) (store.Chat, error) {
	if _, err := c.Client.SetChatMuted(ctx, chatID, muted, d); err != nil {
		return store.Chat{}, err
	}
	return c.a.settled(ctx, chatID)
}

// settled is a chat once the core folded what the command logged. app state
// we sent comes back through the fetch whatsmeow runs after sending it.
func (a *Adapter) settled(ctx context.Context, chatID string) (store.Chat, error) {
	if _, appended := a.core.Progress(); appended > 0 {
		wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = a.core.WaitFolded(wctx, appended)
		cancel()
	}
	return a.GetChat(ctx, chatID)
}

// logRead is a read this device made, as the receipt input another device's
// read would be.
func (a *Adapter) logRead(ctx context.Context, chatID, id string) error {
	head, err := json.Marshal(core.ReceiptHead{
		Source: core.Source{Chat: chatID, FromMe: true},
		IDs:    []string{id},
		Type:   "read-self",
	})
	if err != nil {
		return err
	}
	seq, err := a.core.Append(ctx, core.Input{Kind: core.KindReceipt, V: 1, Head: head})
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return a.core.WaitFolded(wctx, seq)
}

var _ protocol.StickerStore = (*Adapter)(nil)
var _ protocol.DaemonStore = (*Adapter)(nil)
