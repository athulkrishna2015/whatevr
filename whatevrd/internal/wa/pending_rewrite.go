package wa

import (
	"context"
	"database/sql"
	"errors"

	appstore "whatevrd/internal/store"
)

// A rewrite is a message about a message: an edit, which replaces the words, or
// a revoke, which takes them away. Both name a message their sender assumes we
// already have, and on a fresh pairing we usually do not. Live traffic arrives
// seconds after connecting and history sync takes minutes, so this morning's
// deletion lands while this morning is still in the post.
//
// Dropping one is not a cosmetic loss, because nothing ever says it again: the
// message turns up from history wearing the words it had before, and something
// somebody deleted for everybody stays in the transcript for good.

// pendingRewrite is what is waiting to happen to one message.
type pendingRewrite struct {
	revoke   bool
	text     string
	mentions []appstore.MessageMention
}

func (r pendingRewrite) what() string {
	if r.revoke {
		return "revoke"
	}
	return "edit"
}

// applyRewrite performs one and publishes it. sql.ErrNoRows is the message not
// being here yet, which is the caller's cue to park.
func (c *Client) applyRewrite(ctx context.Context, internalID string, rewrite pendingRewrite, silent bool) error {
	var (
		message appstore.Message
		chat    appstore.Chat
		changed bool
		err     error
	)
	if rewrite.revoke {
		message, chat, changed, err = c.store.MarkMessageRevoked(ctx, internalID)
	} else {
		message, chat, changed, err = c.store.UpdateMessageText(ctx, internalID, rewrite.text, rewrite.mentions)
	}
	if err != nil {
		return err
	}
	if changed && !silent {
		c.daemon.PublishMessageUpdated(toDaemonMessage(message))
		c.daemon.PublishChatUpdated(toDaemonChat(chat))
	}
	return nil
}

// parkPendingRewrite holds a rewrite until the message it is about exists.
// keepNewer leaves one that arrived while a replay pass was running in place,
// since that one is the later word on the same message.
func (c *Client) parkPendingRewrite(internalID string, rewrite pendingRewrite, keepNewer bool) {
	if internalID == "" {
		return
	}
	c.pendingRewritesMu.Lock()
	defer c.pendingRewritesMu.Unlock()
	if c.pendingRewrites == nil {
		c.pendingRewrites = make(map[string]pendingRewrite)
	}
	if parked, ok := c.pendingRewrites[internalID]; ok {
		// A deletion is the last thing that happens to a message, so an edit
		// never displaces one: the store refuses that edit anyway, and letting
		// it overwrite here would lose the deletion instead.
		if keepNewer || parked.revoke {
			return
		}
	}
	c.pendingRewrites[internalID] = rewrite
}

// reconcilePendingRewrites applies parked edits and revokes to the messages
// that have arrived since. It runs where the parked stars run: after every
// history sync chunk, and once with final=true when the initial sync settles.
func (c *Client) reconcilePendingRewrites(ctx context.Context, final bool) {
	c.pendingRewritesMu.Lock()
	pending := c.pendingRewrites
	c.pendingRewrites = nil
	c.pendingRewritesMu.Unlock()
	if len(pending) == 0 {
		return
	}

	for internalID, rewrite := range pending {
		if ctx.Err() != nil {
			return
		}
		err := c.applyRewrite(ctx, internalID, rewrite, false)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			c.log.Warnf("Failed to apply %s to message %s: %v", rewrite.what(), internalID, err)
			continue
		}
		if final {
			// The sync is over and the message never came: it is older than the
			// history this device was given, and there is nothing here to
			// rewrite.
			c.log.Debugf("Dropping %s for message %s, which was never synced", rewrite.what(), internalID)
			continue
		}
		c.parkPendingRewrite(internalID, rewrite, true)
	}
}
