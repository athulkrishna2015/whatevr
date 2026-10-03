package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// Decode is the row this ingest would store for a live message, built and
// not stored. the new core reads through it until its own content decoder
// replaces this package.
func (c *Client) Decode(ctx context.Context, evt *events.Message) (appstore.Message, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.Message{}, false
	}
	evt.Message = unwrapNestedMessage(evt.Message)
	return c.decode(ctx, evt, ingestOptions{source: sourceLive})
}

// DecodeHistory is Decode for a message out of a history blob, stubs
// included.
func (c *Client) DecodeHistory(ctx context.Context, chat types.JID, web *waWeb.WebMessageInfo) (appstore.Message, bool) {
	if payload, ts, ok := c.historyStubSystemPayload(ctx, web); ok {
		return systemMessage(chat, payload, ts), true
	}
	client := c.currentClient()
	if client == nil || web.GetMessage() == nil {
		return appstore.Message{}, false
	}
	evt, err := client.ParseWebMessage(chat, web)
	if err != nil || evt.Message == nil {
		return appstore.Message{}, false
	}
	evt.Message = unwrapNestedMessage(evt.Message)
	return c.decode(ctx, evt, ingestOptions{source: sourceHistorySync, historyStatus: mapWebMessageStatus(web)})
}

func (c *Client) decode(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.Message, bool) {
	if in, ok := c.textMessageInput(ctx, evt, opts); ok {
		return messageFromInput(appstore.MediaMessageInput{TextMessageInput: in}), true
	}
	if in, ok := c.mediaMessageInput(ctx, evt, opts); ok {
		return messageFromInput(in), true
	}
	return appstore.Message{}, false
}

// DecodeContent is the content of an edit decoded against the message it
// edits: the text or caption it now says.
func (c *Client) DecodeContent(ctx context.Context, evt *events.Message, content *waE2E.Message) (appstore.Message, bool) {
	cp := *evt
	cp.Message = unwrapNestedMessage(content)
	return c.decode(ctx, &cp, ingestOptions{source: sourceLive})
}

func messageFromInput(in appstore.MediaMessageInput) appstore.Message {
	return appstore.Message{
		ID:                      in.ID,
		ChatID:                  in.ChatID,
		SenderID:                in.SenderID,
		SenderName:              in.SenderName,
		Text:                    in.Text,
		TimestampUnix:           in.Timestamp.Unix(),
		SortMS:                  in.Timestamp.UnixMilli(),
		Direction:               in.Direction,
		Status:                  in.Status,
		IsForwarded:             in.IsForwarded,
		ReplyTo:                 in.ReplyTo,
		Mentions:                in.Mentions,
		PayloadJSON:             in.PayloadJSON,
		MediaKind:               in.MediaKind,
		MediaMimeType:           in.MediaMimeType,
		MediaLocalPath:          in.MediaLocalPath,
		MediaThumbnailLocalPath: in.MediaThumbnailLocalPath,
		MediaWidth:              in.MediaWidth,
		MediaHeight:             in.MediaHeight,
		MediaAnimated:           in.MediaAnimated,
		MediaPayload:            in.MediaPayload,
		MediaCacheKey:           in.MediaCacheKey,
		MediaDurationSecs:       in.MediaDurationSecs,
		MediaSizeBytes:          in.MediaSizeBytes,
		MediaFileName:           in.MediaFileName,
		MediaPageCount:          in.MediaPageCount,
		MediaWaveform:           in.MediaWaveform,
		PayloadSummary:          in.PayloadSummary,
		AlbumParentID:           in.AlbumParentID,
		AlbumIndex:              in.AlbumIndex,
	}
}

// SystemMessage is a system row from what happened, worded the way every
// other system row is.
func SystemMessage(chat types.JID, payload appstore.SystemPayload, ts time.Time) appstore.Message {
	return systemMessage(chat, payload, ts)
}

func systemMessage(chat types.JID, payload appstore.SystemPayload, ts time.Time) appstore.Message {
	payloadJSON, _ := appstore.EncodePayload(appstore.MessagePayload{System: &payload})
	sender := chat.String()
	name := ""
	if payload.Actor != nil {
		if payload.Actor.JID != "" {
			sender = payload.Actor.JID
		}
		name = payload.Actor.Name
	}
	return appstore.Message{
		ChatID:         chat.String(),
		SenderID:       sender,
		SenderName:     name,
		TimestampUnix:  ts.Unix(),
		SortMS:         ts.UnixMilli(),
		Direction:      appstore.DirectionIncoming,
		Status:         appstore.StatusDelivered,
		MediaKind:      appstore.MediaKindSystem,
		PayloadJSON:    payloadJSON,
		PayloadSummary: systemSummary(payload),
	}
}
