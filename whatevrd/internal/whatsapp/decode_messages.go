package whatsapp

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	appstore "whatevrd/internal/store"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"

	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ingestSource distinguishes a freshly-received WhatsApp message from a
// history-sync backfill. The two paths share storage logic but diverge on
// notification, event publication, and status mapping.
type ingestSource int

const (
	sourceLive ingestSource = iota
	sourceHistorySync
	sourceOfflineSync
)

type ingestOptions struct {
	source ingestSource
	// historyStatus is set only for sourceHistorySync; mapped from
	// WebMessageInfo.Status. Empty string means "no override".
	historyStatus     string
	timestampOverride time.Time
}

// mediaMessageInput picks the row shape for whatever this message turns out to
// be, then tags it with the album that owns it. History sync calls this
// directly, so anything that belongs to every media kind belongs here rather
// than in the ingest path above it.
func (d *Decoder) mediaMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	input, ok := d.mediaMessageInputForKind(ctx, evt, opts)
	if !ok {
		return input, false
	}
	applyAlbumAssociation(&input, evt.Message)
	return input, true
}

func (d *Decoder) mediaMessageInputForKind(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if input, ok := d.imageMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.stickerMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.videoMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.audioMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.documentMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.liveUpdateInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.locationMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.contactMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.pollMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.groupInviteMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.eventMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.albumMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.interactiveMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.commerceMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.stickerPackMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	if input, ok := d.callLogMessageInput(ctx, evt, opts); ok {
		return input, true
	}
	return d.unsupportedMessageInput(ctx, evt, opts)
}

// mediaInputBase builds the half of a media row that has nothing to do with the
// media itself: identity, naming, direction, unread accounting and the reply
// quote. The chat id comes back with it because callers need it for thumbnail
// paths. ok=false means the event is not storable at all.
func (d *Decoder) mediaInputBase(ctx context.Context, evt *events.Message, opts ingestOptions, text string, contextInfo *waE2E.ContextInfo) (appstore.TextMessageInput, string, bool) {
	info := evt.Info
	chatJID := d.names.Norm(info.Chat)
	chatID := chatJID.String()
	if chatID == "" || info.ID == "" {
		return appstore.TextMessageInput{}, "", false
	}

	direction, status := messageDirectionAndStatus(info, opts)
	return appstore.TextMessageInput{
		ID:         internalMessageIDForChat(chatID, info.ID),
		ChatID:     chatID,
		SenderID:   senderID(info),
		SenderName: d.names.Name(senderJID(info)),
		Text:       text,
		Timestamp:  messageTimestamp(info, opts, evt.SourceWebMsg),
		Direction:  direction,
		Status:     status,
		IsGroup:    info.IsGroup,
		ReplyTo:    d.replyFromContextInfo(ctx, chatID, contextInfo),
		// Mentions come from the context info the caller already picked out for
		// this kind, not from re-deriving it: a captioned photo or video can
		// @-mention people, and until now the media path dropped every one.
		Mentions: d.resolveMentions(ctx, mentionedJIDsFromContextInfo(contextInfo)),
		// The sender's client marks forwarded copies in the same context
		// info; without this only our own forwards (flagged at send time)
		// ever rendered a forwarded header, so forwarded-to-us rows showed
		// it on the phone but never on the desktop.
		IsForwarded: contextInfo.GetIsForwarded(),
	}, chatID, true
}

// videoMessageInput covers all three video-shaped payloads, which share the
// VideoMessage type on the wire: ordinary videos, GIFs (GifPlayback, rendered
// muted and looping), and round video notes (PtvMessage).
func (d *Decoder) videoMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	// View-once media is meant to be opened on the phone only; it falls
	// through to the unsupported-message tombstone.
	if evt.IsViewOnce {
		return appstore.MediaMessageInput{}, false
	}

	var (
		videoMsg *waE2E.VideoMessage
		kind     string
	)
	switch {
	case evt.Message.GetPtvMessage() != nil:
		videoMsg = evt.Message.GetPtvMessage()
		kind = appstore.MediaKindVideoNote
	case evt.Message.GetVideoMessage() != nil:
		videoMsg = evt.Message.GetVideoMessage()
		kind = appstore.MediaKindVideo
		if videoMsg.GetGifPlayback() {
			kind = appstore.MediaKindGIF
		}
	default:
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, videoMsg.GetCaption(), videoMsg.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload, err := proto.Marshal(videoMsg)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("stanza", evt.Info.ID).Msg("serialize video metadata")
		return appstore.MediaMessageInput{}, false
	}
	mimeType := videoMsg.GetMimetype()
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	return appstore.MediaMessageInput{
		TextMessageInput:        base,
		MediaKind:               kind,
		MediaMimeType:           mimeType,
		MediaThumbnailLocalPath: d.saveMessageThumbnail(chatID, base.ID, videoMsg.GetJPEGThumbnail()),
		MediaWidth:              int32(videoMsg.GetWidth()),
		MediaHeight:             int32(videoMsg.GetHeight()),
		MediaAnimated:           kind == appstore.MediaKindGIF,
		MediaPayload:            payload,
		MediaDurationSecs:       int32(videoMsg.GetSeconds()),
		MediaSizeBytes:          int64(videoMsg.GetFileLength()),
	}, true
}

// audioMessageInput covers recorded voice notes (PTT, which carry a waveform)
// and shared audio files, which differ only by the PTT flag.
func (d *Decoder) audioMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	if evt.IsViewOnce {
		return appstore.MediaMessageInput{}, false
	}

	audioMsg := evt.Message.GetAudioMessage()
	if audioMsg == nil {
		return appstore.MediaMessageInput{}, false
	}

	kind := appstore.MediaKindAudio
	if audioMsg.GetPTT() {
		kind = appstore.MediaKindVoice
	}

	// AudioMessage has no caption field: the bubble is the waveform alone.
	base, _, ok := d.mediaInputBase(ctx, evt, opts, "", audioMsg.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload, err := proto.Marshal(audioMsg)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("stanza", evt.Info.ID).Msg("serialize audio metadata")
		return appstore.MediaMessageInput{}, false
	}
	mimeType := audioMsg.GetMimetype()
	if mimeType == "" {
		mimeType = "audio/ogg; codecs=opus"
	}

	return appstore.MediaMessageInput{
		TextMessageInput:  base,
		MediaKind:         kind,
		MediaMimeType:     mimeType,
		MediaPayload:      payload,
		MediaDurationSecs: int32(audioMsg.GetSeconds()),
		MediaSizeBytes:    int64(audioMsg.GetFileLength()),
		MediaWaveform:     normalizedWaveform(audioMsg.GetWaveform()),
	}, true
}

func (d *Decoder) documentMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	if evt.IsViewOnce {
		return appstore.MediaMessageInput{}, false
	}

	// DocumentWithCaptionMessage arrives already unwrapped (whatsmeow calls
	// UnwrapRaw for both live messages and history), so the caption is on the
	// inner DocumentMessage by the time we see it.
	docMsg := evt.Message.GetDocumentMessage()
	if docMsg == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, docMsg.GetCaption(), docMsg.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload, err := proto.Marshal(docMsg)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("stanza", evt.Info.ID).Msg("serialize document metadata")
		return appstore.MediaMessageInput{}, false
	}
	mimeType := docMsg.GetMimetype()
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	fileName := strings.TrimSpace(docMsg.GetFileName())
	if fileName == "" {
		fileName = strings.TrimSpace(docMsg.GetTitle())
	}

	return appstore.MediaMessageInput{
		TextMessageInput:        base,
		MediaKind:               appstore.MediaKindDocument,
		MediaMimeType:           mimeType,
		MediaThumbnailLocalPath: d.saveMessageThumbnail(chatID, base.ID, docMsg.GetJPEGThumbnail()),
		MediaWidth:              int32(docMsg.GetThumbnailWidth()),
		MediaHeight:             int32(docMsg.GetThumbnailHeight()),
		MediaPayload:            payload,
		MediaSizeBytes:          int64(docMsg.GetFileLength()),
		MediaFileName:           fileName,
		MediaPageCount:          int32(docMsg.GetPageCount()),
	}, true
}

// waveformBuckets is the number of amplitude samples WhatsApp ships with a
// voice note, and the number the bubble draws.
const waveformBuckets = 64

// normalizedWaveform keeps a wire waveform only when it is the shape the
// renderer expects: 64 bytes of 0-100. Anything else (a sender that omitted it,
// or a client that sized it differently) is dropped so the daemon can derive a
// real one after download instead of drawing garbage.
func normalizedWaveform(waveform []byte) []byte {
	if len(waveform) != waveformBuckets {
		return nil
	}
	out := make([]byte, waveformBuckets)
	for i, v := range waveform {
		if v > 100 {
			v = 100
		}
		out[i] = v
	}
	return out
}

func (d *Decoder) imageMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	// View-once photos are meant to be opened on the phone only; they fall
	// through to the unsupported-message tombstone instead of an image bubble.
	if evt.IsViewOnce {
		return appstore.MediaMessageInput{}, false
	}

	imgMsg := evt.Message.GetImageMessage()
	if imgMsg == nil {
		return appstore.MediaMessageInput{}, false
	}

	info := evt.Info
	chatJID := d.names.Norm(info.Chat)
	chatID := chatJID.String()
	if chatID == "" || info.ID == "" {
		return appstore.MediaMessageInput{}, false
	}

	direction, status := messageDirectionAndStatus(info, opts)

	payload, err := proto.Marshal(imgMsg)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("stanza", info.ID).Msg("serialize image metadata")
		return appstore.MediaMessageInput{}, false
	}
	mimeType := imgMsg.GetMimetype()
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	thumbnailLocalPath := d.saveMessageThumbnail(chatID, internalMessageIDForChat(chatID, info.ID), imgMsg.GetJPEGThumbnail())

	caption := imgMsg.GetCaption()
	return appstore.MediaMessageInput{
		TextMessageInput: appstore.TextMessageInput{
			ID:          internalMessageIDForChat(chatID, info.ID),
			ChatID:      chatID,
			SenderID:    senderID(info),
			SenderName:  d.names.Name(senderJID(info)),
			Text:        caption,
			Timestamp:   messageTimestamp(info, opts, evt.SourceWebMsg),
			Direction:   direction,
			Status:      status,
			IsGroup:     info.IsGroup,
			ReplyTo:     d.replyFromContextInfo(ctx, chatID, imgMsg.GetContextInfo()),
			IsForwarded: imgMsg.GetContextInfo().GetIsForwarded(),
		},
		MediaKind:               appstore.MediaKindImage,
		MediaMimeType:           mimeType,
		MediaThumbnailLocalPath: thumbnailLocalPath,
		MediaWidth:              int32(imgMsg.GetWidth()),
		MediaHeight:             int32(imgMsg.GetHeight()),
		MediaPayload:            payload,
	}, true
}

func (d *Decoder) stickerMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}

	stickerMsg := evt.Message.GetStickerMessage()
	if stickerMsg == nil {
		return appstore.MediaMessageInput{}, false
	}

	info := evt.Info
	chatJID := d.names.Norm(info.Chat)
	chatID := chatJID.String()
	if chatID == "" || info.ID == "" {
		return appstore.MediaMessageInput{}, false
	}

	direction, status := messageDirectionAndStatus(info, opts)
	payload, err := proto.Marshal(stickerMsg)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("stanza", info.ID).Msg("serialize sticker metadata")
		return appstore.MediaMessageInput{}, false
	}
	mimeType := stickerMsg.GetMimetype()
	if mimeType == "" {
		mimeType = "image/webp"
	}
	thumbnailLocalPath := d.saveMessageThumbnailWithExtension(chatID, internalMessageIDForChat(chatID, info.ID), stickerMsg.GetPngThumbnail(), ".thumb.png")

	return appstore.MediaMessageInput{
		TextMessageInput: appstore.TextMessageInput{
			ID:          internalMessageIDForChat(chatID, info.ID),
			ChatID:      chatID,
			SenderID:    senderID(info),
			SenderName:  d.names.Name(senderJID(info)),
			Timestamp:   messageTimestamp(info, opts, evt.SourceWebMsg),
			Direction:   direction,
			Status:      status,
			IsGroup:     info.IsGroup,
			ReplyTo:     d.replyFromContextInfo(ctx, chatID, stickerMsg.GetContextInfo()),
			IsForwarded: stickerMsg.GetContextInfo().GetIsForwarded(),
		},
		MediaKind:               appstore.MediaKindSticker,
		MediaMimeType:           mimeType,
		MediaThumbnailLocalPath: thumbnailLocalPath,
		MediaWidth:              int32(stickerMsg.GetWidth()),
		MediaHeight:             int32(stickerMsg.GetHeight()),
		MediaAnimated:           stickerMsg.GetIsAnimated(),
		MediaPayload:            payload,
		MediaCacheKey:           stickerMediaCacheKey(stickerMsg),
	}, true
}

// unsupportedMessageInput stores an honest tombstone for real messages whose
// payload whatevr cannot render yet (documents, voice notes, video, polls,
// view-once media, ...). The human-readable label rides in the text column so
// bubbles, chat previews and notifications all pick it up for free. Protocol
// noise (poll votes, app-state keys, ...) carries no row and stays invisible.
//
// A kind this build has never heard of gets a generic tombstone rather than
// nothing: WhatsApp ships new message types faster than whatevr learns them,
// and a grey bubble is a hole somebody can see and report. The payload is kept
// so the row can be upgraded once the kind is understood.
func (d *Decoder) unsupportedMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	label, ok := unsupportedMessageLabel(evt)
	if !ok {
		// A text payload that got this far is an empty envelope, not a kind
		// nobody has written code for. The text path owned it and declined, so
		// there is no row and nothing to warn about.
		if evt.Message.Conversation != nil || evt.Message.ExtendedTextMessage != nil {
			return appstore.MediaMessageInput{}, false
		}
		field, unknown := unrecognizedPayloadField(evt.Message)
		if !unknown {
			return appstore.MediaMessageInput{}, false
		}
		zerolog.Ctx(ctx).Warn().Str("stanza", evt.Info.ID).Str("payload", field).Msg("storing a tombstone, nothing handles the payload")
		label = "Unsupported message"
	}

	base, _, ok := d.mediaInputBase(ctx, evt, opts, label, unsupportedContextInfo(evt.Message))
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindUnsupported,
		// Keep the marshalled payload even though nothing reads it today. A
		// tombstone that stored nothing could never be upgraded when a later
		// build learned its kind, which is why every location and poll ingested
		// before this change stays grey forever. This one will not.
		MediaPayload: marshalMessagePayload(evt.Message),
	}, true
}

// marshalMessagePayload serializes a whole waE2E message for later re-parsing.
// A failure is not worth losing the row over: the tombstone still has its label.
func marshalMessagePayload(message *waE2E.Message) []byte {
	if message == nil {
		return nil
	}
	payload, err := proto.Marshal(message)
	if err != nil {
		return nil
	}
	return payload
}

// maxMessageUnwrapDepth caps the peel below. Nothing legitimate nests this
// deep, and an unbounded loop on attacker-shaped input is not worth the risk.
const maxMessageUnwrapDepth = 8

// unwrapNestedMessage peels the wrappers whatsmeow's own UnwrapRaw leaves
// alone. Every one of these holds an ordinary message, so leaving it wrapped
// means the payload matches no builder, writes no row, and logs nothing: the
// message simply is not there. Wrappers that are not ordinary messages (status,
// newsletter, bot and settings families) are deliberately left for the
// tombstone, which at least makes them visible.
//
// associatedChildMessage is deliberately not among them. It is not a message
// that arrived wrapped, it is a companion of one that arrived separately: an
// HD photo crosses as the ordinary image and then again, as its own stanza,
// carrying the full-size copy in this wrapper. Peeling it made the companion a
// message of its own, so every HD photo drew twice, at two resolutions, seconds
// apart. See silentMessageFields.
func unwrapNestedMessage(msg *waE2E.Message) *waE2E.Message {
	for range maxMessageUnwrapDepth {
		var inner *waE2E.Message
		switch {
		case msg.GetGroupMentionedMessage().GetMessage() != nil:
			inner = msg.GetGroupMentionedMessage().GetMessage()
		case msg.GetSpoilerMessage().GetMessage() != nil:
			inner = msg.GetSpoilerMessage().GetMessage()
		case msg.GetPollCreationMessageV4().GetMessage() != nil:
			inner = msg.GetPollCreationMessageV4().GetMessage()
		case msg.GetPollCreationOptionImageMessage().GetMessage() != nil:
			inner = msg.GetPollCreationOptionImageMessage().GetMessage()
		case msg.GetAudioStickerMessage().GetMessage() != nil:
			inner = msg.GetAudioStickerMessage().GetMessage()
		case msg.GetBotForwardedMessage().GetMessage() != nil:
			inner = msg.GetBotForwardedMessage().GetMessage()
		}
		if inner == nil {
			return msg
		}
		// The secret and the reply context ride on the wrapper, and whatsmeow
		// carries them inward the same way for the wrappers it peels.
		if inner.MessageContextInfo == nil && msg.MessageContextInfo != nil {
			inner.MessageContextInfo = msg.MessageContextInfo
		}
		msg = inner
	}
	return msg
}

// unsupportedMessageLabel maps not-yet-rendered payload types to a short
// description. ok=false means the message carries nothing user-visible and
// must stay invisible (protocol messages, poll votes, reactions, ...).
//
// This runs last, after every real builder, so anything named here is a kind
// whatevr cannot draw yet. A label is the difference between an honest grey row
// and a hole in the transcript, and the payload is kept with it so a later
// build can upgrade the row in place.
// silentMessageFields are the payload fields that never become a transcript row:
// session plumbing, and the events that modify an existing message rather than
// being one. Anything else set on a message is something a person sent.
//
// It is a denylist on purpose. The allowlist below it can only ever describe
// the kinds this build already knows, and WhatsApp adds them faster than that;
// listing what is definitely not a message is the only version that stays
// correct as the protocol grows.
var silentMessageFields = map[string]bool{
	"senderKeyDistributionMessage":               true,
	"fastRatchetKeySenderKeyDistributionMessage": true,
	"protocolMessage":                            true,
	"messageContextInfo":                         true,
	"stickerSyncRmrMessage":                      true,
	"placeholderMessage":                         true,
	"groupRootKeyShare":                          true,
	"rootSecretDistributeMessage":                true,
	"botPlatformRegistrationSuccessMessage":      true,
	"acp2SettingMessage":                         true,
	// A companion of a message that arrived on its own stanza, not a message.
	// The HD half of a photo is the one that matters here: dropping it shows
	// the standard-quality image once, which is what every other client shows
	// before you ask for HD, instead of the same photo twice.
	"associatedChildMessage": true,
	// Edits to something that already has a row.
	"reactionMessage":                true,
	"encReactionMessage":             true,
	"pollUpdateMessage":              true,
	"pollAddOptionMessage":           true,
	"pollCreationOptionImageMessage": true,
	"encEventResponseMessage":        true,
	"keepInChatMessage":              true,
	"pinInChatMessage":               true,
	"eventCoverImage":                true,
	"statusLinkPreviewMetadata":      true,
	// Status and newsletter housekeeping, none of which is a chat message.
	"statusAddYours":                      true,
	"statusNotificationMessage":           true,
	"newsletterAdminProfileMessage":       true,
	"newsletterAdminProfileStatusMessage": true,
}

// unrecognizedPayloadField names a set field that is neither plumbing nor
// anything the builders above claimed, which is how a message type nobody has
// written code for is told apart from an empty envelope.
func unrecognizedPayloadField(msg *waE2E.Message) (string, bool) {
	found := ""
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if silentMessageFields[string(fd.Name())] {
			return true
		}
		found = string(fd.Name())
		return false
	})
	return found, found != ""
}

func unsupportedMessageLabel(evt *events.Message) (string, bool) {
	msg := evt.Message
	if evt.IsViewOnce {
		switch {
		case msg.GetImageMessage() != nil:
			return "View once photo", true
		case msg.GetVideoMessage() != nil:
			return "View once video", true
		case msg.GetAudioMessage() != nil:
			return "View once voice message", true
		}
	}
	switch {
	// The business family used to land here as a bare "Message". It now has its
	// own card, and the only member still on this path is the non-hydrated
	// TemplateMessage, whose contents are a template name to be looked up in a
	// catalogue a linked device is never sent.
	case msg.GetTemplateMessage() != nil:
		return "Message", true
	case msg.GetHighlyStructuredMessage() != nil:
		return "Message", true
	case msg.GetConditionalRevealMessage() != nil:
		return "Message", true
	case msg.GetScheduledCallCreationMessage() != nil:
		return "Scheduled call", true
	case msg.GetScheduledCallEditMessage() != nil:
		return "Scheduled call updated", true
	case msg.GetEventInviteMessage() != nil:
		return "Event invite", true
	case msg.GetCommentMessage() != nil:
		return "Comment", true
	case msg.GetMusicMessage() != nil:
		return "Music", true
	case msg.GetRichResponseMessage() != nil:
		return "Meta AI response", true
	case msg.GetContactsArrayMessage() != nil:
		return "Contacts", true
	case msg.GetRequestPhoneNumberMessage() != nil:
		return "Phone number request", true
	case msg.GetNewsletterAdminInviteMessage() != nil:
		return "Channel admin invite", true
	case msg.GetNewsletterFollowerInviteMessageV2() != nil:
		return "Channel invite", true
	case msg.GetMessageHistoryBundle() != nil:
		return "Chat history", true
	case msg.GetPollResultSnapshotMessage() != nil, msg.GetPollResultSnapshotMessageV3() != nil:
		return "Poll results", true
	case msg.GetInvoiceMessage() != nil:
		return "Invoice", true
	case msg.GetDeclinePaymentRequestMessage() != nil:
		return "Payment request declined", true
	case msg.GetCancelPaymentRequestMessage() != nil:
		return "Payment request cancelled", true
	case msg.GetPaymentReminderMessage() != nil:
		return "Payment reminder", true
	case msg.GetSplitPaymentMessage() != nil:
		return "Split payment", true
	case msg.GetSplitPaymentUpdateMessage() != nil:
		return "Split payment update", true
	}
	return "", false
}

// unsupportedContextInfo pulls reply context out of the payload types the
// tombstone path covers, so quoted replies still show their preview.
func unsupportedContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	return contextInfoFromMessage(msg)
}

func stickerMediaCacheKey(sticker *waE2E.StickerMessage) string {
	if sticker == nil {
		return ""
	}
	if hash := sticker.GetFileSHA256(); len(hash) > 0 {
		return hex.EncodeToString(hash)
	}
	if hash := sticker.GetFileEncSHA256(); len(hash) > 0 {
		return hex.EncodeToString(hash)
	}
	return ""
}

func (d *Decoder) textMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.TextMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.TextMessageInput{}, false
	}

	text := textFromMessage(evt.Message)
	// Only a message with nothing in it is declined. Spaces and tabs are
	// content: somebody sent them, and rendering an empty bubble is honest,
	// where dropping the row loses a message that is really there.
	if text == "" {
		return appstore.TextMessageInput{}, false
	}

	info := evt.Info
	chatJID := d.names.Norm(info.Chat)
	chatID := chatJID.String()
	if chatID == "" {
		return appstore.TextMessageInput{}, false
	}
	if info.ID == "" {
		return appstore.TextMessageInput{}, false
	}

	direction, status := messageDirectionAndStatus(info, opts)

	input := appstore.TextMessageInput{
		ID:          internalMessageIDForChat(chatID, info.ID),
		ChatID:      chatID,
		SenderID:    senderID(info),
		SenderName:  d.names.Name(senderJID(info)),
		Text:        text,
		Timestamp:   messageTimestamp(info, opts, evt.SourceWebMsg),
		Direction:   direction,
		Status:      status,
		IsGroup:     info.IsGroup,
		ReplyTo:     d.replyFromContextInfo(ctx, chatID, contextInfoFromMessage(evt.Message)),
		Mentions:    d.mentionsFromMessage(ctx, evt.Message),
		IsForwarded: contextInfoFromMessage(evt.Message).GetIsForwarded(),
	}

	// A link preview attaches to the row rather than replacing it: the message
	// is still its text, and only the card beside it is new.
	if preview := d.linkPreviewFromMessage(chatID, input.ID, evt.Message); preview != nil {
		encoded, err := appstore.EncodePayload(appstore.MessagePayload{LinkPreview: preview})
		if err != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Str("msg", input.ID).Msg("encode link preview")
		} else {
			input.PayloadJSON = encoded
		}
	}

	return input, true
}

// mentionedJIDsFromMessage pulls the @-mention JID list out of a message's
// context info (ExtendedTextMessage / image / sticker). The matching
// `@<userpart>` tokens already live in the message text; the renderer pairs
// them up. Returns nil when there are no mentions.
func mentionedJIDsFromMessage(message *waE2E.Message) []string {
	return mentionedJIDsFromContextInfo(contextInfoFromMessage(message))
}

// mentionedJIDsFromContextInfo is the same thing for a caller that already
// holds the right context info for its kind.
func mentionedJIDsFromContextInfo(contextInfo *waE2E.ContextInfo) []string {
	if contextInfo == nil {
		return nil
	}
	mentioned := contextInfo.GetMentionedJID()
	if len(mentioned) == 0 {
		return nil
	}
	out := make([]string, 0, len(mentioned))
	for _, jid := range mentioned {
		if trimmed := strings.TrimSpace(jid); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// mentionsFromMessage resolves each @-mention in a message to a {JID, name}
// pair, resolving display names through the same senderName chain used for the
// message author. The JID is kept verbatim so its user-part still matches the
// `@<userpart>` token the renderer looks for.
func (d *Decoder) mentionsFromMessage(ctx context.Context, message *waE2E.Message) []appstore.MessageMention {
	return d.resolveMentions(ctx, mentionedJIDsFromMessage(message))
}

func (d *Decoder) resolveMentions(ctx context.Context, jids []string) []appstore.MessageMention {
	if len(jids) == 0 {
		return nil
	}
	mentions := make([]appstore.MessageMention, 0, len(jids))
	for _, raw := range jids {
		jid, err := types.ParseJID(raw)
		if err != nil {
			mentions = append(mentions, appstore.MessageMention{JID: raw})
			continue
		}
		// Drop the leading "~" senderName prepends for unsaved push names: a
		// mention chip reads cleaner as "@Name" than "@~Name".
		name := strings.TrimPrefix(d.names.Name(jid), "~")
		mentions = append(mentions, appstore.MessageMention{JID: jid.String(), DisplayName: name})
	}
	return mentions
}

func contextInfoFromMessage(message *waE2E.Message) *waE2E.ContextInfo {
	if message == nil {
		return nil
	}
	if extended := message.GetExtendedTextMessage(); extended != nil {
		return extended.GetContextInfo()
	}
	if image := message.GetImageMessage(); image != nil {
		return image.GetContextInfo()
	}
	if sticker := message.GetStickerMessage(); sticker != nil {
		return sticker.GetContextInfo()
	}
	if video := message.GetVideoMessage(); video != nil {
		return video.GetContextInfo()
	}
	if ptv := message.GetPtvMessage(); ptv != nil {
		return ptv.GetContextInfo()
	}
	if audio := message.GetAudioMessage(); audio != nil {
		return audio.GetContextInfo()
	}
	if document := message.GetDocumentMessage(); document != nil {
		return document.GetContextInfo()
	}
	if location := message.GetLocationMessage(); location != nil {
		return location.GetContextInfo()
	}
	if live := message.GetLiveLocationMessage(); live != nil {
		return live.GetContextInfo()
	}
	if contact := message.GetContactMessage(); contact != nil {
		return contact.GetContextInfo()
	}
	if contacts := message.GetContactsArrayMessage(); contacts != nil {
		return contacts.GetContextInfo()
	}
	if poll := pollCreationFromMessage(message); poll != nil {
		return poll.GetContextInfo()
	}
	if invite := message.GetGroupInviteMessage(); invite != nil {
		return invite.GetContextInfo()
	}
	if event := message.GetEventMessage(); event != nil {
		return event.GetContextInfo()
	}
	if album := message.GetAlbumMessage(); album != nil {
		return album.GetContextInfo()
	}
	if contextInfo := businessContextInfo(message); contextInfo != nil {
		return contextInfo
	}
	return nil
}

func (d *Decoder) replyFromContextInfo(ctx context.Context, chatID string, contextInfo *waE2E.ContextInfo) appstore.MessageReply {
	if contextInfo == nil || contextInfo.GetStanzaID() == "" {
		return appstore.MessageReply{}
	}

	replyChatID := chatID
	if remoteJID := strings.TrimSpace(contextInfo.GetRemoteJID()); remoteJID != "" {
		if jid, err := types.ParseJID(remoteJID); err == nil {
			replyChatID = d.names.Norm(jid).String()
		}
	}
	if replyChatID == "" {
		return appstore.MessageReply{}
	}

	senderID, senderName := d.replySenderFromParticipant(ctx, contextInfo.GetParticipant())
	direction := ""
	if senderID == "me" {
		direction = appstore.DirectionOutgoing
	} else if senderID != "" {
		direction = appstore.DirectionIncoming
	}
	text, mediaKind, mediaMimeType := quotedReplyPreview(contextInfo.GetQuotedMessage())

	return appstore.MessageReply{
		MessageID:     internalMessageIDForChat(replyChatID, types.MessageID(contextInfo.GetStanzaID())),
		SenderID:      senderID,
		SenderName:    senderName,
		Text:          text,
		MediaKind:     mediaKind,
		MediaMimeType: mediaMimeType,
		Direction:     direction,
	}
}

func (d *Decoder) replySenderFromParticipant(ctx context.Context, participant string) (string, string) {
	participant = strings.TrimSpace(participant)
	if participant == "" {
		return "", ""
	}
	jid, err := types.ParseJID(participant)
	if err != nil {
		return participant, ""
	}
	jid = bareAvatarJID(d.names.Norm(jid))
	if d.names.Own(jid) {
		return "me", ""
	}
	return jid.String(), d.names.Name(jid)
}

func quotedReplyPreview(message *waE2E.Message) (string, string, string) {
	if message == nil {
		return "", "", ""
	}
	if text := textFromMessage(message); text != "" {
		return text, "", ""
	}
	if image := message.GetImageMessage(); image != nil {
		mimeType := image.GetMimetype()
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		return image.GetCaption(), appstore.MediaKindImage, mimeType
	}
	if sticker := message.GetStickerMessage(); sticker != nil {
		mimeType := sticker.GetMimetype()
		if mimeType == "" {
			mimeType = "image/webp"
		}
		return "", appstore.MediaKindSticker, mimeType
	}
	if ptv := message.GetPtvMessage(); ptv != nil {
		return "", appstore.MediaKindVideoNote, defaultMime(ptv.GetMimetype(), "video/mp4")
	}
	if video := message.GetVideoMessage(); video != nil {
		kind := appstore.MediaKindVideo
		if video.GetGifPlayback() {
			kind = appstore.MediaKindGIF
		}
		return video.GetCaption(), kind, defaultMime(video.GetMimetype(), "video/mp4")
	}
	if audio := message.GetAudioMessage(); audio != nil {
		kind := appstore.MediaKindAudio
		if audio.GetPTT() {
			kind = appstore.MediaKindVoice
		}
		return "", kind, defaultMime(audio.GetMimetype(), "audio/ogg; codecs=opus")
	}
	if document := message.GetDocumentMessage(); document != nil {
		// A quoted document with no caption shows its filename, matching the
		// chat-list preview.
		text := document.GetCaption()
		if strings.TrimSpace(text) == "" {
			text = document.GetFileName()
		}
		return text, appstore.MediaKindDocument, defaultMime(document.GetMimetype(), "application/octet-stream")
	}
	if location := message.GetLocationMessage(); location != nil {
		payload := locationPayloadFromMessage(location)
		kind := appstore.MediaKindLocation
		if payload.Live {
			kind = appstore.MediaKindLiveLocation
		}
		// A quoted location shows where it is, not the word "Location": that is
		// the whole content of the message.
		return locationSummary(payload), kind, "image/png"
	}
	if live := message.GetLiveLocationMessage(); live != nil {
		return formatCoordinates(live.GetDegreesLatitude(), live.GetDegreesLongitude()),
			appstore.MediaKindLiveLocation, "image/png"
	}
	if contact := message.GetContactMessage(); contact != nil {
		return cardFromContactMessage(contact).DisplayName, appstore.MediaKindContact, ""
	}
	if contacts := message.GetContactsArrayMessage(); contacts != nil {
		return strings.TrimSpace(contacts.GetDisplayName()), appstore.MediaKindContacts, ""
	}
	if poll := pollCreationFromMessage(message); poll != nil {
		return strings.TrimSpace(poll.GetName()), appstore.MediaKindPoll, ""
	}
	if invite := message.GetGroupInviteMessage(); invite != nil {
		return groupInviteSummary(invite), appstore.MediaKindGroupInvite, ""
	}
	if event := message.GetEventMessage(); event != nil {
		return eventSummary(event), appstore.MediaKindEvent, ""
	}
	if album := message.GetAlbumMessage(); album != nil {
		return albumSummary(&appstore.AlbumPayload{
			ExpectedImages: int(album.GetExpectedImageCount()),
			ExpectedVideos: int(album.GetExpectedVideoCount()),
		}), appstore.MediaKindAlbum, ""
	}
	if payload, _, ok := interactivePayloadFromMessage(message); ok && payload.HasContent() {
		return interactiveSummary(payload), appstore.MediaKindInteractive, ""
	}
	if payload, _, _, ok := commercePayloadFromMessage(message); ok {
		return commerceSummary(payload), commerceMediaKind(payload.Kind), ""
	}
	if pack := message.GetStickerPackMessage(); pack != nil {
		return strings.TrimSpace(pack.GetName()), appstore.MediaKindStickerPack, ""
	}
	if log := message.GetCallLogMesssage(); log != nil {
		// A quote of a call log cannot know which side it was on, so it takes
		// the incoming reading: "Missed voice call" is the one somebody would
		// be quoting.
		return callLogSummary(&appstore.CallLogPayload{
			Video:        log.GetIsVideo(),
			Outcome:      callOutcomeName(log.GetCallOutcome()),
			DurationSecs: log.GetDurationSecs(),
			Participants: len(log.GetParticipants()),
		}, false), appstore.MediaKindCallLog, ""
	}
	return "", "", ""
}

func defaultMime(mimeType, fallback string) string {
	if mimeType == "" {
		return fallback
	}
	return mimeType
}

func messageDirectionAndStatus(info types.MessageInfo, opts ingestOptions) (string, string) {
	if info.IsFromMe {
		status := appstore.StatusSent
		if opts.source == sourceHistorySync && opts.historyStatus != "" {
			status = opts.historyStatus
		}
		return appstore.DirectionOutgoing, status
	}
	return appstore.DirectionIncoming, appstore.StatusDelivered
}

// futureTimestampSlack is how far ahead a message may legitimately claim to be.
// Clocks disagree by seconds, not days. Anything beyond this is a broken sender
// clock, and left alone it pins the message to the top of the chat forever,
// because nothing that arrives later can ever sort above it.
const futureTimestampSlack = 12 * time.Hour

func rawMessageTimestamp(info types.MessageInfo, opts ingestOptions, webMsg *waWeb.WebMessageInfo) time.Time {
	if !opts.timestampOverride.IsZero() {
		return opts.timestampOverride
	}
	if opts.source == sourceHistorySync && info.IsFromMe && webMsg != nil {
		if timestamp, ok := whatsAppUnixTimestamp(webMsg.GetMessageC2STimestamp()); ok {
			return timestamp
		}
	}
	return info.Timestamp
}

func clampFutureTimestamp(timestamp, now time.Time) time.Time {
	if timestamp.IsZero() || !timestamp.After(now.Add(futureTimestampSlack)) {
		return timestamp
	}
	return now
}

func whatsAppUnixTimestamp(value uint64) (time.Time, bool) {
	const maxReasonableUnixSeconds = 4102444800 // 2100-01-01
	if value == 0 {
		return time.Time{}, false
	}
	if value <= maxReasonableUnixSeconds {
		return time.Unix(int64(value), 0), true
	}
	if value <= maxReasonableUnixSeconds*1000 {
		return time.UnixMilli(int64(value)), true
	}
	return time.Time{}, false
}

func textFromMessage(message *waE2E.Message) string {
	if text := message.GetConversation(); text != "" {
		return text
	}
	if text := message.GetExtendedTextMessage().GetText(); text != "" {
		return text
	}
	// Pressing a button on a business message sends back a message whose whole
	// content is the words that were on the button, which is exactly what
	// WhatsApp shows for one. So it is text, and every path that handles text
	// (search, quoting, copying, the chat-list preview) handles it for free.
	if text := interactiveResponseText(message); text != "" {
		return text
	}
	return ""
}

func senderID(info types.MessageInfo) string {
	if info.IsFromMe {
		return "me"
	}
	if !info.Sender.IsEmpty() {
		return bareAvatarJID(info.Sender).String()
	}
	return bareAvatarJID(info.Chat).String()
}

func senderJID(info types.MessageInfo) types.JID {
	if info.IsFromMe {
		return types.JID{}
	}
	if !info.Sender.IsEmpty() {
		return bareAvatarJID(info.Sender)
	}
	return bareAvatarJID(info.Chat)
}

// mapWebMessageStatus translates the WhatsApp WebMessageInfo.Status field
// into our internal status string, used for own-message rows during history
// sync. Returns "" when the field is absent.
func mapWebMessageStatus(webMsg *waWeb.WebMessageInfo) string {
	if webMsg == nil || webMsg.Status == nil {
		return ""
	}
	switch webMsg.GetStatus() {
	case waWeb.WebMessageInfo_PENDING:
		return appstore.StatusPending
	case waWeb.WebMessageInfo_SERVER_ACK:
		return appstore.StatusSent
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return appstore.StatusDelivered
	case waWeb.WebMessageInfo_READ, waWeb.WebMessageInfo_PLAYED:
		return appstore.StatusRead
	case waWeb.WebMessageInfo_ERROR:
		return appstore.StatusFailed
	default:
		return ""
	}
}

func internalMessageIDForChat(chatID string, messageID types.MessageID) string {
	return fmt.Sprintf("%s:%s", chatID, messageID)
}

func bareAvatarJID(jid types.JID) types.JID {
	return jid.ToNonAD()
}

func safeMediaFileName(messageID, extension string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	name := replacer.Replace(strings.TrimSpace(messageID))
	if name == "" {
		name = "media"
	}
	if extension == "" {
		extension = ".bin"
	}
	return fmt.Sprintf("%s%s", name, extension)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file into place: %w", err)
	}
	return nil
}
