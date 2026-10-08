// Package commands serves protocol 2's request arms: each one is its ids
// taken back to the core's keys, a call on the whatsapp client, and the
// client's error put on the wire.
package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/frontends"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
	"whatevrd/internal/views"
	"whatevrd/internal/whatsapp"
)

type Options struct {
	Server *server.Server
	Client *whatsapp.Client
	Reads  *views.Reads
	Log    zerolog.Logger
	// Frontends is where frontend manifests are read from
	Frontends frontends.Dirs
	// Launch lets a click or link start the default frontend
	Launch bool
	// Hint tells the user why nothing opened, nil to only log it
	Hint func(title, body string)
}

type commands struct {
	srv    *server.Server
	c      *whatsapp.Client
	rs     *views.Reads
	log    zerolog.Logger
	dirs   frontends.Dirs
	launch bool
	hint   func(title, body string)
	opener *Opener
}

// Register serves every command on o.Server and has it tell the client
// what the frontends show. the opener is for clicks from outside the socket.
func Register(o Options) *Opener {
	x := &commands{srv: o.Server, c: o.Client, rs: o.Reads, log: o.Log, dirs: o.Frontends, launch: o.Launch, hint: o.Hint}
	x.opener = &Opener{x: x}
	type arm = protoreflect.FieldNumber
	off := map[arm]server.Method{
		arm(v2.Request_DaemonReconnect_case):          x.reconnect,
		arm(v2.Request_AccountLogout_case):            x.logout,
		arm(v2.Request_ChatMarkRead_case):             x.markRead,
		arm(v2.Request_ChatMarkAllRead_case):          x.markAllRead,
		arm(v2.Request_ChatExport_case):               x.exportChat,
		arm(v2.Request_ChatPin_case):                  x.chatPin,
		arm(v2.Request_ChatFavorite_case):             x.chatFavorite,
		arm(v2.Request_ChatArchive_case):              x.chatArchive,
		arm(v2.Request_ChatMute_case):                 x.chatMute,
		arm(v2.Request_ChatTyping_case):               x.typing,
		arm(v2.Request_ChatRequestOlder_case):         x.requestOlder,
		arm(v2.Request_ChatEnsureDirect_case):         x.ensureDirect,
		arm(v2.Request_MessageReact_case):             x.react,
		arm(v2.Request_MessageEdit_case):              x.edit,
		arm(v2.Request_MessageRevoke_case):            x.revoke,
		arm(v2.Request_MessageDelete_case):            x.deleteForMe,
		arm(v2.Request_MessageStar_case):              x.star,
		arm(v2.Request_MessagePin_case):               x.pin,
		arm(v2.Request_MessageForward_case):           x.forward,
		arm(v2.Request_MessageMarkPlayed_case):        x.markPlayed,
		arm(v2.Request_MessageRequestFromPhone_case):  x.requestFromPhone,
		arm(v2.Request_PollVote_case):                 x.vote,
		arm(v2.Request_EventRsvp_case):                x.rsvp,
		arm(v2.Request_GroupJoinInvite_case):          x.joinInvite,
		arm(v2.Request_MediaDownload_case):            x.download,
		arm(v2.Request_MediaStream_case):              x.stream,
		arm(v2.Request_MediaCancelDownload_case):      x.cancelDownload,
		arm(v2.Request_MediaRead_case):                x.read,
		arm(v2.Request_MediaFetchProfilePicture_case): x.profilePicture,
		arm(v2.Request_MediaSave_case):                x.saveMedia,
		arm(v2.Request_LogMessage_case):               x.logMessage,
		arm(v2.Request_PrivacySet_case):               x.privacy,
		arm(v2.Request_PrivacySetDefaultTimer_case):   x.defaultTimer,
		arm(v2.Request_SelfSetAbout_case):             x.about,
		arm(v2.Request_ContactBlock_case):             x.block,
		arm(v2.Request_StickerFavorite_case):          x.stickerFavorite,
		arm(v2.Request_StickerDownload_case):          x.stickerDownload,
		arm(v2.Request_StickerPackInstall_case):       x.packInstall,
		arm(v2.Request_StickerPacksRefresh_case):      x.packsRefresh,
		arm(v2.Request_ContactCheckPhone_case):        x.checkPhone,
		arm(v2.Request_FrontendList_case):             x.frontendList,
		arm(v2.Request_FrontendSetDefault_case):       x.frontendSetDefault,
		arm(v2.Request_LinkOpen_case):                 x.linkOpen,
	}
	for n, fn := range off {
		o.Server.Handle(n, fn)
	}
	// in the order sent: two sends from one frontend land in that order
	on := map[arm]server.Method{
		arm(v2.Request_SessionUpdate_case):       x.session,
		arm(v2.Request_SendText_case):            x.sendText,
		arm(v2.Request_SendMedia_case):           x.sendMedia,
		arm(v2.Request_SendSticker_case):         x.sendSticker,
		arm(v2.Request_SendPoll_case):            x.sendPoll,
		arm(v2.Request_SendContact_case):         x.sendContact,
		arm(v2.Request_SendLocation_case):        x.sendLocation,
		arm(v2.Request_SendCancel_case):          x.sendCancel,
		arm(v2.Request_ScheduleText_case):        x.scheduleText,
		arm(v2.Request_ScheduleList_case):        x.scheduleList,
		arm(v2.Request_ScheduleCancel_case):      x.scheduleCancel,
		arm(v2.Request_PreferencesSet_case):      x.prefs,
		arm(v2.Request_NotificationDismiss_case): x.dismiss,
	}
	for n, fn := range on {
		o.Server.HandleInline(n, fn)
	}
	o.Server.OnSessions = x.sessions
	return x.opener
}

// wire is err as a request's failure.
func wire(err error) error {
	if err == nil {
		return nil
	}
	var se *server.Error
	if errors.As(err, &se) {
		return err
	}
	kinds := []struct {
		kind error
		code v2.ErrorCode
	}{
		{whatsapp.ErrNotLoggedIn, v2.ErrorCode_ERROR_CODE_NOT_LOGGED_IN},
		{whatsapp.ErrNotConnected, v2.ErrorCode_ERROR_CODE_NOT_CONNECTED},
		{whatsapp.ErrNotFound, v2.ErrorCode_ERROR_CODE_NOT_FOUND},
		{whatsapp.ErrInvalid, v2.ErrorCode_ERROR_CODE_INVALID_PARAMS},
		{whatsapp.ErrRejected, v2.ErrorCode_ERROR_CODE_REJECTED},
		{whatsapp.ErrExpired, v2.ErrorCode_ERROR_CODE_EXPIRED},
		{whatsapp.ErrGuarded, v2.ErrorCode_ERROR_CODE_GUARDED},
		{whatsapp.ErrIO, v2.ErrorCode_ERROR_CODE_IO},
	}
	for _, k := range kinds {
		if errors.Is(err, k.kind) {
			return server.Errorf(k.code, "%s", strings.TrimPrefix(err.Error(), k.kind.Error()+": "))
		}
	}
	return err
}

func invalid(format string, args ...any) error {
	return server.Errorf(v2.ErrorCode_ERROR_CODE_INVALID_PARAMS, format, args...)
}

func notFound(format string, args ...any) error {
	return server.Errorf(v2.ErrorCode_ERROR_CODE_NOT_FOUND, format, args...)
}

// chat is the key a chat id stands for.
func (x *commands) chat(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", invalid("chat_id is required")
	}
	w, err := x.rs.World(ctx)
	if err != nil {
		return "", err
	}
	key, ok := x.rs.IDs().Key(w, id)
	if !ok {
		return "", notFound("no chat %q", id)
	}
	return key, nil
}

// person is the key an address names.
func (x *commands) person(ctx context.Context, a *v2.Address) (string, error) {
	if a == nil || a.WhichAddress() == v2.Address_Address_not_set_case {
		return "", invalid("person is required")
	}
	key, err := x.rs.Address(ctx, a)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", notFound("no one by that address")
	}
	return key, nil
}

// id is the id rows show key under, given one now if it had none.
func (x *commands) id(ctx context.Context, key string) (string, error) {
	w, err := x.rs.World(ctx)
	if err != nil {
		return "", err
	}
	if err := x.rs.IDs().Ensure(ctx, w, key); err != nil {
		return "", err
	}
	return x.rs.IDs().Of(w, key), nil
}

func message(tok string) (whatsapp.Ref, error) {
	addr, id, ok := views.SplitToken(tok)
	if !ok {
		return whatsapp.Ref{}, invalid("message id %q", tok)
	}
	return whatsapp.Ref{Chat: addr, ID: id}, nil
}

func token(r whatsapp.Ref) string { return views.MessageToken(r.Chat, r.ID) }

func duration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

func (x *commands) reconnect(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	x.c.Reconnect()
	return nil, nil
}

// logMessage lands a frontend's own diagnostics in the run log the logs
// view tails, so the Logs tab shows both halves of the story.
func (x *commands) logMessage(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	x.log.Info().Msg(req.GetLogMessage().GetMessage())
	return nil, nil
}

func (x *commands) logout(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return nil, wire(x.c.Logout(ctx))
}

func (x *commands) markRead(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetChatMarkRead()
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	ref, err := message(p.GetUpToMessageId())
	if err != nil {
		return nil, err
	}
	return nil, wire(x.c.MarkRead(ctx, key, ref))
}

func (x *commands) markAllRead(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	n, err := x.c.MarkAllRead(ctx)
	if err != nil {
		return nil, wire(err)
	}
	res := &v2.Response{}
	res.SetChatMarkAllRead(v2.ChatMarkAllReadResult_builder{Count: int32(n)}.Build())
	return res, nil
}

func (x *commands) exportChat(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetChatExport()
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	path, err := x.c.ExportChat(ctx, key, p.GetPath())
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetChatExport(v2.ChatExportResult_builder{Path: path}.Build())
	return resp, nil
}

func (x *commands) chatPin(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetChatPin()
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	return nil, wire(x.c.PinChat(ctx, key, p.GetPinned()))
}

func (x *commands) chatFavorite(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetChatFavorite()
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	return nil, wire(x.c.SetChatFavorite(ctx, key, p.GetFavorite()))
}

func (x *commands) chatArchive(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetChatArchive()
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	return nil, wire(x.c.ArchiveChat(ctx, key, p.GetArchived()))
}

func (x *commands) chatMute(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetChatMute()
	if p.GetDurationMs() < 0 {
		return nil, invalid("duration_ms can't be negative")
	}
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	return nil, wire(x.c.MuteChat(ctx, key, p.GetMuted(), duration(p.GetDurationMs())))
}

func (x *commands) typing(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetChatTyping()
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	return nil, wire(x.c.SetTyping(ctx, key, p.GetComposing() || p.GetRecording(), p.GetRecording()))
}

func (x *commands) requestOlder(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	key, err := x.chat(ctx, req.GetChatRequestOlder().GetChatId())
	if err != nil {
		return nil, err
	}
	ok, err := x.c.RequestOlder(ctx, key)
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetChatRequestOlder(v2.ChatRequestOlderResult_builder{Requested: ok}.Build())
	return resp, nil
}

func (x *commands) ensureDirect(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	key, err := x.person(ctx, req.GetChatEnsureDirect().GetPerson())
	if err != nil {
		return nil, err
	}
	if model.IsGroup(key) {
		return nil, invalid("that is a group, not a person")
	}
	id, err := x.id(ctx, key)
	if err != nil {
		return nil, err
	}
	resp := &v2.Response{}
	resp.SetChatEnsureDirect(v2.ChatEnsureDirectResult_builder{ChatId: id}.Build())
	return resp, nil
}

// draft is a send's chat, reply and mentions in the client's terms.
func (x *commands) draft(ctx context.Context, chatID, replyTo string, mentions []*v2.Address) (whatsapp.Draft, error) {
	key, err := x.chat(ctx, chatID)
	if err != nil {
		return whatsapp.Draft{}, err
	}
	d := whatsapp.Draft{Chat: key}
	if replyTo != "" {
		r, err := message(replyTo)
		if err != nil {
			return d, err
		}
		d.ReplyTo = &r
	}
	for _, a := range mentions {
		k, err := x.person(ctx, a)
		if err != nil {
			return d, err
		}
		d.Mentions = append(d.Mentions, k)
	}
	return d, nil
}

// once is a send's key and a digest of the rest of p
func once(p interface {
	proto.Message
	GetKey() string
}) whatsapp.Once {
	if p.GetKey() == "" {
		return whatsapp.Once{}
	}
	rest := proto.Clone(p)
	r := rest.ProtoReflect()
	r.Clear(r.Descriptor().Fields().ByName("key"))
	b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(rest)
	sum := sha256.Sum256(b)
	return whatsapp.Once{Key: p.GetKey(), Params: hex.EncodeToString(sum[:16])}
}

func sent(r whatsapp.Ref, err error) (*v2.Response, error) {
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetSend(v2.SendResult_builder{MessageId: token(r)}.Build())
	return resp, nil
}

func (x *commands) sendText(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetSendText()
	if strings.TrimSpace(p.GetText()) == "" {
		return nil, invalid("text is empty")
	}
	d, err := x.draft(ctx, p.GetChatId(), p.GetReplyTo(), p.GetMentions())
	if err != nil {
		return nil, err
	}
	d.Once = once(p)
	return sent(x.c.SendText(ctx, d, p.GetText()))
}

func (x *commands) sendMedia(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetSendMedia()
	if p.GetPath() == "" {
		return nil, invalid("path is required")
	}
	d, err := x.draft(ctx, p.GetChatId(), p.GetReplyTo(), p.GetMentions())
	if err != nil {
		return nil, err
	}
	d.Once = once(p)
	return sent(x.c.SendMedia(ctx, d, p.GetPath(), p.GetCaption(), p.GetAsDocument(), p.GetViewOnce()))
}

func (x *commands) sendSticker(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetSendSticker()
	if p.GetStickerId() == "" {
		return nil, invalid("sticker_id is required")
	}
	d, err := x.draft(ctx, p.GetChatId(), p.GetReplyTo(), nil)
	if err != nil {
		return nil, err
	}
	d.Once = once(p)
	return sent(x.c.SendSticker(ctx, d, p.GetStickerId()))
}

func (x *commands) sendPoll(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetSendPoll()
	d, err := x.draft(ctx, p.GetChatId(), p.GetReplyTo(), nil)
	if err != nil {
		return nil, err
	}
	d.Once = once(p)
	return sent(x.c.SendPoll(ctx, d, p.GetQuestion(), p.GetOptions(), p.GetMulti()))
}

func (x *commands) sendContact(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetSendContact()
	d, err := x.draft(ctx, p.GetChatId(), p.GetReplyTo(), nil)
	if err != nil {
		return nil, err
	}
	d.Once = once(p)
	return sent(x.c.SendContact(ctx, d, p.GetName(), p.GetPhone()))
}

func (x *commands) sendLocation(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetSendLocation()
	d, err := x.draft(ctx, p.GetChatId(), p.GetReplyTo(), nil)
	if err != nil {
		return nil, err
	}
	d.Once = once(p)
	return sent(x.c.SendLocation(ctx, d, p.GetLat(), p.GetLng(), p.GetName(), p.GetAddress()))
}

func (x *commands) scheduleText(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetScheduleText()
	key, err := x.chat(ctx, p.GetChatId())
	if err != nil {
		return nil, err
	}
	id, err := x.c.ScheduleText(ctx, key, p.GetText(), time.Unix(p.GetSendAt(), 0))
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetScheduleText(v2.ScheduleTextResult_builder{ScheduledId: id}.Build())
	return resp, nil
}

func (x *commands) scheduleList(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	chat := strings.TrimSpace(req.GetScheduleList().GetChatId())
	key := ""
	if chat != "" {
		var err error
		if key, err = x.chat(ctx, chat); err != nil {
			return nil, err
		}
	}
	rows, err := x.c.Scheduled(ctx, key)
	if err != nil {
		return nil, wire(err)
	}
	res := v2.ScheduleListResult_builder{}.Build()
	for _, sc := range rows {
		id, err := x.id(ctx, sc.Chat)
		if err != nil {
			return nil, err
		}
		m := v2.ScheduledMessage_builder{Id: sc.ID, ChatId: id, Text: sc.Text, SendAt: sc.SendAt}.Build()
		res.SetMessages(append(res.GetMessages(), m))
	}
	resp := &v2.Response{}
	resp.SetScheduleList(res)
	return resp, nil
}

func (x *commands) scheduleCancel(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	if req.GetScheduleCancel().GetId() <= 0 {
		return nil, invalid("id is required")
	}
	return nil, wire(x.c.CancelScheduled(ctx, req.GetScheduleCancel().GetId()))
}

func (x *commands) sendCancel(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	ref, err := message(req.GetSendCancel().GetMessageId())
	if err != nil {
		return nil, err
	}
	return nil, wire(x.c.Cancel(ctx, ref))
}

// onMessage is a request that names one message and answers done.
func onMessage(tok string, fn func(whatsapp.Ref) error) (*v2.Response, error) {
	r, err := message(tok)
	if err != nil {
		return nil, err
	}
	return nil, wire(fn(r))
}

func (x *commands) react(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetMessageReact()
	return onMessage(p.GetMessageId(), func(r whatsapp.Ref) error { return x.c.React(ctx, r, p.GetEmoji()) })
}

func (x *commands) edit(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetMessageEdit()
	if strings.TrimSpace(p.GetText()) == "" {
		return nil, invalid("text is empty")
	}
	return onMessage(p.GetMessageId(), func(r whatsapp.Ref) error { return x.c.Edit(ctx, r, p.GetText()) })
}

func (x *commands) revoke(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return onMessage(req.GetMessageRevoke().GetMessageId(), func(r whatsapp.Ref) error { return x.c.Revoke(ctx, r) })
}

func (x *commands) deleteForMe(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return onMessage(req.GetMessageDelete().GetMessageId(), func(r whatsapp.Ref) error { return x.c.DeleteForMe(ctx, r) })
}

func (x *commands) star(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetMessageStar()
	return onMessage(p.GetMessageId(), func(r whatsapp.Ref) error { return x.c.Star(ctx, r, p.GetStarred()) })
}

func (x *commands) pin(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetMessagePin()
	if p.GetDurationMs() < 0 {
		return nil, invalid("duration_ms can't be negative")
	}
	return onMessage(p.GetMessageId(), func(r whatsapp.Ref) error {
		return x.c.Pin(ctx, r, p.GetPinned(), duration(p.GetDurationMs()))
	})
}

func (x *commands) forward(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetMessageForward()
	r, err := message(p.GetMessageId())
	if err != nil {
		return nil, err
	}
	if len(p.GetChatIds()) == 0 {
		return nil, invalid("chat_ids is empty")
	}
	keys := make([]string, len(p.GetChatIds()))
	for i, id := range p.GetChatIds() {
		if keys[i], err = x.chat(ctx, id); err != nil {
			return nil, err
		}
	}
	refs, err := x.c.Forward(ctx, r, keys, once(p))
	if err != nil {
		return nil, wire(err)
	}
	ids := make([]string, len(refs))
	for i, r := range refs {
		ids[i] = token(r)
	}
	resp := &v2.Response{}
	resp.SetMessageForward(v2.MessageForwardResult_builder{MessageIds: ids}.Build())
	return resp, nil
}

func (x *commands) markPlayed(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return onMessage(req.GetMessageMarkPlayed().GetMessageId(), func(r whatsapp.Ref) error { return x.c.MarkPlayed(ctx, r) })
}

func (x *commands) requestFromPhone(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return onMessage(req.GetMessageRequestFromPhone().GetMessageId(), func(r whatsapp.Ref) error {
		return x.c.RequestFromPhone(ctx, r)
	})
}

func (x *commands) vote(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetPollVote()
	return onMessage(p.GetMessageId(), func(r whatsapp.Ref) error { return x.c.Vote(ctx, r, p.GetOptionIndexes()) })
}

var rsvps = map[v2.Rsvp]waE2E.EventResponseMessage_EventResponseType{
	v2.Rsvp_RSVP_GOING:     waE2E.EventResponseMessage_GOING,
	v2.Rsvp_RSVP_NOT_GOING: waE2E.EventResponseMessage_NOT_GOING,
	v2.Rsvp_RSVP_MAYBE:     waE2E.EventResponseMessage_MAYBE,
}

func (x *commands) rsvp(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetEventRsvp()
	answer, ok := rsvps[p.GetResponse()]
	if !ok {
		return nil, invalid("response must be going, not going or maybe")
	}
	return onMessage(p.GetMessageId(), func(r whatsapp.Ref) error {
		return x.c.Rsvp(ctx, r, answer, p.GetExtraGuests())
	})
}

func (x *commands) joinInvite(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	r, err := message(req.GetGroupJoinInvite().GetMessageId())
	if err != nil {
		return nil, err
	}
	grp, err := x.c.JoinInvite(ctx, r)
	if err != nil {
		return nil, wire(err)
	}
	w, err := x.rs.World(ctx)
	if err != nil {
		return nil, err
	}
	id, err := x.id(ctx, w.Now(model.Norm(grp)))
	if err != nil {
		return nil, err
	}
	resp := &v2.Response{}
	resp.SetGroupJoinInvite(v2.GroupJoinInviteResult_builder{ChatId: id}.Build())
	return resp, nil
}

func (x *commands) download(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return onMessage(req.GetMediaDownload().GetMessageId(), func(r whatsapp.Ref) error { return x.c.Download(ctx, r) })
}

func (x *commands) cancelDownload(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return onMessage(req.GetMediaCancelDownload().GetMessageId(), func(r whatsapp.Ref) error {
		return x.c.CancelDownload(ctx, r)
	})
}

func (x *commands) saveMedia(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetMediaSave()
	if strings.TrimSpace(p.GetStatusId()) != "" {
		return nil, invalid("status saves need the status views")
	}
	var ref whatsapp.Ref
	if tok := strings.TrimSpace(p.GetMessageId()); tok != "" {
		var err error
		if ref, err = message(tok); err != nil {
			return nil, err
		}
	}
	var avatar string
	if jid := strings.TrimSpace(p.GetJid()); jid != "" {
		w, err := x.rs.World(ctx)
		if err != nil {
			return nil, err
		}
		key := w.Now(model.Norm(jid))
		if key == "" {
			return nil, invalid("jid is required")
		}
		avatar = key
	}
	path, err := x.c.SaveMedia(ctx, ref, avatar, p.GetPath())
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetMediaSave(v2.MediaSaveResult_builder{Path: path}.Build())
	return resp, nil
}

func (x *commands) stream(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	tok := req.GetMediaStream().GetMessageId()
	r, err := message(tok)
	if err != nil {
		return nil, err
	}
	// a late failure goes to the frontend that asked, as an event
	st, err := x.c.StreamMedia(ctx, r, func(u whatsapp.StreamUpdate) {
		b := v2.MediaStreamUpdate_builder{StreamId: u.StreamID, MessageId: token(u.Ref), Path: u.Path}
		if u.Path != "" {
			b.State = v2.MediaStreamState_MEDIA_STREAM_STATE_LOCAL
		} else {
			b.State = v2.MediaStreamState_MEDIA_STREAM_STATE_FAILED
		}
		if u.Err != nil {
			b.Error = u.Err.Error()
		}
		e := &v2.Event{}
		e.SetMediaStreamUpdate(b.Build())
		s.Send(e)
	})
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetMediaStream(v2.MediaStreamResult_builder{StreamId: st.ID, Url: st.URL, Mime: st.Mime, SizeBytes: st.Size,
		DurationMs: st.Duration.Milliseconds()}.Build())
	return resp, nil
}

func (x *commands) read(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetMediaRead()
	if p.GetPath() == "" {
		return nil, invalid("path is required")
	}
	data, size, eof, err := x.c.ReadFile(p.GetPath(), p.GetOffset(), p.GetMaxBytes())
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetMediaRead(v2.MediaReadResult_builder{Data: data, SizeBytes: size, Eof: eof}.Build())
	return resp, nil
}

func (x *commands) profilePicture(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	key, err := x.person(ctx, req.GetMediaFetchProfilePicture().GetPerson())
	if err != nil {
		return nil, err
	}
	path, err := x.c.FetchProfilePicture(ctx, key)
	if err != nil {
		return nil, wire(err)
	}
	resp := &v2.Response{}
	resp.SetMediaFetchProfilePicture(v2.MediaFetchProfilePictureResult_builder{Path: path}.Build())
	return resp, nil
}

var privacyNames = map[v2.PrivacyCategory]types.PrivacySettingType{
	v2.PrivacyCategory_PRIVACY_CATEGORY_LAST_SEEN:     types.PrivacySettingTypeLastSeen,
	v2.PrivacyCategory_PRIVACY_CATEGORY_ONLINE:        types.PrivacySettingTypeOnline,
	v2.PrivacyCategory_PRIVACY_CATEGORY_PROFILE_PHOTO: types.PrivacySettingTypeProfile,
	v2.PrivacyCategory_PRIVACY_CATEGORY_ABOUT:         types.PrivacySettingTypeStatus,
	v2.PrivacyCategory_PRIVACY_CATEGORY_GROUP_ADD:     types.PrivacySettingTypeGroupAdd,
	v2.PrivacyCategory_PRIVACY_CATEGORY_CALL_ADD:      types.PrivacySettingTypeCallAdd,
	v2.PrivacyCategory_PRIVACY_CATEGORY_READ_RECEIPTS: types.PrivacySettingTypeReadReceipts,
}

var privacyValues = map[v2.PrivacyValue]types.PrivacySetting{
	v2.PrivacyValue_PRIVACY_VALUE_ALL:             types.PrivacySettingAll,
	v2.PrivacyValue_PRIVACY_VALUE_CONTACTS:        types.PrivacySettingContacts,
	v2.PrivacyValue_PRIVACY_VALUE_CONTACTS_EXCEPT: types.PrivacySettingContactBlacklist,
	v2.PrivacyValue_PRIVACY_VALUE_NOBODY:          types.PrivacySettingNone,
	v2.PrivacyValue_PRIVACY_VALUE_MATCH_LAST_SEEN: types.PrivacySettingMatchLastSeen,
	v2.PrivacyValue_PRIVACY_VALUE_KNOWN:           types.PrivacySettingKnown,
}

// privacyTakes is what each category takes, as whatsapp's own settings
// offer them
var privacyTakes = map[v2.PrivacyCategory][]v2.PrivacyValue{
	v2.PrivacyCategory_PRIVACY_CATEGORY_LAST_SEEN:     {v2.PrivacyValue_PRIVACY_VALUE_ALL, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS_EXCEPT, v2.PrivacyValue_PRIVACY_VALUE_NOBODY},
	v2.PrivacyCategory_PRIVACY_CATEGORY_PROFILE_PHOTO: {v2.PrivacyValue_PRIVACY_VALUE_ALL, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS_EXCEPT, v2.PrivacyValue_PRIVACY_VALUE_NOBODY},
	v2.PrivacyCategory_PRIVACY_CATEGORY_ABOUT:         {v2.PrivacyValue_PRIVACY_VALUE_ALL, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS_EXCEPT, v2.PrivacyValue_PRIVACY_VALUE_NOBODY},
	v2.PrivacyCategory_PRIVACY_CATEGORY_GROUP_ADD:     {v2.PrivacyValue_PRIVACY_VALUE_ALL, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS, v2.PrivacyValue_PRIVACY_VALUE_CONTACTS_EXCEPT},
	v2.PrivacyCategory_PRIVACY_CATEGORY_ONLINE:        {v2.PrivacyValue_PRIVACY_VALUE_ALL, v2.PrivacyValue_PRIVACY_VALUE_MATCH_LAST_SEEN},
	v2.PrivacyCategory_PRIVACY_CATEGORY_CALL_ADD:      {v2.PrivacyValue_PRIVACY_VALUE_ALL, v2.PrivacyValue_PRIVACY_VALUE_KNOWN},
	v2.PrivacyCategory_PRIVACY_CATEGORY_READ_RECEIPTS: {v2.PrivacyValue_PRIVACY_VALUE_ALL, v2.PrivacyValue_PRIVACY_VALUE_NOBODY},
}

func (x *commands) privacy(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetPrivacySet()
	name, ok := privacyNames[p.GetCategory()]
	if !ok {
		return nil, invalid("no privacy category %v", p.GetCategory())
	}
	takes := false
	for _, v := range privacyTakes[p.GetCategory()] {
		takes = takes || v == p.GetValue()
	}
	if !takes {
		return nil, invalid("%v can't be %v", p.GetCategory(), p.GetValue())
	}
	return nil, wire(x.c.SetPrivacy(ctx, name, privacyValues[p.GetValue()]))
}

func (x *commands) defaultTimer(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return nil, wire(x.c.SetDefaultTimer(ctx, req.GetPrivacySetDefaultTimer().GetSeconds()))
}

func (x *commands) prefs(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return nil, wire(x.c.SetPreferences(ctx, req.GetPreferencesSet()))
}

func (x *commands) about(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return nil, wire(x.c.SetAbout(ctx, req.GetSelfSetAbout().GetText()))
}

func (x *commands) block(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetContactBlock()
	key, err := x.person(ctx, p.GetPerson())
	if err != nil {
		return nil, err
	}
	if model.IsGroup(key) {
		return nil, invalid("a group can't be blocked")
	}
	return nil, wire(x.c.Block(ctx, key, p.GetBlocked()))
}

func (x *commands) stickerFavorite(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetStickerFavorite()
	var key, msg string
	switch p.WhichSticker() {
	case v2.StickerFavorite_StickerId_case:
		key = p.GetStickerId()
	case v2.StickerFavorite_MessageId_case:
		r, err := message(p.GetMessageId())
		if err != nil {
			return nil, err
		}
		msg = r.ID
	default:
		return nil, invalid("sticker_id or message_id is required")
	}
	return nil, wire(x.c.SetStickerFavorite(ctx, key, msg, p.GetFavorite()))
}

func (x *commands) stickerDownload(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	key := req.GetStickerDownload().GetStickerId()
	if key == "" {
		return nil, invalid("sticker_id is required")
	}
	return nil, wire(x.c.DownloadSticker(ctx, key))
}

func (x *commands) packInstall(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetStickerPackInstall()
	if p.GetPackId() == "" {
		return nil, invalid("pack_id is required")
	}
	return nil, wire(x.c.SetStickerPackInstalled(ctx, p.GetPackId(), p.GetInstalled()))
}

func (x *commands) packsRefresh(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return nil, wire(x.c.RefreshStickerPacks(ctx))
}

func (x *commands) dismiss(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	x.c.DismissNotification(req.GetNotificationDismiss().GetId())
	return nil, nil
}

func (x *commands) checkPhone(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	res, err := x.c.CheckPhone(ctx, req.GetContactCheckPhone().GetPhone())
	if err != nil {
		return nil, wire(err)
	}
	b := v2.ContactCheckPhoneResult_builder{Registered: res.Registered, Phone: res.Phone, Name: res.Name, Business: res.Business}
	if res.Registered {
		w, err := x.rs.World(ctx)
		if err != nil {
			return nil, err
		}
		if b.PersonId, err = x.id(ctx, w.Now(model.Norm(res.JID))); err != nil {
			return nil, err
		}
	}
	resp := &v2.Response{}
	resp.SetContactCheckPhone(b.Build())
	return resp, nil
}

func (x *commands) session(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	p := req.GetSessionUpdate()
	s.Update(p.GetFocused(), p.GetActiveChatId(), p.GetShowsNotifications())
	return nil, nil
}

// sessions tells the client what every frontend is and shows: which chat is
// open where, whose presence is on screen, which sticker packs are open.
func (x *commands) sessions() {
	ctx := context.Background()
	w, err := x.rs.World(ctx)
	if err != nil {
		return
	}
	key := func(id string) string {
		if id == "" {
			return ""
		}
		k, _ := x.rs.IDs().Key(w, id)
		return k
	}
	var fs []whatsapp.Frontend
	packs := false
	for _, st := range x.srv.Sessions() {
		f := whatsapp.Frontend{Focused: st.Focused, Active: key(st.Active), Notifies: st.Notifies}
		for _, sub := range st.Shown {
			switch sub.WhichView() {
			case v2.Subscribe_Presence_case:
				if k := key(sub.GetPresence().GetChatId()); k != "" && !model.IsGroup(k) {
					f.Watching = append(f.Watching, k)
				}
			case v2.Subscribe_StickerPacks_case:
				packs = true
			case v2.Subscribe_StickerPack_case:
				if id := sub.GetStickerPack().GetPackId(); id != "" {
					x.c.WantStickerPack(id)
				}
			}
		}
		fs = append(fs, f)
	}
	x.c.SetFrontends(fs)
	if packs {
		x.c.WantStickerPacks()
	}
}
