package whatsapp

import (
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/model"
)

// This file implements the group.* and community.* management commands:
// create, leave, rename, re-describe, photo, invite links, membership and
// flags. Reads (info card, member list) already exist via the group views;
// everything here mutates through whatsmeow and then refreshes the group so
// receipts, @-mentions and the info card stay correct.

// groupJID resolves a chat id to its group JID.
func (c *Client) groupJID(chat string) (types.JID, error) {
	w, err := c.world()
	if err != nil {
		return types.JID{}, err
	}
	key := w.Now(model.Norm(chat))
	j, err := types.ParseJID(key)
	if err != nil || j.User == "" {
		return types.JID{}, Errorf(ErrInvalid, "invalid chat_id")
	}
	if j.Server != types.GroupServer {
		return types.JID{}, Errorf(ErrInvalid, "not a group chat")
	}
	return j, nil
}

// memberJIDs parses member ids to user JIDs: groups are refused, and a phone
// number we know the LID for goes by LID.
func (c *Client) memberJIDs(raw []string) ([]types.JID, error) {
	w, err := c.world()
	if err != nil {
		return nil, err
	}
	var out []types.JID
	for _, s := range raw {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		key := w.Now(model.Norm(s))
		if key == "" {
			key = s
		}
		j, err := types.ParseJID(key)
		if err != nil || j.User == "" {
			return nil, Errorf(ErrInvalid, "invalid member %q", s)
		}
		if j.Server == types.GroupServer {
			return nil, Errorf(ErrInvalid, "member %q must be a user, not a group", s)
		}
		out = append(out, j.ToNonAD())
	}
	return out, nil
}

// refreshGroup re-reads a group after a mutation so the store (and every
// view over it) reflects what the server accepted.
func (c *Client) refreshGroup(ctx context.Context, cli *whatsmeow.Client, jid types.JID) {
	info, err := cli.GetGroupInfo(ctx, jid)
	if err != nil || info == nil {
		if err != nil && ctx.Err() == nil {
			c.log.Warn().Err(err).Str("chat", jid.String()).Msg("whatsapp: refresh group after change")
		}
		return
	}
	if err := c.ingest.Group(ctx, info); err != nil {
		c.log.Warn().Err(err).Str("chat", jid.String()).Msg("whatsapp: log refreshed group")
		return
	}
	c.waitLogged(ctx)
}

// CreateGroup creates a group with an optional member list and photo. It
// answers the chat key; rows for it arrive through the views like any other
// new chat.
func (c *Client) CreateGroup(ctx context.Context, name string, members []string, photoPath string) (string, error) {
	cli, err := c.connected()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", Errorf(ErrInvalid, "group name is required")
	}
	if utf8.RuneCountInString(name) > 100 {
		return "", Errorf(ErrInvalid, "group name must be <= 100 characters")
	}
	parsed, err := c.memberJIDs(members)
	if err != nil {
		return "", err
	}
	info, err := cli.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: strings.TrimSpace(name), Participants: parsed})
	if err != nil {
		return "", Errorf(ErrRejected, "%v", err)
	}
	if photoPath = strings.TrimSpace(photoPath); photoPath != "" {
		if err := c.setGroupPhoto(ctx, cli, info.JID, photoPath); err != nil {
			c.log.Warn().Err(err).Str("chat", info.JID.String()).Msg("whatsapp: group created but photo set failed")
		}
	}
	c.refreshGroup(ctx, cli, info.JID)
	return info.JID.ToNonAD().String(), nil
}

// LeaveGroup leaves a group. The row stays (history is kept).
func (c *Client) LeaveGroup(ctx context.Context, chat string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	if err := cli.LeaveGroup(ctx, jid); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	return nil
}

// SetGroupName renames a group.
func (c *Client) SetGroupName(ctx context.Context, chat, name string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return Errorf(ErrInvalid, "group name is required")
	}
	if err := cli.SetGroupName(ctx, jid, strings.TrimSpace(name)); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// SetGroupDescription updates a group's description.
func (c *Client) SetGroupDescription(ctx context.Context, chat, description string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	if err := cli.SetGroupTopic(ctx, jid, "", "", strings.TrimSpace(description)); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// SetGroupPhoto sets (or, with an empty path, clears) a group's photo.
func (c *Client) SetGroupPhoto(ctx context.Context, chat, photoPath string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	if err := c.setGroupPhoto(ctx, cli, jid, strings.TrimSpace(photoPath)); err != nil {
		return err
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

func (c *Client) setGroupPhoto(ctx context.Context, cli *whatsmeow.Client, jid types.JID, photoPath string) error {
	if photoPath == "" {
		_, err := cli.SetGroupPhoto(ctx, jid, nil)
		return err
	}
	data, err := groupPhotoBytes(photoPath)
	if err != nil {
		return err
	}
	_, err = cli.SetGroupPhoto(ctx, jid, data)
	if err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	return nil
}

// groupPhotoBytes reads a photo file: absolute, regular, no symlink,
// non-empty, an image, at most 16 MB.
func groupPhotoBytes(photoPath string) ([]byte, error) {
	if !filepath.IsAbs(photoPath) {
		return nil, Errorf(ErrInvalid, "photo path must be absolute")
	}
	info, err := os.Lstat(photoPath)
	if err != nil {
		return nil, Errorf(ErrInvalid, "photo file is not accessible")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, Errorf(ErrInvalid, "photo file must not be a symlink")
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, Errorf(ErrInvalid, "photo file is empty")
	}
	if info.Size() > 16<<20 {
		return nil, Errorf(ErrInvalid, "photo file is too big")
	}
	data, err := os.ReadFile(photoPath)
	if err != nil {
		return nil, Errorf(ErrInvalid, "photo file is not accessible")
	}
	f, err := os.Open(photoPath)
	if err != nil {
		return nil, Errorf(ErrInvalid, "photo file is not accessible")
	}
	defer f.Close()
	if _, _, err := image.DecodeConfig(f); err != nil {
		return nil, Errorf(ErrInvalid, "photo file is not an image")
	}
	return data, nil
}

// GroupInviteLink returns the invite link, resetting it first when asked.
func (c *Client) GroupInviteLink(ctx context.Context, chat string, reset bool) (string, error) {
	cli, err := c.connected()
	if err != nil {
		return "", err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return "", err
	}
	link, err := cli.GetGroupInviteLink(ctx, jid, reset)
	if err != nil {
		return "", Errorf(ErrRejected, "%v", err)
	}
	return link, nil
}

// JoinGroupLink joins a group from an invite link or bare code. It answers
// the chat key; rows for it arrive through the views like any other chat.
func (c *Client) JoinGroupLink(ctx context.Context, codeOrLink string) (string, error) {
	cli, err := c.connected()
	if err != nil {
		return "", err
	}
	code := strings.TrimSpace(codeOrLink)
	if code == "" {
		return "", Errorf(ErrInvalid, "invite link or code is required")
	}
	for _, prefix := range []string{"https://chat.whatsapp.com/", "http://chat.whatsapp.com/", "chat.whatsapp.com/"} {
		code = strings.TrimPrefix(code, prefix)
	}
	if code = strings.TrimSpace(code); code == "" {
		return "", Errorf(ErrInvalid, "invite link or code is required")
	}
	jid, err := cli.JoinGroupWithLink(ctx, code)
	if err != nil {
		return "", Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return jid.ToNonAD().String(), nil
}

// UpdateGroupMembers adds, removes, promotes or demotes members.
func (c *Client) UpdateGroupMembers(ctx context.Context, chat, action string, members []string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	var change whatsmeow.ParticipantChange
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "add":
		change = whatsmeow.ParticipantChangeAdd
	case "remove":
		change = whatsmeow.ParticipantChangeRemove
	case "promote":
		change = whatsmeow.ParticipantChangePromote
	case "demote":
		change = whatsmeow.ParticipantChangeDemote
	default:
		return Errorf(ErrInvalid, "action must be one of add, remove, promote, demote")
	}
	parsed, err := c.memberJIDs(members)
	if err != nil {
		return err
	}
	if len(parsed) == 0 {
		return Errorf(ErrInvalid, "at least one member is required")
	}
	if _, err := cli.UpdateGroupParticipants(ctx, jid, parsed, change); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// SetGroupAnnounce toggles admins-only sending.
func (c *Client) SetGroupAnnounce(ctx context.Context, chat string, announce bool) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	if err := cli.SetGroupAnnounce(ctx, jid, announce); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// SetGroupLocked toggles admins-only group-info editing.
func (c *Client) SetGroupLocked(ctx context.Context, chat string, locked bool) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	if err := cli.SetGroupLocked(ctx, jid, locked); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// LinkCommunityGroup links a group to a community.
func (c *Client) LinkCommunityGroup(ctx context.Context, community, group string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	communityJID, err := c.groupJID(community)
	if err != nil {
		return err
	}
	groupJID, err := c.groupJID(group)
	if err != nil {
		return err
	}
	if err := cli.LinkGroup(ctx, communityJID, groupJID); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, communityJID)
	c.refreshGroup(ctx, cli, groupJID)
	return nil
}

// UnlinkCommunityGroup unlinks a group from a community.
func (c *Client) UnlinkCommunityGroup(ctx context.Context, community, group string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	communityJID, err := c.groupJID(community)
	if err != nil {
		return err
	}
	groupJID, err := c.groupJID(group)
	if err != nil {
		return err
	}
	if err := cli.UnlinkGroup(ctx, communityJID, groupJID); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, communityJID)
	c.refreshGroup(ctx, cli, groupJID)
	return nil
}

// SetGroupMemberAddMode toggles whether anyone in the group may add people,
// or only admins. whatsapp's "member_add_mode": "all_member_add" / "admin_add".
func (c *Client) SetGroupMemberAddMode(ctx context.Context, chat string, anyoneCanAdd bool) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	mode := types.GroupMemberAddModeAdmin
	if anyoneCanAdd {
		mode = types.GroupMemberAddModeAllMember
	}
	if err := cli.SetGroupMemberAddMode(ctx, jid, mode); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// SetGroupJoinApproval toggles whether joining this group needs an admin's
// approval. Requests themselves live in GroupJoinRequests.
func (c *Client) SetGroupJoinApproval(ctx context.Context, chat string, required bool) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	if err := cli.SetGroupJoinApprovalMode(ctx, jid, required); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// GroupJoinRequest is one person waiting to be let into a group.
type GroupJoinRequest struct {
	JID         string
	RequestedAt int64
}

// GroupJoinRequests reads who is waiting, for admins of an approval group.
func (c *Client) GroupJoinRequests(ctx context.Context, chat string) ([]GroupJoinRequest, error) {
	cli, err := c.connected()
	if err != nil {
		return nil, err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return nil, err
	}
	reqs, err := cli.GetGroupRequestParticipants(ctx, jid)
	if err != nil {
		return nil, Errorf(ErrRejected, "%v", err)
	}
	out := make([]GroupJoinRequest, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, GroupJoinRequest{JID: r.JID.ToNonAD().String(), RequestedAt: r.RequestedAt.UnixMilli()})
	}
	return out, nil
}

// ResolveGroupJoinRequests approves or rejects the people waiting to join.
// action is "approve" or "reject"; an empty member list resolves everyone.
func (c *Client) ResolveGroupJoinRequests(ctx context.Context, chat, action string, members []string) error {
	cli, err := c.connected()
	if err != nil {
		return err
	}
	jid, err := c.groupJID(chat)
	if err != nil {
		return err
	}
	var change whatsmeow.ParticipantRequestChange
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve":
		change = whatsmeow.ParticipantChangeApprove
	case "reject":
		change = whatsmeow.ParticipantChangeReject
	default:
		return Errorf(ErrInvalid, "action must be approve or reject")
	}
	var jids []types.JID
	if len(members) == 0 {
		// nobody named: resolve the whole waiting list, which is what an
		// admin means by "approve all"
		reqs, err := cli.GetGroupRequestParticipants(ctx, jid)
		if err != nil {
			return Errorf(ErrRejected, "%v", err)
		}
		for _, r := range reqs {
			jids = append(jids, r.JID)
		}
	} else {
		if jids, err = c.memberJIDs(members); err != nil {
			return err
		}
	}
	if len(jids) == 0 {
		return nil
	}
	if _, err := cli.UpdateGroupRequestParticipants(ctx, jid, jids, change); err != nil {
		return Errorf(ErrRejected, "%v", err)
	}
	c.refreshGroup(ctx, cli, jid)
	return nil
}

// GroupInvitePreview is what a chat.whatsapp.com invite leads to, before the
// frontend commits to joining it.
type GroupInvitePreview struct {
	JID          string
	Name         string
	Topic        string
	CreatedMs    int64
	MemberCount  uint32
	Announce     bool
	Locked       bool
	Approval     bool
	Parent       bool
}

// PreviewGroupInvite describes the group behind an invite link or bare code,
// without joining it. zapfast resolves before it joins; joining blind means
// committing to a group the user never got to look at.
func (c *Client) PreviewGroupInvite(ctx context.Context, codeOrLink string) (GroupInvitePreview, error) {
	cli, err := c.connected()
	if err != nil {
		return GroupInvitePreview{}, err
	}
	code := strings.TrimSpace(codeOrLink)
	if code == "" {
		return GroupInvitePreview{}, Errorf(ErrInvalid, "invite link or code is required")
	}
	for _, prefix := range []string{"https://chat.whatsapp.com/", "http://chat.whatsapp.com/", "chat.whatsapp.com/"} {
		code = strings.TrimPrefix(code, prefix)
	}
	if code = strings.TrimSpace(code); code == "" {
		return GroupInvitePreview{}, Errorf(ErrInvalid, "invite link or code is required")
	}
	info, err := cli.GetGroupInfoFromLink(ctx, code)
	if err != nil {
		return GroupInvitePreview{}, Errorf(ErrRejected, "%v", err)
	}
	if info == nil {
		return GroupInvitePreview{}, Errorf(ErrNotFound, "no group behind that invite")
	}
	return GroupInvitePreview{
		JID:         info.JID.ToNonAD().String(),
		Name:        info.Name,
		Topic:       info.Topic,
		CreatedMs:   info.GroupCreated.UnixMilli(),
		MemberCount: uint32(info.ParticipantCount),
		Announce:    info.IsAnnounce,
		Locked:      info.IsLocked,
		Approval:    info.IsJoinApprovalRequired,
		Parent:      info.IsParent,
	}, nil
}

