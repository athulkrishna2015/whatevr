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
// document form). The post is stored as our own status update so it shows in
// the `status` feed; unlike chat sends it never touches the chats table.
func (c *Client) PostStatus(ctx context.Context, text, path, caption string) (appstore.StatusUpdate, error) {
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
		if _, err := client.SendMessage(ctx, types.StatusBroadcastJID,
			&waE2E.Message{Conversation: proto.String(text)}); err != nil {
			return appstore.StatusUpdate{}, err
		}
		return c.storeOwnStatus(ctx, messageID, appstore.StatusUpdateInput{
			Kind: "text",
			Text: text,
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
