package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/model"
	appstore "whatevrd/internal/store"
)

// Names is what the decoder asks about people while it builds a row.
type Names interface {
	// Norm turns a lid into its phone number when one is known.
	Norm(types.JID) types.JID
	Name(types.JID) string
	Own(types.JID) bool
}

// Decoder turns a message into the row a frontend shows. it needs no
// connection: everything it knows comes from the message and Names.
type Decoder struct {
	names    Names
	mediaDir string
}

func NewDecoder(names Names, mediaDir string) *Decoder {
	return &Decoder{names: names, mediaDir: mediaDir}
}

// Decode is the row for a live message.
func (d *Decoder) Decode(ctx context.Context, evt *events.Message) (appstore.Message, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.Message{}, false
	}
	evt.Message = unwrapNestedMessage(evt.Message)
	return d.decode(ctx, evt, ingestOptions{source: sourceLive})
}

// DecodeHistory is Decode for a message out of a history blob, stubs
// included.
func (d *Decoder) DecodeHistory(ctx context.Context, chat types.JID, web *waWeb.WebMessageInfo) (appstore.Message, bool) {
	if payload, ts, ok := d.historyStubSystemPayload(ctx, web); ok {
		return systemMessage(chat, payload, ts), true
	}
	if web.GetMessage() == nil {
		return appstore.Message{}, false
	}
	evt, err := parseWebMessage(chat, web)
	if err != nil || evt.Message == nil {
		return appstore.Message{}, false
	}
	evt.Message = unwrapNestedMessage(evt.Message)
	return d.decode(ctx, evt, ingestOptions{source: sourceHistorySync, historyStatus: mapWebMessageStatus(web)})
}

// DecodeContent is the content of an edit decoded against the message it
// edits: the text or caption it now says.
func (d *Decoder) DecodeContent(ctx context.Context, evt *events.Message, content *waE2E.Message) (appstore.Message, bool) {
	cp := *evt
	cp.Message = unwrapNestedMessage(content)
	return d.decode(ctx, &cp, ingestOptions{source: sourceLive})
}

func (d *Decoder) decode(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.Message, bool) {
	if in, ok := d.textMessageInput(ctx, evt, opts); ok {
		return messageFromInput(appstore.MediaMessageInput{TextMessageInput: in}), true
	}
	if in, ok := d.mediaMessageInput(ctx, evt, opts); ok {
		return messageFromInput(in), true
	}
	return appstore.Message{}, false
}

// parseWebMessage is whatsmeow's ParseWebMessage without the client: a
// message from us gets no sender, the row says "me" for it anyway.
func parseWebMessage(chat types.JID, web *waWeb.WebMessageInfo) (*events.Message, error) {
	var err error
	if chat.IsEmpty() {
		if chat, err = types.ParseJID(web.GetKey().GetRemoteJID()); err != nil {
			return nil, err
		}
	}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chat,
			IsFromMe: web.GetKey().GetFromMe(),
			IsGroup:  chat.Server == types.GroupServer,
		},
		ID:        web.GetKey().GetID(),
		PushName:  web.GetPushName(),
		Timestamp: time.Unix(int64(web.GetMessageTimestamp()), 0),
	}
	switch {
	case info.IsFromMe:
		if s := web.GetOriginalSelfAuthorUserJIDString(); s != "" {
			info.Sender, err = types.ParseJID(s)
		}
	case chat.Server == types.DefaultUserServer || chat.Server == types.HiddenUserServer || chat.Server == types.NewsletterServer:
		info.Sender = chat
	case web.GetParticipant() != "":
		info.Sender, err = types.ParseJID(web.GetParticipant())
	case web.GetKey().GetParticipant() != "":
		info.Sender, err = types.ParseJID(web.GetKey().GetParticipant())
	default:
		return nil, fmt.Errorf("no sender for message %s", info.ID)
	}
	if err != nil {
		return nil, err
	}
	if pk := web.GetCommentMetadata().GetCommentParentKey(); pk != nil {
		info.MsgMetaInfo.ThreadMessageID = pk.GetID()
		info.MsgMetaInfo.ThreadMessageSenderJID, _ = types.ParseJID(pk.GetParticipant())
	}
	evt := (&events.Message{RawMessage: web.GetMessage(), SourceWebMsg: web, Info: info}).UnwrapRaw()
	if pm := evt.Message.GetProtocolMessage(); pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		evt.Info.ID = pm.GetKey().GetID()
		evt.Message = pm.GetEditedMessage()
	}
	return evt, nil
}

func messageTimestamp(info types.MessageInfo, opts ingestOptions, webMsg *waWeb.WebMessageInfo) time.Time {
	return clampFutureTimestamp(rawMessageTimestamp(info, opts, webMsg), time.Now())
}

func (d *Decoder) saveMessageThumbnail(chatID, messageID string, thumbnail []byte) string {
	return d.saveMessageThumbnailWithExtension(chatID, messageID, thumbnail, ".thumb.jpg")
}

// saveMessageThumbnailWithExtension writes a thumbnail once; the message
// carrying it never changes, so a file already there is the same bytes.
func (d *Decoder) saveMessageThumbnailWithExtension(chatID, messageID string, thumbnail []byte, extension string) string {
	if len(thumbnail) == 0 || d.mediaDir == "" {
		return ""
	}
	dir := filepath.Join(d.mediaDir, "messages", chatID)
	path := filepath.Join(dir, safeMediaFileName(messageID, extension))
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	if err := writeFileAtomic(path, thumbnail, 0o600); err != nil {
		return ""
	}
	return path
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

// worldNames is Names over the model's identity.
type worldNames struct{ w *model.World }

// WorldNames is Names as the model's world knows them.
func WorldNames(w *model.World) Names { return worldNames{w} }

func (n worldNames) Norm(j types.JID) types.JID {
	if j.Server != types.HiddenUserServer {
		return j
	}
	if pn := n.w.PN(n.w.Now(j.ToNonAD().String())); pn != "" {
		return parseJID(pn)
	}
	return j
}

// Name falls back to the bare user of anything but a lid, a bot's number say.
func (n worldNames) Name(j types.JID) string {
	if name, _ := n.w.Name(n.w.Now(j.ToNonAD().String())); name != "" || j.Server == types.HiddenUserServer {
		return name
	}
	return j.User
}

func (n worldNames) Own(j types.JID) bool { return !j.IsEmpty() && n.w.IsSelf(j.ToNonAD().String()) }

func parseJID(s string) types.JID {
	j, _ := types.ParseJID(s)
	return j
}

// Model is the row for a message the model has, and the content it came
// from. false is a message the decoder has nothing for.
func (d *Decoder) Model(ctx context.Context, w *model.World, m model.Message) (appstore.Message, *waE2E.Message, bool) {
	raw, hm := m.Content()
	switch {
	case hm != nil:
		sm, ok := d.DecodeHistory(ctx, parseJID(m.Chat), hm.GetMessage())
		return sm, hm.GetMessage().GetMessage(), ok
	case raw != nil:
		sm, ok := d.Decode(ctx, ModelEvent(w, m, raw))
		return sm, raw, ok
	}
	return appstore.Message{}, nil, false
}

// ModelEvent is the whatsmeow event a live message was, rebuilt.
func ModelEvent(w *model.World, m model.Message, raw *waE2E.Message) *events.Message {
	sender := parseJID(m.Sender)
	if m.FromMe {
		sender = parseJID(w.SelfPN())
	}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat: parseJID(m.Chat), Sender: sender, SenderAlt: parseJID(m.SenderAlt),
			IsFromMe: m.FromMe, IsGroup: model.IsGroup(m.Chat),
		},
		ID:        m.ID,
		Timestamp: time.UnixMilli(m.T),
	}
	evt := &events.Message{Info: info, RawMessage: proto.Clone(raw).(*waE2E.Message)}
	return evt.UnwrapRaw()
}
