package whatsapp

import (
	"context"
	"strings"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/core"
	"whatevrd/internal/ingest"
	"whatevrd/internal/model"
)

// This file implements the status.* commands over the broadcast rows: the
// feed itself is the status view, so posting, viewing, saving, replying,
// deleting and muting are all operations on message rows. Our own posts
// go out through the same queue as chat sends and echo back into the feed.

// statusChat is where status updates fold: the broadcast pseudo-chat, never
// a row in the chat list.
const statusChat = "status@broadcast"

// defaultStatusBackground is the backdrop of a text status posted without
// one, the same blue official clients use.
const defaultStatusBackground = 0xFF285CF0

// statusFont clamps a caller font id to the WhatsApp FontType enum,
// defaulting to SYSTEM for anything unknown.
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

// statusRef resolves a status token to its message. Statuses live under the
// broadcast pseudo-chat, whatever address the token names.
func (c *Client) statusRef(ctx context.Context, token string) (model.Message, *model.World, error) {
	addr, id, ok := splitToken(token)
	if !ok {
		return model.Message{}, nil, Errorf(ErrInvalid, "malformed status id %q", token)
	}
	_ = addr
	return c.message(ctx, Ref{Chat: statusChat, ID: id})
}

func splitToken(token string) (string, string, bool) {
	addr, id, ok := strings.Cut(token, "/")
	return addr, id, ok && addr != "" && id != ""
}

// MarkStatusViewed flags a status as seen locally and kicks off its media
// fetch, best effort: viewed-but-never-downloaded rows cannot linger.
func (c *Client) MarkStatusViewed(ctx context.Context, token string) error {
	m, _, err := c.statusRef(ctx, token)
	if err != nil {
		return err
	}
	h := core.LocalHead{Chat: m.Chat, ID: m.ID, Op: model.StatusViewOp}
	if err := c.append(ctx, core.KindLocal, h, nil); err != nil {
		return err
	}
	if m.Facts.Local.File == "" {
		c.spawn(func(ctx context.Context) {
			_ = c.Download(ctx, Ref{Chat: m.Chat, ID: m.ID})
		})
	}
	return nil
}

// PostStatusText publishes a text status with a background color and font.
func (c *Client) PostStatusText(ctx context.Context, text string, background uint32, font int32) (Ref, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return Ref{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Ref{}, Errorf(ErrInvalid, "status text is required")
	}
	if background == 0 {
		background = defaultStatusBackground
	}
	body := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(text), BackgroundArgb: proto.Uint32(background),
		TextArgb: proto.Uint32(0xFFFFFFFF), Font: statusFont(font),
	}}
	to := types.StatusBroadcastJID
	id := string(cli.GenerateMessageID())
	if err := c.ingest.Queued(ctx, to.ToNonAD().String(), id, body, "", ingest.Once{}); err != nil {
		return Ref{}, err
	}
	c.signalSender()
	return Ref{Chat: to.ToNonAD().String(), ID: id}, nil
}

// PostStatusMedia publishes a photo, video or audio status with a caption.
func (c *Client) PostStatusMedia(ctx context.Context, path, caption string) (Ref, error) {
	return c.SendMedia(ctx, Draft{Chat: statusChat}, path, caption, false, false)
}

// DeleteStatus deletes one of our own statuses for everyone.
func (c *Client) DeleteStatus(ctx context.Context, token string) error {
	m, _, err := c.statusRef(ctx, token)
	if err != nil {
		return err
	}
	if !m.FromMe {
		return Errorf(ErrRejected, "only your own statuses can be deleted")
	}
	return c.Revoke(ctx, Ref{Chat: m.Chat, ID: m.ID})
}

// StatusViewers lists who opened one of our own statuses: the read receipts
// folded onto its row, newest first.
func (c *Client) StatusViewers(ctx context.Context, token string) ([]model.Receipt, error) {
	m, _, err := c.statusRef(ctx, token)
	if err != nil {
		return nil, err
	}
	if !m.FromMe {
		return nil, Errorf(ErrRejected, "only your own statuses have viewers")
	}
	out := append([]model.Receipt(nil), m.Facts.Receipts...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// SetStatusMuted hides (or unhides) a contact's statuses. The phone remains
// the source of truth and overwrites this set on the next full sync.
func (c *Client) SetStatusMuted(ctx context.Context, sender string, muted bool) error {
	if strings.TrimSpace(sender) == "" {
		return Errorf(ErrInvalid, "sender_id is required")
	}
	keys := []string{strings.TrimSpace(sender)}
	if w, err := c.world(); err == nil {
		if key := w.Now(model.Norm(sender)); key != "" && key != keys[0] {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		if err := c.append(ctx, core.KindStatusMute, core.StatusMuteHead{Sender: key, Muted: muted}, nil); err != nil {
			return err
		}
	}
	c.waitLogged(ctx)
	return nil
}

// ReplyToStatus sends a chat message to the status author quoting their
// status, the way official clients reply to stories.
func (c *Client) ReplyToStatus(ctx context.Context, token, text string) (Ref, error) {
	if strings.TrimSpace(text) == "" {
		return Ref{}, Errorf(ErrInvalid, "reply text is required")
	}
	m, _, err := c.statusRef(ctx, token)
	if err != nil {
		return Ref{}, err
	}
	if m.FromMe {
		return Ref{}, Errorf(ErrRejected, "only others' statuses can be replied to")
	}
	if m.Sender == "" {
		return Ref{}, Errorf(ErrInvalid, "status has no sender")
	}
	return c.SendText(ctx, Draft{Chat: m.Sender, ReplyTo: &Ref{Chat: m.Chat, ID: m.ID}}, strings.TrimSpace(text))
}

// RejectCall rejects the latest ringing call in a chat, if any. Rejecting a
// call that already ended is a silent no-op.
func (c *Client) RejectCall(ctx context.Context, chat string) error {
	if strings.TrimSpace(chat) == "" {
		return Errorf(ErrInvalid, "chat_id is required")
	}
	w, err := c.world()
	if err != nil {
		return err
	}
	key := w.Now(model.Norm(chat))
	ringing, err := c.r.Ringing(ctx)
	if err != nil {
		return err
	}
	var match *model.RingingCall
	for i, call := range ringing {
		from := call.From
		if call.Group != "" {
			from = call.Group
		}
		if w.Now(model.Norm(from)) == key {
			match = &ringing[i]
			break
		}
	}
	if match == nil {
		return nil
	}
	cli, err := c.connected()
	if err != nil {
		return err
	}
	from, err := types.ParseJID(match.From)
	if err != nil {
		return Errorf(ErrInvalid, "call from %q", match.From)
	}
	if err := cli.RejectCall(ctx, from, match.ID); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	return nil
}
