package ingest

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/core"
)

// fullGroup is a group as a fetch or a join describes it, all of it.
func fullGroup(g *types.GroupInfo, at time.Time) core.GroupInfoHead {
	h := core.GroupInfoHead{
		JID:       jid(g.JID),
		Full:      true,
		T:         unix(at),
		Name:      &g.Name,
		NameT:     unix(g.NameSetAt),
		Topic:     &g.Topic,
		TopicT:    unix(g.TopicSetAt),
		TopicDel:  g.TopicDeleted,
		Announce:  &g.IsAnnounce,
		Locked:    &g.IsLocked,
		Parent:    g.IsParent,
		LinkedTo:  jid(g.LinkedParentJID),
		Created:   unix(g.GroupCreated),
		Owner:     jid(g.OwnerJID),
		Addressed: string(g.AddressingMode),
	}
	timer := uint32(0)
	if g.IsEphemeral {
		timer = g.DisappearingTimer
	}
	h.Ephemeral = &timer
	for _, p := range g.Participants {
		h.Participants = append(h.Participants, core.GroupParticipant{
			JID: jid(p.JID), PN: jid(p.PhoneNumber), LID: jid(p.LID), Admin: p.IsAdmin, Super: p.IsSuperAdmin,
		})
	}
	return h
}

func groupChange(evt *events.GroupInfo) core.GroupInfoHead {
	h := core.GroupInfoHead{JID: jid(evt.JID), T: unix(evt.Timestamp), JoinReason: evt.JoinReason}
	if evt.Sender != nil {
		h.By = jid(*evt.Sender)
	}
	if evt.SenderPN != nil {
		h.ByPN = jid(*evt.SenderPN)
	}
	if n := evt.Name; n != nil {
		h.Name, h.NameT = &n.Name, unix(n.NameSetAt)
	}
	if tp := evt.Topic; tp != nil {
		h.Topic, h.TopicT, h.TopicDel = &tp.Topic, unix(tp.TopicSetAt), tp.TopicDeleted
	}
	if a := evt.Announce; a != nil {
		h.Announce = &a.IsAnnounce
	}
	if l := evt.Locked; l != nil {
		h.Locked = &l.IsLocked
	}
	if e := evt.Ephemeral; e != nil {
		timer := uint32(0)
		if e.IsEphemeral {
			timer = e.DisappearingTimer
		}
		h.Ephemeral = &timer
	}
	if d := evt.Delete; d != nil {
		h.Deleted, h.DeleteReason = d.Deleted, d.DeleteReason
	}
	if l := evt.Link; l != nil {
		if l.Type == types.GroupLinkChangeTypeParent {
			h.LinkedTo = jid(l.Group.JID)
		}
		h.LinkChange, h.LinkType, h.LinkName = "link", string(l.Type), l.Group.Name
	}
	if l := evt.Unlink; l != nil {
		h.LinkChange, h.LinkType, h.LinkName = "unlink", string(l.Type), l.Group.Name
	}
	if m := evt.MembershipApprovalMode; m != nil {
		h.Approval = &m.IsJoinApprovalRequired
	}
	h.InviteLink = evt.NewInviteLink != nil
	for _, j := range evt.Join {
		h.Join = append(h.Join, jid(j))
	}
	for _, j := range evt.Leave {
		h.Leave = append(h.Leave, jid(j))
	}
	for _, j := range evt.Promote {
		h.Promote = append(h.Promote, jid(j))
	}
	for _, j := range evt.Demote {
		h.Demote = append(h.Demote, jid(j))
	}
	return h
}

func callHead(m types.BasicCallMeta, event string) core.CallHead {
	return core.CallHead{
		ID: m.CallID, From: jid(m.CallCreator), Alt: jid(m.CallCreatorAlt), T: unix(m.Timestamp),
		Event: event, Group: jid(m.GroupJID),
	}
}

// Group logs a group as whatsapp described it just now.
func (g *Ingest) Group(ctx context.Context, info *types.GroupInfo) error {
	in, err := input(core.KindGroupInfo, fullGroup(info, time.Time{}), nil)
	if err != nil {
		return err
	}
	_, err = g.log.AppendBatch(ctx, []core.Input{in})
	return err
}

// Event logs an event the daemon made up itself, as if whatsmeow had handed
// it over: our own read receipt, which whatsmeow never echoes.
func (g *Ingest) Event(ctx context.Context, evt any) error {
	ins, err := inputsFor(evt)
	if err != nil || len(ins) == 0 {
		return err
	}
	_, err = g.log.AppendBatch(ctx, ins)
	return err
}
