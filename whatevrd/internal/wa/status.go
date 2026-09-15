package wa

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// isStatusBroadcast reports whether an event is a contact status (story)
// rather than a chat message. Statuses arrive as ordinary events.Message from
// status@broadcast; ingesting them as chat messages would materialize a bogus
// "status" chat row, so handleMessage routes them here instead.
func isStatusBroadcast(evt *events.Message) bool {
	return evt != nil && evt.Info.Chat == types.StatusBroadcastJID
}

// DownloadStatusMedia fetches a status payload into the cache (no-op when
// already there) and publishes the change. Text statuses fail rejected.
func (c *Client) DownloadStatusMedia(ctx context.Context, statusID string) (appstore.StatusUpdate, error) {
	statusID = strings.TrimSpace(statusID)
	if statusID == "" {
		return appstore.StatusUpdate{}, app.NewCommandError(app.CommandErrorInvalidArgument, "status_id is required")
	}
	status, err := c.store.GetStatusUpdate(ctx, statusID)
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	if strings.TrimSpace(status.MediaLocalPath) != "" {
		if _, err := os.Stat(status.MediaLocalPath); err == nil {
			return status, nil
		}
	}
	return c.downloadStatusMedia(ctx, status)
}

// defaultStatusBackground is the text-status backdrop when the caller picks
// none: WhatsApp's dark outgoing green.
const defaultStatusBackground = 0xFF075E54

// statusFont clamps a caller font id to the WhatsApp FontType enum, defaulting
// to SYSTEM (0) for anything unknown.
func statusFont(font int32) *waE2E.ExtendedTextMessage_FontType {
	switch waE2E.ExtendedTextMessage_FontType(font) {
	case waE2E.ExtendedTextMessage_SYSTEM,
		waE2E.ExtendedTextMessage_SYSTEM_TEXT,
		waE2E.ExtendedTextMessage_FB_SCRIPT,
		waE2E.ExtendedTextMessage_SYSTEM_BOLD,
		waE2E.ExtendedTextMessage_MORNINGBREEZE_REGULAR,
		waE2E.ExtendedTextMessage_CALISTOGA_REGULAR,
		waE2E.ExtendedTextMessage_EXO2_EXTRABOLD,
		waE2E.ExtendedTextMessage_COURIERPRIME_BOLD:
		f := waE2E.ExtendedTextMessage_FontType(font)
		return &f
	default:
		f := waE2E.ExtendedTextMessage_SYSTEM
		return &f
	}
}

// recordStatusViewers files viewed receipts on status@broadcast as status
// viewers. Only receipts for our own statuses (sender "me" rows) are kept;
// anything else is someone else's business.
func (c *Client) recordStatusViewers(ctx context.Context, evt *events.Receipt) {
	if evt == nil || len(evt.MessageIDs) == 0 {
		return
	}
	viewer := c.canonicalParticipantJID(ctx, evt.Sender)
	if viewer == "" {
		return
	}
	own := c.ownParticipantJIDs()
	if own[viewer] {
		return
	}
	viewedAt := evt.Timestamp
	if viewedAt.IsZero() {
		viewedAt = time.Now()
	}
	changed := false
	for _, messageID := range evt.MessageIDs {
		statusID := "status:" + string(messageID)
		status, err := c.store.GetStatusUpdate(ctx, statusID)
		if err != nil || strings.TrimSpace(status.SenderID) != "me" {
			continue
		}
		if err := c.store.RecordStatusViewer(ctx, statusID, viewer, viewedAt); err != nil {
			c.log.Warnf("Failed to record status viewer for %s: %v", statusID, err)
			continue
		}
		changed = true
	}
	if changed {
		c.daemon.PublishStatusChanged()
	}
}

// MarkStatusViewed flags a status as seen locally and publishes it. Viewed
// receipts to the sender are not sent yet; this only drives local state.
func (c *Client) MarkStatusViewed(ctx context.Context, statusID string) (appstore.StatusUpdate, error) {
	statusID = strings.TrimSpace(statusID)
	if statusID == "" {
		return appstore.StatusUpdate{}, app.NewCommandError(app.CommandErrorInvalidArgument, "status_id is required")
	}
	updated, err := c.store.MarkStatusViewed(ctx, statusID)
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	c.daemon.PublishStatusChanged()
	return updated, nil
}

// PostStatus publishes a text or media status (story) to status@broadcast.
// Exactly one of text or path must be set; path reuses the send.media
// classifier (photo, video, audio; documents are rejected — statuses have no
// document form). Text statuses post as ExtendedTextMessage with a background
// color and font: a bare Conversation renders as "unsupported" on official
// clients. The post is stored as our own status update so it shows in the
// `status` feed; unlike chat sends it never touches the chats table.
func (c *Client) PostStatus(ctx context.Context, text, path, caption string, background uint32, font int32) (appstore.StatusUpdate, error) {
	client := c.currentClient()
	if client == nil {
		return appstore.StatusUpdate{}, app.NewCommandError(app.CommandErrorNotConnected, "WhatsApp client is not initialized")
	}
	if client.Store.ID == nil {
		return appstore.StatusUpdate{}, app.NewCommandError(app.CommandErrorNotLoggedIn, "WhatsApp session is not logged in")
	}

	text = strings.TrimSpace(text)
	path = strings.TrimSpace(path)
	if (text == "") == (path == "") {
		return appstore.StatusUpdate{}, app.NewCommandError(app.CommandErrorInvalidArgument, "exactly one of text or path is required")
	}

	messageID := client.GenerateMessageID()
	if text != "" {
		if background == 0 {
			background = defaultStatusBackground
		}
		statusMsg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:           proto.String(text),
			BackgroundArgb: proto.Uint32(background),
			TextArgb:       proto.Uint32(0xFFFFFFFF),
			Font:           statusFont(font),
		}}
		if _, err := client.SendMessage(ctx, types.StatusBroadcastJID, statusMsg); err != nil {
			return appstore.StatusUpdate{}, err
		}
		return c.storeOwnStatus(ctx, messageID, appstore.StatusUpdateInput{
			Kind:     "text",
			Text:     text,
			TextBG:   background,
			TextFont: int32(statusMsg.GetExtendedTextMessage().GetFont()),
		})
	}

	data, mimeType, _, mediaKind, _, err := readOutboundMedia(path, MediaSendOptions{})
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	if mediaKind == appstore.MediaKindDocument {
		return appstore.StatusUpdate{}, app.NewCommandError(app.CommandErrorInvalidArgument, "statuses cannot be documents: send a photo, video or audio file")
	}
	mediaDir := filepath.Join(c.paths.MediaCacheDir, "status")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return appstore.StatusUpdate{}, err
	}
	localPath := filepath.Join(mediaDir, safeMediaFileName("own-"+string(messageID), mediaExtension(mimeType)))
	if err := writeFileAtomic(localPath, data, 0o600); err != nil {
		return appstore.StatusUpdate{}, err
	}

	envelope, inner, uploadType, err := buildOutgoingMediaMessage(appstore.Message{
		Text:          caption,
		MediaKind:     mediaKind,
		MediaMimeType: mimeType,
	}, data, mimeType)
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	resp, err := client.Upload(ctx, data, uploadType)
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	fillOutgoingMediaUpload(inner, resp)
	if _, err := client.SendMessage(ctx, types.StatusBroadcastJID, envelope); err != nil {
		return appstore.StatusUpdate{}, err
	}
	payload, err := proto.Marshal(inner)
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	stored, err := c.storeOwnStatus(ctx, messageID, appstore.StatusUpdateInput{
		Kind:          mediaKind,
		Text:          caption,
		MediaKind:     mediaKind,
		MediaMimeType: mimeType,
		MediaPayload:  payload,
	})
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	updated, err := c.store.SetStatusMediaPath(ctx, stored.ID, localPath)
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	return updated, nil
}

// DeleteStatus deletes one of our own statuses: revoke on WhatsApp (delete
// for everyone, like official clients) plus drop the local row. Others'
// statuses cannot be deleted.
func (c *Client) DeleteStatus(ctx context.Context, statusID string) error {
	client, err := c.requireConnectedClient()
	if err != nil {
		return err
	}
	statusID = strings.TrimSpace(statusID)
	if statusID == "" {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "status_id is required")
	}
	status, err := c.store.GetStatusUpdate(ctx, statusID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(status.SenderID) != "me" {
		return app.NewCommandError(app.CommandErrorRejected, "only your own statuses can be deleted")
	}
	externalID := strings.TrimPrefix(status.ID, "status:")
	if _, err := client.RevokeMessage(ctx, types.StatusBroadcastJID, types.MessageID(externalID)); err != nil {
		return err
	}
	if err := c.store.DeleteStatusUpdate(ctx, status.ID); err != nil {
		return err
	}
	c.daemon.PublishStatusChanged()
	return nil
}

func (c *Client) ListStatusViewers(ctx context.Context, statusID string) ([]appstore.StatusViewer, error) {
	return c.store.ListStatusViewers(ctx, strings.TrimSpace(statusID))
}

// ReplyToStatus sends a chat message to the status author quoting their
// status, the way official clients reply to stories: the reply lands as an
// ordinary DM carrying the status as its quote context.
func (c *Client) ReplyToStatus(ctx context.Context, statusID, text string) (appstore.SavedTextMessage, error) {
	client, err := c.requireConnectedClient()
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	statusID = strings.TrimSpace(statusID)
	if statusID == "" {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "status_id is required")
	}
	if strings.TrimSpace(text) == "" {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "text is required")
	}
	status, err := c.store.GetStatusUpdate(ctx, statusID)
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	if strings.TrimSpace(status.SenderID) == "" || strings.TrimSpace(status.SenderID) == "me" {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorRejected, "only others' statuses can be replied to")
	}
	chat, err := c.EnsureDirectChat(ctx, status.SenderID)
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}

	rpcArrival := time.Now()
	messageID := client.GenerateMessageID()
	quoteText := status.Text
	if quoteText == "" {
		quoteText = statusFallbackText(status.Kind)
	}
	saved, err := c.store.SaveTextMessage(ctx, appstore.TextMessageInput{
		ID:          internalMessageIDForChat(chat.ID, messageID),
		ChatID:      chat.ID,
		SenderID:    "me",
		Text:        text,
		Timestamp:   time.Now(),
		Direction:   appstore.DirectionOutgoing,
		Status:      appstore.StatusPending,
		CountUnread: false,
		ReplyTo: appstore.MessageReply{
			MessageID:     strings.TrimPrefix(status.ID, "status:"),
			SenderID:      status.SenderID,
			SenderName:    status.SenderName,
			Text:          quoteText,
			MediaKind:     status.MediaKind,
			MediaMimeType: status.MediaMimeType,
			Direction:     appstore.DirectionIncoming,
		},
	})
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	if saved.Inserted {
		c.beginSendTiming(saved.Message.ID, rpcArrival)
		c.daemon.PublishNewMessage(toDaemonMessage(saved.Message), toDaemonChat(saved.Chat))
	}
	c.signalSendQueue()
	return saved, nil
}

// statusFallbackText is the quote text for statuses without a caption.
func statusFallbackText(kind string) string {
	switch kind {
	case appstore.MediaKindImage:
		return "📷 Photo status"
	case appstore.MediaKindVideo:
		return "🎥 Video status"
	case appstore.MediaKindVoice:
		return "🎤 Voice status"
	case appstore.MediaKindAudio:
		return "🎵 Audio status"
	default:
		return "Status update"
	}
}

// storeOwnStatus records a status we posted so it shows in the `status` feed
// next to everyone else's. The ID reuses the sent message ID.
func (c *Client) storeOwnStatus(ctx context.Context, messageID types.MessageID, input appstore.StatusUpdateInput) (appstore.StatusUpdate, error) {
	input.ID = "status:" + string(messageID)
	input.SenderID = "me"
	input.Timestamp = time.Now()
	stored, _, err := c.store.SaveStatusUpdate(ctx, input)
	if err != nil {
		return appstore.StatusUpdate{}, err
	}
	c.daemon.PublishStatusChanged()
	return stored, nil
}

// ingestStatusUpdate stores a contact status outside the chat store and
// publishes it to the `status` view. Text, photo, video and audio statuses
// are kept; protocol noise (reactions, votes, ...) is ignored. Nothing here
// notifies or counts unread — statuses are browsed, not pushed.
func (c *Client) ingestStatusUpdate(ctx context.Context, evt *events.Message) {
	if evt == nil || evt.Message == nil || evt.Info.ID == "" {
		return
	}
	sender := senderID(evt.Info)
	if sender == "" {
		return
	}
	var senderName string
	if sender != "me" {
		if jid, err := types.ParseJID(sender); err == nil {
			senderName = c.senderName(ctx, jid)
		}
	}

	input := appstore.StatusUpdateInput{
		ID:        "status:" + string(evt.Info.ID),
		SenderID:  sender,
		Timestamp: messageTimestamp(evt.Info, ingestOptions{}, evt.SourceWebMsg),
	}
	if input.Timestamp.IsZero() {
		input.Timestamp = evt.Info.Timestamp
	}

	if text := strings.TrimSpace(textFromMessage(evt.Message)); text != "" {
		input.Kind = "text"
		input.Text = text
		input.TextBG, input.TextFont = statusTextStyle(evt.Message)
	} else if kind, mime, payload, duration, size, caption, ok := statusMediaPayload(evt.Message); ok {
		input.Kind = kind
		input.Text = caption
		input.MediaKind = kind
		input.MediaMimeType = mime
		input.MediaPayload = payload
		input.MediaDurationSecs = duration
		input.MediaSizeBytes = size
	} else {
		return
	}
	if senderName != "" {
		input.SenderName = senderName
	}

	if _, inserted, err := c.store.SaveStatusUpdate(ctx, input); err != nil {
		c.log.Warnf("Failed to store status %s: %v", input.ID, err)
		return
	} else if inserted {
		c.daemon.PublishStatusChanged()
	}
}

// statusTextStyle extracts a text status's background color (ARGB, 0 when
// unset) and WhatsApp font id from its ExtendedTextMessage. Plain
// Conversation texts carry neither.
func statusTextStyle(msg *waE2E.Message) (uint32, int32) {
	extended := msg.GetExtendedTextMessage()
	if extended == nil {
		return 0, 0
	}
	return extended.GetBackgroundArgb(), int32(extended.GetFont())
}

// statusMediaPayload extracts the storable media facts from a status message:
// the gallery kind, MIME type, wire payload (media keys for later download),
// duration, size and caption. View-once statuses stay tombstoned like
// view-once chat media: the payload is deliberately not kept.
func statusMediaPayload(msg *waE2E.Message) (kind, mime string, payload []byte, duration int32, size int64, caption string, ok bool) {
	if msg == nil {
		return "", "", nil, 0, 0, "", false
	}
	if img := msg.GetImageMessage(); img != nil {
		payload, err := proto.Marshal(img)
		if err != nil {
			return "", "", nil, 0, 0, "", false
		}
		return appstore.MediaKindImage, defaultMime(img.GetMimetype(), "image/jpeg"),
			payload, 0, int64(img.GetFileLength()), img.GetCaption(), true
	}
	if video := msg.GetVideoMessage(); video != nil {
		payload, err := proto.Marshal(video)
		if err != nil {
			return "", "", nil, 0, 0, "", false
		}
		kind := appstore.MediaKindVideo
		if video.GetGifPlayback() {
			kind = appstore.MediaKindGIF
		}
		return kind, defaultMime(video.GetMimetype(), "video/mp4"),
			payload, int32(video.GetSeconds()), int64(video.GetFileLength()), video.GetCaption(), true
	}
	if audio := msg.GetAudioMessage(); audio != nil {
		payload, err := proto.Marshal(audio)
		if err != nil {
			return "", "", nil, 0, 0, "", false
		}
		kind := appstore.MediaKindAudio
		if audio.GetPTT() {
			kind = appstore.MediaKindVoice
		}
		return kind, defaultMime(audio.GetMimetype(), "audio/ogg; codecs=opus"),
			payload, int32(audio.GetSeconds()), int64(audio.GetFileLength()), "", true
	}
	return "", "", nil, 0, 0, "", false
}
