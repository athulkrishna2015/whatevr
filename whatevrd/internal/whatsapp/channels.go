package whatsapp

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

// This file implements the readable slice of WhatsApp Channels
// (newsletters): refresh the followed directory, read a channel's posts
// live, follow and unfollow, mute, mark viewed and react. Admin posting is
// out of scope (like v1).

// parseChannelJID resolves a channel id to its newsletter JID.
func (c *Client) parseChannelJID(id string) (types.JID, error) {
	j, err := types.ParseJID(strings.TrimSpace(id))
	if err != nil || j.User == "" {
		return types.JID{}, Errorf(ErrInvalid, "invalid channel id")
	}
	if j.Server != types.NewsletterServer {
		return types.JID{}, Errorf(ErrInvalid, "not a channel")
	}
	return j, nil
}

// RefreshChannels refetches the subscribed-channel directory. It answers the
// rows; the channels view reads the same table.
func (c *Client) RefreshChannels(ctx context.Context) ([]model.Channel, error) {
	cli, err := c.connected()
	if err != nil {
		return nil, err
	}
	infos, err := cli.GetSubscribedNewsletters(ctx)
	if err != nil {
		return nil, Errorf(ErrRejected, "%v", err)
	}
	for _, info := range infos {
		if info == nil || info.ID.IsEmpty() {
			continue
		}
		h := core.NewsletterHead{Event: "directory", JID: info.ID.String(),
			Name:        strings.TrimSpace(info.ThreadMeta.Name.Text),
			Description: strings.TrimSpace(info.ThreadMeta.Description.Text),
			Followers:   int64(info.ThreadMeta.SubscriberCount),
			Verified:    info.ThreadMeta.VerificationState == types.NewsletterVerificationStateVerified,
		}
		if info.ViewerMeta != nil && info.ViewerMeta.Mute != types.NewsletterMuteOff {
			h.Mute = string(types.NewsletterMuteOn)
		}
		if err := c.append(ctx, core.KindNewsletter, h, nil); err != nil {
			return nil, err
		}
	}
	c.waitLogged(ctx)
	return c.r.Channels(ctx)
}

// FollowChannel follows a channel by JID.
func (c *Client) FollowChannel(ctx context.Context, id string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.parseChannelJID(id)
	if err != nil {
		return err
	}
	if err := cli.FollowNewsletter(ctx, jid); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	if err := c.append(ctx, core.KindNewsletter, core.NewsletterHead{JID: jid.String(), Event: "join"}, nil); err != nil {
		return err
	}
	c.waitLogged(ctx)
	return nil
}

// FollowChannelLink follows a channel from an invite code or link.
func (c *Client) FollowChannelLink(ctx context.Context, invite string) error {
	code := strings.TrimSpace(invite)
	if code == "" {
		return Errorf(ErrInvalid, "invite is required")
	}
	for _, prefix := range []string{"https://whatsapp.com/channel/", "http://whatsapp.com/channel/", "whatsapp.com/channel/"} {
		code = strings.TrimPrefix(code, prefix)
	}
	cli, err := c.connected()
	if err != nil {
		return err
	}
	info, err := cli.GetNewsletterInfoWithInvite(ctx, strings.TrimSpace(code))
	if err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	if info == nil || info.ID.IsEmpty() {
		return Errorf(ErrNotFound, "no channel for that invite")
	}
	if err := cli.FollowNewsletter(ctx, info.ID); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	if _, err := c.RefreshChannels(ctx); err != nil {
		c.log.Warn().Err(err).Msg("whatsapp: refresh channels after follow")
	}
	return nil
}

// UnfollowChannel leaves a channel.
func (c *Client) UnfollowChannel(ctx context.Context, id string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.parseChannelJID(id)
	if err != nil {
		return err
	}
	if err := cli.UnfollowNewsletter(ctx, jid); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	if err := c.append(ctx, core.KindNewsletter, core.NewsletterHead{JID: jid.String(), Event: "leave"}, nil); err != nil {
		return err
	}
	c.waitLogged(ctx)
	return nil
}

// SetChannelMuted mutes or unmutes a channel.
func (c *Client) SetChannelMuted(ctx context.Context, id string, muted bool) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.parseChannelJID(id)
	if err != nil {
		return err
	}
	if err := cli.NewsletterToggleMute(ctx, jid, muted); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	mute := types.NewsletterMuteOff
	if muted {
		mute = types.NewsletterMuteOn
	}
	if err := c.append(ctx, core.KindNewsletter,
		core.NewsletterHead{JID: jid.String(), Event: "mute", Mute: string(mute)}, nil); err != nil {
		return err
	}
	c.waitLogged(ctx)
	return nil
}

// MarkChannelViewed marks channel posts viewed up to their server IDs.
func (c *Client) MarkChannelViewed(ctx context.Context, id string, serverIDs []int64) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.parseChannelJID(id)
	if err != nil {
		return err
	}
	if len(serverIDs) == 0 {
		return Errorf(ErrInvalid, "at least one server id is required")
	}
	ids := make([]types.MessageServerID, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		ids = append(ids, types.MessageServerID(serverID))
	}
	if err := cli.NewsletterMarkViewed(ctx, jid, ids); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	return nil
}

// ReactToChannelMessage adds or removes our reaction on a channel post.
func (c *Client) ReactToChannelMessage(ctx context.Context, id string, serverID int64, emoji string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.parseChannelJID(id)
	if err != nil {
		return err
	}
	if err := cli.NewsletterSendReaction(ctx, jid, types.MessageServerID(serverID), strings.TrimSpace(emoji), ""); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	return nil
}

// ChannelMessage is one channel post: server id, text and view count.
type ChannelMessage struct {
	ServerID int64
	T        int64
	Text     string
	Fallback string
	Views    int64
}

// ChannelMessages fetches a channel's recent posts live (never stored).
// before pages older; 0 means latest.
func (c *Client) ChannelMessages(ctx context.Context, id string, count int, before int64) ([]ChannelMessage, error) {
	cli, err := c.connected()
	if err != nil {
		return nil, err
	}
	jid, err := c.parseChannelJID(id)
	if err != nil {
		return nil, err
	}
	if count <= 0 || count > 100 {
		count = 30
	}
	params := &whatsmeow.GetNewsletterMessagesParams{Count: count}
	if before > 0 {
		params.Before = types.MessageServerID(before)
	}
	msgs, err := cli.GetNewsletterMessages(ctx, jid, params)
	if err != nil {
		return nil, Errorf(ErrRejected, "%v", err)
	}
	out := make([]ChannelMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		text := channelPostText(msg.Message)
		out = append(out, ChannelMessage{
			ServerID: int64(msg.MessageServerID), T: msg.Timestamp.Unix(),
			Text: text, Fallback: text, Views: int64(msg.ViewsCount),
		})
	}
	return out, nil
}

// channelPostText is the one-line rendering of a channel post. Channel media
// is plaintext-hosted and not downloaded, so the feed shows the label
// rather than the bytes.
func channelPostText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if text := strings.TrimSpace(msg.GetConversation()); text != "" {
		return text
	}
	if text := strings.TrimSpace(msg.GetExtendedTextMessage().GetText()); text != "" {
		return text
	}
	switch {
	case msg.GetImageMessage() != nil:
		return "📷 Photo"
	case msg.GetVideoMessage() != nil:
		return "🎥 Video"
	case msg.GetAudioMessage() != nil:
		return "🎵 Audio"
	case msg.GetDocumentMessage() != nil:
		return "📄 Document"
	case msg.GetPollCreationMessage() != nil:
		return "📊 Poll: " + msg.GetPollCreationMessage().GetName()
	default:
		return "Unsupported message"
	}
}
