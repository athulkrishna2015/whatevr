package views

import (
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/model"
	"whatevrd/internal/store"
	"whatevrd/internal/whatsapp"
)

// Token is the id a message row goes by: the address it is named under and
// whatsapp's own id.
func Token(m model.Message) string { return MessageToken(m.Home, m.ID) }

func MessageToken(addr, id string) string { return addr + "/" + id }

// SplitToken takes a message id apart.
func SplitToken(tok string) (addr, id string, ok bool) {
	addr, id, ok = strings.Cut(tok, "/")
	return addr, id, ok && addr != "" && id != ""
}

// chatCtx is the chat rows are built for.
type chatCtx struct {
	key   string
	group bool
	addrs []string
	name  string
}

func chatOf(c model.Chat) chatCtx {
	return chatCtx{key: c.Key, group: c.Group, addrs: c.Addrs, name: c.Name}
}

// names is the world as the decoder asks it.
type names struct{ w *model.World }

func (n names) Norm(j types.JID) types.JID {
	if j.Server != types.HiddenUserServer {
		return j
	}
	if pn := n.w.PN(n.w.Now(j.ToNonAD().String())); pn != "" {
		return jid(pn)
	}
	return j
}

// Name falls back to the bare user of anything but a lid, a bot's number say.
func (n names) Name(j types.JID) string {
	if name, _ := n.w.Name(n.w.Now(j.ToNonAD().String())); name != "" || j.Server == types.HiddenUserServer {
		return name
	}
	return j.User
}

func (n names) Own(j types.JID) bool { return !j.IsEmpty() && n.w.IsSelf(j.ToNonAD().String()) }

func jid(s string) types.JID {
	j, _ := types.ParseJID(s)
	return j
}

// messages is ms as rows, with the quotes among them pointed at the copies
// they name.
func (c *rc) messages(ch chatCtx, ms []model.Message) []*v2.MessageRow {
	out := make([]*v2.MessageRow, len(ms))
	for i, m := range ms {
		out[i] = c.message(ch, m)
	}
	c.homeQuotes(ch, out)
	return out
}

// homeQuotes renames each quote by the copy its message is named under, so
// a frontend finds it among the rows it holds.
func (c *rc) homeQuotes(ch chatCtx, rows []*v2.MessageRow) {
	var ids []string
	for _, r := range rows {
		if q := r.GetReplyTo(); q != nil {
			if _, id, ok := SplitToken(q.GetMessageId()); ok {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	homes, err := c.r.Homes(c.ctx, ch.addrs, ids)
	if err != nil {
		c.log.Warn().Err(err).Msg("views: quote homes")
		return
	}
	for _, r := range rows {
		q := r.GetReplyTo()
		if q == nil {
			continue
		}
		if _, id, ok := SplitToken(q.GetMessageId()); ok && homes[id] != "" {
			q.SetMessageId(MessageToken(homes[id], id))
		}
	}
}

// message is m as a row. ids and avatars land at finish.
func (c *rc) message(ch chatCtx, m model.Message) *v2.MessageRow {
	row, _ := c.build(ch, m)
	return row
}

// build is m as a row, and its one line as the chat list says it.
func (c *rc) build(ch chatCtx, m model.Message) (*v2.MessageRow, string) {
	row := &v2.MessageRow{}
	row.SetId(Token(m))
	c.wait(ch.key, func(id, _ string) { row.SetChatId(id) })
	row.SetFromMe(m.FromMe)
	row.SetTMs(m.T)
	if m.System != nil {
		c.system(row, m)
		return row, row.GetFallback()
	}
	if m.FromMe {
		row.SetSender(c.self())
	} else {
		row.SetSender(c.at(m.Sender, m.T))
	}
	sm, raw := c.decode(m)
	f := m.Facts
	if f.Edit != nil && !f.Revoked {
		if raw != nil {
			if edited, ok := c.decoder().DecodeContent(c.ctx, c.event(m, raw), f.Edit); ok {
				sm.Text = edited.Text
				sm.Mentions = edited.Mentions
			}
		} else if t := f.Edit.GetConversation(); t != "" {
			sm.Text = t
		} else if t := f.Edit.GetExtendedTextMessage().GetText(); t != "" {
			sm.Text = t
		}
		row.SetEdited(true)
	}
	if f.Revoked {
		row.SetRevoked(true)
		if f.RevokeBy != "" && !sameSender(c.w, f.RevokeBy, m) {
			row.SetRevokedBy(c.now(f.RevokeBy))
		}
		row.SetFallback(store.RevokedPreview)
		row.SetTextBody(&v2.Text{})
		c.facts(row, m)
		return row, store.RevokedPreview
	}
	local(&sm, f.Local)
	if f.Live != nil {
		sm = shared(sm, *f.Live, time.Now().UnixMilli())
	}
	text, mentions := c.mentions(sm.Text, sm.Mentions)
	row.SetText(text)
	row.SetMentions(mentions)
	pf := store.PreviewFacts{Text: text, PayloadSummary: sm.PayloadSummary, MediaKind: sm.MediaKind,
		MediaMimeType: sm.MediaMimeType, MediaFileName: sm.MediaFileName, DurationSecs: sm.MediaDurationSecs}
	line := store.PreviewLine(pf)
	pf.KindWins = true
	row.SetFallback(store.PreviewLine(pf))
	row.SetForwarded(sm.IsForwarded)
	row.SetViewOnce(m.ViewOnce)
	if r := sm.ReplyTo; r.MessageID != "" {
		row.SetReplyTo(c.quote(m, r))
	}
	if m.FromMe && (sm.MediaKind == "" || sm.MediaKind == store.MediaKindImage) && !m.Queued {
		row.SetEditUntilMs(m.T + whatsmeow.EditWindow.Milliseconds())
	}
	c.body(row, ch, m, sm, raw)
	c.facts(row, m)
	return row, line
}

// decode is m's content as the decoder reads it, and the message it came
// from when it is a live one.
func (c *rc) decode(m model.Message) (store.Message, *waE2E.Message) {
	if m.Waiting {
		return placeholder(m), nil
	}
	raw, hm := m.Content()
	var sm store.Message
	ok := false
	switch {
	case hm != nil:
		sm, ok = c.decoder().DecodeHistory(c.ctx, jid(m.Chat), hm.GetMessage())
		raw = hm.GetMessage().GetMessage()
	case raw != nil:
		sm, ok = c.decoder().Decode(c.ctx, c.event(m, raw))
	}
	if !ok {
		return placeholder(m), raw
	}
	return sm, raw
}

// event is the whatsmeow event a live row was, rebuilt for the decoder.
func (c *rc) event(m model.Message, raw *waE2E.Message) *events.Message {
	sender := jid(m.Sender)
	if m.FromMe {
		sender = jid(c.w.SelfPN())
	}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat: jid(m.Chat), Sender: sender, SenderAlt: jid(m.SenderAlt),
			IsFromMe: m.FromMe, IsGroup: model.IsGroup(m.Chat),
		},
		ID:        m.ID,
		Timestamp: time.UnixMilli(m.T),
	}
	evt := &events.Message{Info: info, RawMessage: proto.Clone(raw).(*waE2E.Message)}
	return evt.UnwrapRaw()
}

// placeholder is a row the decoder had nothing for: still a row, never a
// hole.
func placeholder(m model.Message) store.Message {
	switch {
	case m.Waiting && m.Wait == "view_once", m.ViewOnce:
		return store.Message{MediaKind: store.MediaKindUnsupported, Text: "View once message"}
	case m.Waiting:
		p, _ := store.EncodePayload(store.MessagePayload{Waiting: &store.WaitingPayload{FirstSeen: m.T / 1000}})
		return store.Message{MediaKind: store.MediaKindWaiting, PayloadJSON: p}
	case m.Queued:
		return store.Message{MediaKind: store.MediaKindUnsupported, Text: "Message"}
	}
	return store.Message{MediaKind: store.MediaKindUnsupported, Text: "Unsupported message"}
}

// sameSender says the revoke came from whoever sent m.
func sameSender(w *model.World, by string, m model.Message) bool {
	if m.FromMe {
		return by == model.Me || w.IsSelf(by)
	}
	return w.Now(model.Norm(by)) == w.Now(model.Norm(m.Sender))
}

// facts lays on what other messages and app state said about m.
func (c *rc) facts(row *v2.MessageRow, m model.Message) {
	f := m.Facts
	row.SetStarred(f.Starred)
	row.SetKept(f.Kept)
	if f.Pinned {
		row.SetPinnedUntilMs(f.PinEnd)
	}
	if m.FromMe {
		row.SetStatus(msgStatus(m))
		if m.Queued && m.Out.Failed {
			row.SetError(m.Out.Error)
		}
	}
	if f.Revoked {
		return
	}
	// one person may have reacted under two addresses: newest wins
	newest := map[string]model.Reaction{}
	var order []string
	for _, r := range f.Reactions {
		k := r.Sender
		if k != model.Me {
			k = c.w.Now(model.Norm(k))
		}
		if old, ok := newest[k]; !ok {
			order = append(order, k)
			newest[k] = r
		} else if r.T > old.T {
			newest[k] = r
		}
	}
	var out []*v2.Reaction
	for _, k := range order {
		r := newest[k]
		if r.Emoji == "" {
			continue
		}
		out = append(out, v2.Reaction_builder{Emoji: r.Emoji, Sender: c.now(r.Sender), TMs: r.T}.Build())
	}
	row.SetReactions(out)
}

func msgStatus(m model.Message) v2.MessageStatus {
	if m.Queued {
		if m.Out.Failed {
			return v2.MessageStatus_MESSAGE_STATUS_FAILED
		}
		return v2.MessageStatus_MESSAGE_STATUS_PENDING
	}
	best := 0
	for _, r := range m.Facts.Receipts {
		switch r.Type {
		case "played":
			best = max(best, 4)
		case "read":
			best = max(best, 3)
		case "delivered":
			best = max(best, 2)
		}
	}
	switch best {
	case 4:
		return v2.MessageStatus_MESSAGE_STATUS_PLAYED
	case 3:
		return v2.MessageStatus_MESSAGE_STATUS_READ
	case 2:
		return v2.MessageStatus_MESSAGE_STATUS_DELIVERED
	}
	switch m.Status {
	case 1:
		return v2.MessageStatus_MESSAGE_STATUS_FAILED
	case 2:
		return v2.MessageStatus_MESSAGE_STATUS_PENDING
	case 4:
		return v2.MessageStatus_MESSAGE_STATUS_DELIVERED
	case 5:
		return v2.MessageStatus_MESSAGE_STATUS_READ
	case 6:
		return v2.MessageStatus_MESSAGE_STATUS_PLAYED
	}
	return v2.MessageStatus_MESSAGE_STATUS_SENT
}

// mentions is text with each @number reading @Name, and where each sits in
// code points.
func (c *rc) mentions(text string, ms []store.MessageMention) (string, []*v2.Mention) {
	if len(ms) == 0 || !strings.Contains(text, "@") {
		return text, nil
	}
	byUser := map[string]string{}
	for _, m := range ms {
		user, _, _ := strings.Cut(m.JID, "@")
		if user != "" {
			byUser[user] = m.JID
		}
	}
	var b strings.Builder
	var out []*v2.Mention
	n := 0
	for i := 0; i < len(text); {
		if text[i] != '@' {
			r, size := utf8.DecodeRuneInString(text[i:])
			b.WriteRune(r)
			n++
			i += size
			continue
		}
		j := i + 1
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		addr, ok := byUser[text[i+1:j]]
		if !ok || j == i+1 {
			b.WriteByte('@')
			n++
			i++
			continue
		}
		p := c.now(addr)
		name := strings.TrimPrefix(p.GetName(), "~")
		if name == "" {
			name = text[i+1 : j]
		}
		start := n
		b.WriteString("@" + name)
		n += 1 + utf8.RuneCountInString(name)
		out = append(out, v2.Mention_builder{Person: p, Start: uint32(start), End: uint32(n)}.Build())
		i = j
	}
	return b.String(), out
}

// quote is the message m answers, as far as m itself says.
func (c *rc) quote(m model.Message, r store.MessageReply) *v2.Quote {
	q := &v2.Quote{}
	// the decoder names it chat:id with the chat as it reads it; the row's own
	// address is a better first guess, homeQuotes settles it
	id := r.MessageID
	if i := strings.LastIndexByte(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	q.SetMessageId(MessageToken(m.Home, id))
	switch {
	case r.SenderID == "me":
		q.SetSender(c.self())
	case r.SenderID != "":
		q.SetSender(c.now(r.SenderID))
	}
	q.SetText(store.ReplyPreviewLine(r))
	return q
}

// local is what this daemon did for the row on its own: its file, a failed
// download, a voice note played, the full link preview picture, the group
// an invite points at.
func local(out *store.Message, l model.Local) {
	if l.File != "" {
		out.MediaLocalPath = l.File
		if l.W > 0 && l.H > 0 {
			out.MediaWidth, out.MediaHeight = l.W, l.H
		}
	}
	out.MediaDownloadError = l.DownloadError()
	out.MediaPlayed = out.MediaPlayed || l.Played
	if l.Poster != "" {
		out.MediaThumbnailLocalPath = l.Poster
	}
	if len(out.MediaWaveform) == 0 {
		out.MediaWaveform = l.Waveform
	}
	if l.Preview == "" && len(l.Invite) == 0 && l.InviteErr == "" {
		return
	}
	p := store.DecodePayload(out.PayloadJSON)
	if lp := p.LinkPreview; lp != nil && l.Preview != "" {
		lp.ThumbnailPath, lp.ThumbnailWidth, lp.ThumbnailHeight = l.Preview, int(l.PreviewW), int(l.PreviewH)
	}
	if g := p.GroupInvite; g != nil {
		var r store.GroupInvitePayload
		if len(l.Invite) > 0 && json.Unmarshal(l.Invite, &r) == nil {
			g.Subject, g.Topic, g.MemberCount, g.ResolvedAt, g.Joined = r.Subject, r.Topic, r.MemberCount, r.ResolvedAt, r.Joined
		}
		g.ResolveError = l.InviteErr
		out.PayloadSummary = g.DisplayName()
	}
	if enc, err := store.EncodePayload(p); err == nil {
		out.PayloadJSON = enc
	}
}

// shared is a live location row at its share's newest point.
func shared(out store.Message, l model.Live, now int64) store.Message {
	p := store.DecodePayload(out.PayloadJSON)
	if p.Location == nil {
		p.Location = &store.LocationPayload{}
	}
	if l.Known {
		p.Location.Latitude, p.Location.Longitude, p.Location.AccuracyMeters = l.Lat, l.Lng, l.Acc
	}
	p.Location.Live = true
	p.LiveShare = &store.LiveSharePayload{
		Active:         l.Active(now),
		StartedAt:      l.Start / 1000,
		ExpiresAt:      l.Ends() / 1000,
		UpdatedAt:      l.Updated / 1000,
		SpeedMPS:       l.Speed,
		HeadingDegrees: l.Heading,
		PointCount:     l.Points,
	}
	if enc, err := store.EncodePayload(p); err == nil {
		out.PayloadJSON = enc
		out.PayloadSummary = whatsapp.LocationSummary(p.Location)
	}
	return out
}

// system is a row the chat wrote about itself, names as of now.
func (c *rc) system(row *v2.MessageRow, m model.Message) {
	s := m.System
	who := func(j string) store.SystemParticipant {
		if c.w.IsSelf(j) {
			return store.SystemParticipant{JID: j, Self: true}
		}
		name, _ := c.w.Name(c.w.Key(model.Norm(j), m.T))
		return store.SystemParticipant{JID: j, Name: strings.TrimPrefix(name, "~")}
	}
	p := store.SystemPayload{Type: s.Type, Value: s.Value, Detail: s.Detail, On: s.On, Seconds: s.Seconds}
	if s.Actor != "" {
		a := who(s.Actor)
		p.Actor = &a
	}
	for _, j := range s.Who {
		p.Participants = append(p.Participants, who(j))
	}
	p.AboutSelf = c.w.Loud(s) || s.Type == store.SystemTypeIdentityChange
	sm := whatsapp.SystemMessage(jid(m.Chat), p, time.UnixMilli(m.T))
	sys := v2.System_builder{
		Type:          systemType(s.Type),
		Text:          sm.PayloadSummary,
		Value:         s.Value,
		On:            s.On,
		EphemeralSecs: s.Seconds,
		AboutSelf:     p.AboutSelf,
	}.Build()
	if s.Actor != "" {
		sys.SetActor(c.at(s.Actor, m.T))
		row.SetSender(sys.GetActor())
	}
	var names []*v2.Person
	for i, j := range s.Who {
		if i >= systemNames {
			sys.SetOverflow(uint32(len(s.Who) - systemNames))
			break
		}
		names = append(names, c.at(j, m.T))
	}
	sys.SetNames(names)
	row.SetText(sm.PayloadSummary)
	row.SetFallback(sm.PayloadSummary)
	row.SetSystem(sys)
}

// systemNames is how many names a system row carries, as many as its
// sentence names before it counts
const systemNames = 3

func systemType(t string) v2.SystemType {
	switch t {
	case store.SystemTypeGroupJoin:
		return v2.SystemType_SYSTEM_TYPE_GROUP_JOIN
	case store.SystemTypeGroupLeave:
		return v2.SystemType_SYSTEM_TYPE_GROUP_LEAVE
	case store.SystemTypeGroupPromote:
		return v2.SystemType_SYSTEM_TYPE_GROUP_PROMOTE
	case store.SystemTypeGroupDemote:
		return v2.SystemType_SYSTEM_TYPE_GROUP_DEMOTE
	case store.SystemTypeGroupName:
		return v2.SystemType_SYSTEM_TYPE_GROUP_NAME
	case store.SystemTypeGroupTopic:
		return v2.SystemType_SYSTEM_TYPE_GROUP_TOPIC
	case store.SystemTypeGroupPhoto:
		return v2.SystemType_SYSTEM_TYPE_GROUP_PHOTO
	case store.SystemTypeGroupLocked:
		return v2.SystemType_SYSTEM_TYPE_GROUP_LOCKED
	case store.SystemTypeGroupAnnounce:
		return v2.SystemType_SYSTEM_TYPE_GROUP_ANNOUNCE
	case store.SystemTypeGroupApproval:
		return v2.SystemType_SYSTEM_TYPE_GROUP_APPROVAL
	case store.SystemTypeGroupInviteLink:
		return v2.SystemType_SYSTEM_TYPE_GROUP_INVITE_LINK
	case store.SystemTypeGroupLink:
		return v2.SystemType_SYSTEM_TYPE_GROUP_LINK
	case store.SystemTypeGroupUnlink:
		return v2.SystemType_SYSTEM_TYPE_GROUP_UNLINK
	case store.SystemTypeGroupDelete:
		return v2.SystemType_SYSTEM_TYPE_GROUP_DELETE
	case store.SystemTypeEphemeral:
		return v2.SystemType_SYSTEM_TYPE_EPHEMERAL
	case store.SystemTypeIdentityChange:
		return v2.SystemType_SYSTEM_TYPE_IDENTITY_CHANGE
	}
	return v2.SystemType_SYSTEM_TYPE_UNSPECIFIED
}

// pollOf is the poll a message opens, in whichever version it came.
func pollOf(m *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{
		m.GetPollCreationMessage(), m.GetPollCreationMessageV2(), m.GetPollCreationMessageV3(),
		m.GetPollCreationMessageV5(), m.GetPollCreationMessageV6(),
	} {
		if p != nil {
			return p
		}
	}
	return nil
}

// tally counts each voter's newest vote against the options by hash.
func (c *rc) tally(out *v2.Poll, poll *waE2E.PollCreationMessage, votes []model.Sealed) {
	byHash := map[[32]byte]int{}
	opts := make([]*v2.PollOption, len(poll.GetOptions()))
	for i, o := range poll.GetOptions() {
		byHash[sha256.Sum256([]byte(o.GetOptionName()))] = i
		opts[i] = v2.PollOption_builder{Index: uint32(i), Name: o.GetOptionName()}.Build()
	}
	newest := map[string]model.Sealed{}
	for _, v := range votes {
		k := v.Sender
		if k != model.Me {
			k = c.w.Now(model.Norm(k))
		}
		if old, ok := newest[k]; !ok || v.T > old.T {
			newest[k] = v
		}
	}
	voters := make([]string, 0, len(newest))
	for s := range newest {
		voters = append(voters, s)
	}
	sort.Slice(voters, func(i, j int) bool { return newest[voters[i]].T < newest[voters[j]].T })
	n := 0
	for _, s := range voters {
		v := newest[s]
		picked := model.Vote(v.Plain)
		if len(picked) == 0 {
			continue
		}
		n++
		me := s == model.Me || c.w.IsSelf(s)
		if me {
			out.SetSelfVoted(true)
		}
		for _, h := range picked {
			if len(h) != 32 {
				continue
			}
			i, ok := byHash[[32]byte(h)]
			if !ok {
				continue
			}
			o := opts[i]
			o.SetVoters(append(o.GetVoters(), v2.Voter_builder{Person: c.now(s), TMs: v.T}.Build()))
			if me {
				o.SetSelfVoted(true)
			}
		}
	}
	out.SetOptions(opts)
	out.SetVoters(uint32(n))
}

// rsvps is where an event's answers stand, each person's newest.
func (c *rc) rsvps(out *v2.ScheduledEvent, responses []model.Sealed) {
	newest := map[string]model.Sealed{}
	for _, r := range responses {
		k := r.Sender
		if k != model.Me {
			k = c.w.Now(model.Norm(k))
		}
		if old, ok := newest[k]; !ok || r.T > old.T {
			newest[k] = r
		}
	}
	senders := make([]string, 0, len(newest))
	for s := range newest {
		senders = append(senders, s)
	}
	sort.Slice(senders, func(i, j int) bool { return newest[senders[i]].T < newest[senders[j]].T })
	var list []*v2.Responder
	going := 0
	for _, s := range senders {
		var rm waE2E.EventResponseMessage
		if proto.Unmarshal(newest[s].Plain, &rm) != nil {
			continue
		}
		resp := v2.Rsvp_RSVP_MAYBE
		switch rm.GetResponse() {
		case waE2E.EventResponseMessage_GOING:
			resp = v2.Rsvp_RSVP_GOING
			going += 1 + int(rm.GetExtraGuestCount())
		case waE2E.EventResponseMessage_NOT_GOING:
			resp = v2.Rsvp_RSVP_NOT_GOING
		}
		if s == model.Me || c.w.IsSelf(s) {
			out.SetSelfResponse(resp)
			out.SetSelfGuests(uint32(rm.GetExtraGuestCount()))
		}
		list = append(list, v2.Responder_builder{Person: c.now(s), Response: resp,
			ExtraGuests: uint32(rm.GetExtraGuestCount()), TMs: newest[s].T}.Build())
	}
	out.SetResponders(list)
	out.SetGoing(uint32(going))
}
