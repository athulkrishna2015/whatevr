package wa

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"

	appstore "whatevrd/internal/store"
)

const (
	// Recommended request size for on-demand history (whatsmeow docs).
	backfillRequestCount = 50
	// How long to wait for the phone to answer before the request is
	// forgotten. The phone may be offline; expiring without marking the
	// chat exhausted keeps the affordance retryable.
	backfillRequestTimeout = 90 * time.Second
)

type backfillRequest struct {
	requested int
	timer     *time.Timer
}

// RequestOlderMessages asks the phone for messages older than the oldest one
// stored for the chat (on-demand history sync). Returns false when there was
// nothing to ask: no stored messages, history already exhausted, or a request
// already in flight. The response lands asynchronously through the normal
// history sync pipeline; HistoryBackfilled and ChatUpdated events follow.
func (c *Client) RequestOlderMessages(ctx context.Context, chatID string) (bool, error) {
	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return false, errors.New("not connected to WhatsApp")
	}

	chat, err := c.store.GetChat(ctx, chatID)
	if err != nil {
		return false, err
	}
	if chat.HistoryExhausted {
		return false, nil
	}
	oldest, ok, err := c.store.OldestStoredMessage(ctx, chatID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	chatJID, err := types.ParseJID(chatID)
	if err != nil {
		return false, err
	}

	c.backfillMu.Lock()
	if c.backfillInFlight == nil {
		c.backfillInFlight = make(map[string]*backfillRequest)
	}
	if _, busy := c.backfillInFlight[chatID]; busy {
		c.backfillMu.Unlock()
		return false, nil
	}
	req := &backfillRequest{requested: backfillRequestCount}
	c.backfillInFlight[chatID] = req
	c.backfillMu.Unlock()

	info := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chatJID,
			IsFromMe: oldest.Direction == appstore.DirectionOutgoing,
			IsGroup:  chat.IsGroup,
		},
		ID:        appstore.ExternalMessageID(chatID, oldest.ID),
		Timestamp: time.Unix(oldest.TimestampUnix, 0),
	}
	if _, err := client.SendPeerMessage(ctx, client.BuildHistorySyncRequest(info, backfillRequestCount)); err != nil {
		c.finishBackfillRequest(chatID)
		return false, err
	}
	req.timer = time.AfterFunc(backfillRequestTimeout, func() {
		if c.finishBackfillRequest(chatID) {
			zerolog.Ctx(ctx).Debug().Str("chat", chatID).Msg("on-demand history request expired without a response")
		}
	})
	zerolog.Ctx(ctx).Info().Str("chat", chatID).Int("count", backfillRequestCount).Time("before", info.Timestamp).Msg("requested older messages")
	return true, nil
}

// finishBackfillRequest drops the in-flight entry for a chat, reporting
// whether one existed.
func (c *Client) finishBackfillRequest(chatID string) bool {
	c.backfillMu.Lock()
	req, ok := c.backfillInFlight[chatID]
	if ok {
		delete(c.backfillInFlight, chatID)
	}
	c.backfillMu.Unlock()
	if ok && req.timer != nil {
		req.timer.Stop()
	}
	return ok
}

// resolveBackfillRequests matches an ON_DEMAND history sync response against
// in-flight requests. messagesByChat holds the raw payload message count per
// (normalized) chat in the chunk. A response carrying fewer messages than
// requested means the phone has nothing older, so the chat's history is
// exhausted.
//
// Only a chat the chunk actually mentions is resolved here. Silence is not
// evidence: an omitted chat is left to its expiry timer, because exhaustion is
// a one-way latch and guessing it wrong costs that chat its older history
// permanently.
func (c *Client) resolveBackfillRequests(ctx context.Context, messagesByChat map[string]int, answered map[string]bool) {
	type resolution struct {
		chatID    string
		exhausted bool
	}
	var resolved []resolution

	c.backfillMu.Lock()
	for chatID, req := range c.backfillInFlight {
		count, present := messagesByChat[chatID]
		if !present {
			continue
		}
		if req.timer != nil {
			req.timer.Stop()
		}
		delete(c.backfillInFlight, chatID)
		// The phone already said whether more remain; counting messages is only
		// a guess and must not overrule it.
		resolved = append(resolved, resolution{chatID: chatID, exhausted: !answered[chatID] && count < req.requested})
	}
	c.backfillMu.Unlock()

	for _, r := range resolved {
		if !r.exhausted {
			continue
		}
		zerolog.Ctx(ctx).Info().Str("chat", r.chatID).Msg("on-demand history returned less than requested, history exhausted")
		chat, changed, err := c.store.UpdateChatHistoryExhausted(ctx, r.chatID, true)
		if err != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Str("chat", r.chatID).Msg("mark history exhausted")
			continue
		}
		if changed {
			c.daemon.PublishChatUpdated(toDaemonChat(chat))
		}
	}
}
