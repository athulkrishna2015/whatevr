package wa

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/app"
)

// CommunityGroup is one sub-group linked under a community: its JID plus the
// best-effort display name (empty when the group is not in the local store).
type CommunityGroup = app.CommunityGroup

// ListCommunitySubgroups returns the sub-groups linked under a community,
// newest directory first as WhatsApp serves them. Sub-groups are ordinary
// groups otherwise: opening one uses the normal chat flow.
func (c *Client) ListCommunitySubgroups(ctx context.Context, chatID string) ([]CommunityGroup, error) {
	client, err := c.requireConnectedClient()
	if err != nil {
		return nil, err
	}
	community, err := c.requireGroupJID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	targets, err := client.GetSubGroups(ctx, community)
	if err != nil {
		return nil, err
	}
	groups := make([]CommunityGroup, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.JID.IsEmpty() {
			continue
		}
		id := c.normalizeJIDForChat(ctx, target.JID).String()
		name := strings.TrimSpace(target.GroupName.Name)
		if name == "" {
			if chat, err := c.store.GetChat(ctx, id); err == nil {
				name = chat.Name
			}
		}
		groups = append(groups, CommunityGroup{ID: id, Name: name})
	}
	return groups, nil
}

// LinkCommunityGroup attaches an existing group under a community. Both cards
// move — the community gains a directory row, the group learns the community it
// now sits under — so both are re-fetched. Spawned rather than inline: three
// round trips (group info, then the directory) must not sit in front of the
// command's response, and the card the frontend holds only has to catch up
// before the user looks at it, not before the button returns.
func (c *Client) LinkCommunityGroup(ctx context.Context, communityID, groupID string) error {
	client, err := c.requireConnectedClient()
	if err != nil {
		return err
	}
	community, err := c.requireGroupJID(ctx, communityID)
	if err != nil {
		return err
	}
	group, err := c.requireGroupJID(ctx, groupID)
	if err != nil {
		return err
	}
	if err := client.LinkGroup(ctx, community, group); err != nil {
		return err
	}
	c.refreshLinkedGroupCards(community, group)
	return nil
}

// UnlinkCommunityGroup detaches a sub-group from its community. The same two
// cards move as in LinkCommunityGroup, in the other direction.
func (c *Client) UnlinkCommunityGroup(ctx context.Context, communityID, groupID string) error {
	client, err := c.requireConnectedClient()
	if err != nil {
		return err
	}
	community, err := c.requireGroupJID(ctx, communityID)
	if err != nil {
		return err
	}
	group, err := c.requireGroupJID(ctx, groupID)
	if err != nil {
		return err
	}
	if err := client.UnlinkGroup(ctx, community, group); err != nil {
		return err
	}
	c.refreshLinkedGroupCards(community, group)
	return nil
}

// refreshLinkedGroupCards re-publishes a community's card and a sub-group's
// card after their link changed. Each refresh is the same best-effort live
// fetch a plain group-info open runs; an empty avatar path keeps whatever the
// card already resolved (the same rule every other refresh uses).
func (c *Client) refreshLinkedGroupCards(jids ...types.JID) {
	for _, jid := range jids {
		if jid.IsEmpty() {
			continue
		}
		c.spawn(func(ctx context.Context) { c.refreshGroupInfoLive(ctx, jid, "") })
	}
}
