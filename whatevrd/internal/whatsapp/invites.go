package whatsapp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	appstore "whatevrd/internal/store"
)

const (
	inviteLookTimeout = 20 * time.Second
	// a code whatsapp does not know gets silence, not a no
	inviteJoinTimeout = 15 * time.Second
)

// decoded is a message the model has and its row as the decoder reads it.
func (c *Client) decoded(ctx context.Context, ref Ref) (model.Message, *model.World, appstore.Message, error) {
	m, w, err := c.message(ctx, ref)
	if err != nil {
		return model.Message{}, nil, appstore.Message{}, err
	}
	sm, _, ok := NewDecoder(WorldNames(w), c.o.Paths.MediaCacheDir).Model(ctx, w, m)
	if !ok {
		return m, w, appstore.Message{}, Errorf(ErrRejected, "the message does not decode")
	}
	return m, w, sm, nil
}

// invite is the invite a row offers, with what was learned of it.
func (c *Client) invite(ctx context.Context, ref Ref) (model.Message, *appstore.GroupInvitePayload, error) {
	m, _, sm, err := c.decoded(ctx, ref)
	if err != nil {
		return m, nil, err
	}
	p := appstore.DecodePayload(sm.PayloadJSON).GroupInvite
	if sm.MediaKind != appstore.MediaKindGroupInvite || p == nil || p.Code == "" {
		return m, nil, Errorf(ErrInvalid, "the message is not a group invite")
	}
	var got appstore.GroupInvitePayload
	if l := m.Facts.Local; len(l.Invite) > 0 && json.Unmarshal(l.Invite, &got) == nil {
		p.Subject, p.Topic, p.MemberCount, p.ResolvedAt, p.Joined = got.Subject, got.Topic, got.MemberCount, got.ResolvedAt, got.Joined
	}
	return m, p, nil
}

func expired(p *appstore.GroupInvitePayload) bool {
	return p.ExpiresAt > 0 && time.Now().Unix() >= p.ExpiresAt
}

// parties is the group an invite offers and who offered it; one we sent is
// ours, which makes an invite we forwarded resolvable.
func (c *Client) parties(cli *whatsmeow.Client, m model.Message, p *appstore.GroupInvitePayload) (types.JID, types.JID, bool) {
	g, err := types.ParseJID(p.GroupJID)
	if err != nil || g.Server != types.GroupServer {
		return types.JID{}, types.JID{}, false
	}
	if m.FromMe || m.Sender == "" || m.Sender == model.Me {
		own := cli.Store.GetJID()
		if own.IsEmpty() {
			return types.JID{}, types.JID{}, false
		}
		return g, own.ToNonAD(), true
	}
	from := m.Sender
	if !model.IsGroup(m.Chat) && from == "" {
		from = m.Chat
	}
	inviter, err := types.ParseJID(from)
	if err != nil {
		return types.JID{}, types.JID{}, false
	}
	return g, inviter.ToNonAD(), true
}

// lookInvite asks whatsapp what a live invite points at. a lapsed code
// can't be looked up, so old invites cost nothing.
func (c *Client) lookInvite(ctx context.Context, ref Ref) {
	m, p, err := c.invite(ctx, ref)
	if err != nil || p.GroupJID == "" || expired(p) {
		return
	}
	cli := c.client()
	if cli == nil || !cli.IsLoggedIn() {
		return
	}
	g, inviter, ok := c.parties(cli, m, p)
	if !ok {
		return
	}
	c.spawn(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, inviteLookTimeout)
		defer cancel()
		// members ask group info, outsiders the invite: and whatsapp never
		// answers an outsider's question from a member, so this order
		if info, err := cli.GetGroupInfo(ctx, g); err == nil && info != nil {
			c.logInvite(ctx, m, resolved(info, true), "")
			return
		}
		info, err := cli.GetGroupInfoFromInvite(ctx, g, inviter, p.Code, p.ExpiresAt)
		switch {
		case ctx.Err() != nil:
		case err != nil:
			c.logInvite(ctx, m, nil, err.Error())
		case info != nil:
			c.logInvite(ctx, m, resolved(info, c.isMember(info)), "")
		}
	})
}

func resolved(info *types.GroupInfo, joined bool) *appstore.GroupInvitePayload {
	n := len(info.Participants)
	if n == 0 {
		n = info.ParticipantCount
	}
	return &appstore.GroupInvitePayload{ResolvedAt: time.Now().Unix(), Subject: strings.TrimSpace(info.Name),
		Topic: strings.TrimSpace(info.Topic), MemberCount: n, Joined: joined}
}

// isMember finds us in a group's list, under either address.
func (c *Client) isMember(info *types.GroupInfo) bool {
	w, err := c.world()
	if err != nil {
		return false
	}
	for _, p := range info.Participants {
		for _, j := range []types.JID{p.JID, p.PhoneNumber, p.LID} {
			if !j.IsEmpty() && w.IsSelf(j.ToNonAD().String()) {
				return true
			}
		}
	}
	return false
}

func (c *Client) logInvite(ctx context.Context, m model.Message, p *appstore.GroupInvitePayload, why string) {
	h := core.LocalHead{Chat: m.Chat, ID: m.ID, Op: core.LocalInvite, Error: why}
	if p != nil {
		h.Invite, _ = json.Marshal(p)
	}
	c.logLocal(ctx, h)
}

// JoinInvite joins the group an invite row offers and says the group's
// address; one we are in already is just its address.
func (c *Client) JoinInvite(ctx context.Context, ref Ref) (string, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return "", err
	}
	m, p, err := c.invite(ctx, ref)
	if err != nil {
		return "", err
	}
	g, inviter, ok := c.parties(cli, m, p)
	if !ok {
		return "", Errorf(ErrInvalid, "the invite names no group")
	}
	if p.Joined || c.inGroup(ctx, g.String()) {
		return g.String(), nil
	}
	if expired(p) {
		return "", Errorf(ErrExpired, "this invite has expired")
	}
	jctx, cancel := context.WithTimeout(ctx, inviteJoinTimeout)
	defer cancel()
	if err := cli.JoinGroupWithInvite(jctx, g, inviter, p.Code, p.ExpiresAt); err != nil {
		if jctx.Err() != nil && ctx.Err() == nil {
			return "", Errorf(ErrRejected, "WhatsApp did not answer; this invite may have been revoked")
		}
		return "", Errorf(ErrRejected, "join the group: %v", err)
	}
	done := *p
	done.Joined, done.ResolvedAt = true, time.Now().Unix()
	c.logInvite(ctx, m, &done, "")
	// the group's row comes with its info; logging it now makes the chat
	// there before the frontend opens it
	if info, err := cli.GetGroupInfo(ctx, g); err == nil && info != nil {
		if err := c.ingest.Group(ctx, info); err != nil {
			c.log.Warn().Err(err).Msg("whatsapp: log the joined group")
		} else if _, seq := c.core.Progress(); seq > 0 {
			_ = c.core.WaitFolded(ctx, seq)
		}
	}
	return g.String(), nil
}

// inGroup says the model has us in grp now.
func (c *Client) inGroup(ctx context.Context, grp string) bool {
	ms, err := c.r.Members(ctx, grp)
	if err != nil {
		return false
	}
	w, err := c.world()
	if err != nil {
		return false
	}
	for _, m := range ms {
		if m.In && w.IsSelf(m.JID) {
			return true
		}
	}
	return false
}
