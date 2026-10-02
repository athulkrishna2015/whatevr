package wa

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types/events"
)

// eventContext puts the ids of one whatsmeow event on the logger in ctx and
// logs the event once at info. stanza is the WhatsApp message id; rows the
// event writes are logged with msg, our internal id.
func eventContext(ctx context.Context, raw any) context.Context {
	name := strings.TrimPrefix(fmt.Sprintf("%T", raw), "*events.")
	lc := zerolog.Ctx(ctx).With().Str("event", name)
	extra := func(e *zerolog.Event) *zerolog.Event { return e }
	switch evt := raw.(type) {
	case *events.Message:
		lc = lc.Stringer("chat", evt.Info.Chat).Stringer("sender", evt.Info.Sender).Str("stanza", evt.Info.ID)
		extra = func(e *zerolog.Event) *zerolog.Event {
			return e.Str("msg_type", evt.Info.Type).Str("media_type", evt.Info.MediaType).Bool("from_me", evt.Info.IsFromMe).
				Bool("edit", evt.IsEdit).Bool("unavailable_request", evt.UnavailableRequestID != "")
		}
	case *events.UndecryptableMessage:
		lc = lc.Stringer("chat", evt.Info.Chat).Stringer("sender", evt.Info.Sender).Str("stanza", evt.Info.ID)
		extra = func(e *zerolog.Event) *zerolog.Event {
			return e.Bool("unavailable", evt.IsUnavailable).Str("unavailable_type", string(evt.UnavailableType)).Str("fail_mode", string(evt.DecryptFailMode))
		}
	case *events.Receipt:
		lc = lc.Stringer("chat", evt.Chat).Stringer("sender", evt.Sender)
		extra = func(e *zerolog.Event) *zerolog.Event {
			return e.Str("receipt_type", string(evt.Type)).Strs("stanzas", evt.MessageIDs)
		}
	case *events.ChatPresence:
		lc = lc.Stringer("chat", evt.Chat).Stringer("sender", evt.Sender)
		extra = func(e *zerolog.Event) *zerolog.Event { return e.Str("state", string(evt.State)) }
	case *events.Presence:
		lc = lc.Stringer("jid", evt.From)
		extra = func(e *zerolog.Event) *zerolog.Event { return e.Bool("unavailable", evt.Unavailable) }
	case *events.HistorySync:
		extra = func(e *zerolog.Event) *zerolog.Event {
			return e.Str("sync_type", evt.Data.GetSyncType().String()).Uint32("chunk_order", evt.Data.GetChunkOrder()).
				Uint32("progress", evt.Data.GetProgress()).Int("conversations", len(evt.Data.GetConversations()))
		}
	case *events.Pin:
		lc = lc.Stringer("chat", evt.JID)
		extra = fullSync(evt.FromFullSync)
	case *events.Archive:
		lc = lc.Stringer("chat", evt.JID)
		extra = fullSync(evt.FromFullSync)
	case *events.Mute:
		lc = lc.Stringer("chat", evt.JID)
		extra = fullSync(evt.FromFullSync)
	case *events.MarkChatAsRead:
		lc = lc.Stringer("chat", evt.JID)
		extra = fullSync(evt.FromFullSync)
	case *events.ClearChat:
		lc = lc.Stringer("chat", evt.JID)
		extra = fullSync(evt.FromFullSync)
	case *events.DeleteChat:
		lc = lc.Stringer("chat", evt.JID)
		extra = fullSync(evt.FromFullSync)
	case *events.Star:
		lc = lc.Stringer("chat", evt.ChatJID).Stringer("sender", evt.SenderJID).Str("stanza", evt.MessageID)
		extra = fullSync(evt.FromFullSync)
	case *events.DeleteForMe:
		lc = lc.Stringer("chat", evt.ChatJID).Stringer("sender", evt.SenderJID).Str("stanza", evt.MessageID)
		extra = fullSync(evt.FromFullSync)
	case *events.MediaRetry:
		lc = lc.Stringer("chat", evt.ChatID).Str("stanza", evt.MessageID)
	case *events.JoinedGroup:
		lc = lc.Stringer("chat", evt.JID)
	case *events.GroupInfo:
		lc = lc.Stringer("chat", evt.JID)
	case *events.Picture:
		lc = lc.Stringer("jid", evt.JID)
	case *events.IdentityChange:
		lc = lc.Stringer("jid", evt.JID)
	case *events.UserAbout:
		lc = lc.Stringer("jid", evt.JID)
	case *events.AppStateSyncComplete:
		extra = func(e *zerolog.Event) *zerolog.Event {
			return e.Str("collection", string(evt.Name)).Uint64("version", evt.Version).Bool("recovery", evt.Recovery)
		}
	case *events.AppStateSyncError:
		extra = func(e *zerolog.Event) *zerolog.Event {
			return e.Str("collection", string(evt.Name)).Err(evt.Error).Bool("full_sync", evt.FullSync)
		}
	case *events.LoggedOut:
		extra = func(e *zerolog.Event) *zerolog.Event {
			return e.Stringer("reason", evt.Reason).Bool("on_connect", evt.OnConnect)
		}
	case *events.ConnectFailure:
		extra = func(e *zerolog.Event) *zerolog.Event { return e.Stringer("reason", evt.Reason) }
	case *events.TemporaryBan:
		extra = func(e *zerolog.Event) *zerolog.Event { return e.Stringer("code", evt.Code).Dur("expire", evt.Expire) }
	case *events.OfflineSyncPreview:
		extra = func(e *zerolog.Event) *zerolog.Event { return e.Int("total", evt.Total).Int("messages", evt.Messages) }
	case *events.OfflineSyncCompleted:
		extra = func(e *zerolog.Event) *zerolog.Event { return e.Int("count", evt.Count) }
	}
	l := lc.Logger()
	extra(l.Info()).Msg("event")
	return l.WithContext(ctx)
}

func fullSync(full bool) func(*zerolog.Event) *zerolog.Event {
	return func(e *zerolog.Event) *zerolog.Event { return e.Bool("full_sync", full) }
}
