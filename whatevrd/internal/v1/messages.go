package v1

import (
	"context"
	"crypto/sha256"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/model"
	"whatevrd/internal/store"
	"whatevrd/internal/whatsapp"
)

// message is one model row as v1 shows it.
func (a *Adapter) message(ctx context.Context, w *model.World, c model.Chat, m model.Message) store.Message {
	out, over := a.build(ctx, w, c, m)
	if over {
		a.overlay(ctx, []*store.Message{&out})
	}
	return out
}

// build is message without the old store's overlay, and whether the row
// takes one.
func (a *Adapter) build(ctx context.Context, w *model.World, c model.Chat, m model.Message) (store.Message, bool) {
	chatID := ChatID(w, c.Key)
	if m.Queued && len(m.Body) == 0 {
		return a.queued(ctx, chatID, m), false
	}
	if m.System != nil {
		return systemRow(w, chatID, m), false
	}
	var out store.Message
	ok := false
	raw, hm := m.Content()
	switch {
	case m.Waiting:
	case hm != nil:
		out, ok = a.decoder(w).DecodeHistory(ctx, jid(m.Chat), hm.GetMessage())
	case raw != nil:
		out, ok = a.decoder(w).Decode(ctx, a.event(w, m, raw))
	}
	if !ok {
		out = placeholder(m)
	}
	out.ID = MessageID(chatID, m.ID)
	out.ChatID = chatID
	out.TimestampUnix = m.T / 1000
	out.SortMS = m.T
	if m.FromMe {
		out.Direction = store.DirectionOutgoing
		out.SenderID = "me"
		out.SenderName = ""
		out.Status = status(m)
	} else {
		out.Direction = store.DirectionIncoming
		if out.MediaKind != store.MediaKindSystem || out.SenderID == "" {
			out.SenderID = m.Sender
		}
		if name, _ := w.Name(w.Key(m.Sender, m.T)); name != "" {
			out.SenderName = name
		}
	}
	f := m.Facts
	if f.Edit != nil && !f.Revoked && raw != nil {
		if edited, ok := a.decoder(w).DecodeContent(ctx, a.event(w, m, raw), f.Edit); ok {
			out.Text = edited.Text
		}
		out.IsEdited = true
	} else if f.Edit != nil && !f.Revoked {
		if t := f.Edit.GetConversation(); t != "" {
			out.Text = t
		} else if t := f.Edit.GetExtendedTextMessage().GetText(); t != "" {
			out.Text = t
		}
		out.IsEdited = true
	}
	if f.Revoked {
		out = revoked(out)
	} else if f.Live != nil {
		out = shared(out, *f.Live, time.Now().UnixMilli())
	}
	out.IsStarred = f.Starred
	out.IsKept = f.Kept
	if f.Pinned {
		out.PinnedAt, out.PinnedUntil = f.PinT/1000, f.PinEnd/1000
	}
	// one person may have reacted under two addresses: newest wins
	newest := map[string]model.Reaction{}
	var order []string
	for _, r := range f.Reactions {
		if f.Revoked {
			break
		}
		k := r.Sender
		if k != model.Me {
			k = w.Now(k)
		}
		if old, ok := newest[k]; !ok {
			order = append(order, k)
			newest[k] = r
		} else if r.T > old.T {
			newest[k] = r
		}
	}
	for _, k := range order {
		r := newest[k]
		fromMe := r.Sender == model.Me
		name := ""
		if !fromMe {
			name, _ = w.Name(w.Now(r.Sender))
		}
		sender := r.Sender
		if fromMe {
			sender = "me"
		}
		out.Reactions = append(out.Reactions, store.Reaction{Emoji: r.Emoji, SenderID: sender, SenderName: name, TimestampUnix: r.T / 1000, FromMe: fromMe})
	}
	if raw != nil && !f.Revoked {
		if poll := pollOf(model.Unwrap(raw).Msg); poll != nil {
			out.Poll = tally(w, poll, f.Votes)
		}
		if model.Unwrap(raw).Msg.GetEventMessage() != nil {
			out.Event = rsvps(w, f.Events)
		}
	}
	if out.MediaKind == store.MediaKindAlbum {
		out.Album = a.album(ctx, w, c, m)
	}
	if m.Queued {
		return outgoing(out, m), false
	}
	return out, true
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

// revoked is what a message deleted for everyone leaves behind: who, when
// and where, none of what it said or quoted
func revoked(m store.Message) store.Message {
	return store.Message{ID: m.ID, ChatID: m.ChatID, SenderID: m.SenderID, SenderName: m.SenderName,
		SenderAvatarLocalPath: m.SenderAvatarLocalPath, TimestampUnix: m.TimestampUnix, SortMS: m.SortMS,
		Direction: m.Direction, IsRead: m.IsRead, Status: m.Status, AlbumParentID: m.AlbumParentID,
		AlbumIndex: m.AlbumIndex, IsForwarded: m.IsForwarded, IsRevoked: true}
}

// queued is a send the old core queued with no body: what it says lives in
// the old store, which built it, until the send logs it here.
func (a *Adapter) queued(ctx context.Context, chatID string, m model.Message) store.Message {
	var out store.Message
	if a.old != nil {
		if old, err := a.old.GetMessage(ctx, m.Chat+":"+m.ID); err == nil {
			out = old
		}
	}
	out.ID, out.ChatID = MessageID(chatID, m.ID), chatID
	out.Direction, out.SenderID, out.SenderName = store.DirectionOutgoing, "me", ""
	out.TimestampUnix, out.SortMS = m.T/1000, m.T
	return outgoing(out, m)
}

// outgoing is a row as the outbox has it: owed or given up on, and the file
// that will go when the body has no upload in it yet.
func outgoing(out store.Message, m model.Message) store.Message {
	out.Status = store.StatusPending
	if m.Out.Failed {
		out.Status = store.StatusFailed
	}
	out.SendAttempts = int32(m.Out.Attempts)
	out.LastSendError = m.Out.Error
	if m.Out.File != "" {
		out.MediaLocalPath = m.Out.File
	}
	return out
}

// event is the whatsmeow event a live row was, rebuilt for the decoders.
func (a *Adapter) event(w *model.World, m model.Message, raw *waE2E.Message) *events.Message {
	sender := jid(m.Sender)
	if m.FromMe {
		sender = jid(w.SelfPN())
	}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat: jid(m.Chat), Sender: sender, SenderAlt: jid(m.SenderAlt),
			IsFromMe: m.FromMe, IsGroup: model.IsGroup(m.Chat),
		},
		ID:        m.ID,
		Timestamp: ms(m.T),
	}
	evt := &events.Message{Info: info, RawMessage: proto.Clone(raw).(*waE2E.Message)}
	return evt.UnwrapRaw()
}

// placeholder is a row the decoders had nothing for: still a row, never a
// hole.
func placeholder(m model.Message) store.Message {
	switch {
	case m.Waiting && m.Wait == "view_once":
		return store.Message{MediaKind: store.MediaKindUnsupported, Text: "View once message"}
	case m.Waiting && m.Wait != "":
		return store.Message{MediaKind: store.MediaKindUnsupported, Text: "Message unavailable"}
	case m.Waiting:
		payload, _ := store.EncodePayload(store.MessagePayload{Waiting: &store.WaitingPayload{FirstSeen: m.T / 1000}})
		return store.Message{MediaKind: store.MediaKindWaiting, PayloadJSON: payload}
	case m.ViewOnce:
		return store.Message{MediaKind: store.MediaKindUnsupported, Text: "View once message"}
	}
	return store.Message{MediaKind: store.MediaKindUnsupported, Text: "Unsupported message"}
}

func status(m model.Message) string {
	best := 0
	for _, r := range m.Facts.Receipts {
		switch r.Type {
		case "read", "played":
			best = max(best, 3)
		case "delivered":
			best = max(best, 2)
		}
	}
	switch {
	case best == 3:
		return store.StatusRead
	case best == 2:
		return store.StatusDelivered
	}
	switch m.Status {
	case 1:
		return store.StatusFailed
	case 2:
		return store.StatusPending
	case 4:
		return store.StatusDelivered
	case 5, 6:
		return store.StatusRead
	}
	return store.StatusSent
}

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
func tally(w *model.World, poll *waE2E.PollCreationMessage, votes []model.Sealed) *store.PollState {
	st := &store.PollState{}
	byHash := map[[32]byte]int{}
	for i, o := range poll.GetOptions() {
		h := sha256.Sum256([]byte(o.GetOptionName()))
		byHash[h] = i
		st.Options = append(st.Options, store.PollOption{Index: i, Name: o.GetOptionName(), SHA256: h[:]})
	}
	newest := map[string]model.Sealed{}
	for _, v := range votes {
		k := v.Sender
		if k != model.Me {
			k = w.Now(k)
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
	for _, s := range voters {
		v := newest[s]
		picked := model.Vote(v.Plain)
		if len(picked) == 0 {
			continue
		}
		st.TotalVoters++
		fromMe := s == model.Me
		name := ""
		if !fromMe {
			name, _ = w.Name(w.Now(s))
		}
		for _, h := range picked {
			if len(h) != 32 {
				continue
			}
			if i, ok := byHash[[32]byte(h)]; ok {
				st.Options[i].Voters = append(st.Options[i].Voters, store.PollVoter{JID: s, DisplayName: name, VotedAtUnix: v.T / 1000, FromMe: fromMe})
			}
		}
	}
	return st
}

func rsvps(w *model.World, responses []model.Sealed) *store.EventState {
	st := &store.EventState{}
	newest := map[string]model.Sealed{}
	for _, r := range responses {
		if old, ok := newest[r.Sender]; !ok || r.T > old.T {
			newest[r.Sender] = r
		}
	}
	senders := make([]string, 0, len(newest))
	for s := range newest {
		senders = append(senders, s)
	}
	sort.Slice(senders, func(i, j int) bool { return newest[senders[i]].T < newest[senders[j]].T })
	for _, s := range senders {
		var rm waE2E.EventResponseMessage
		if proto.Unmarshal(newest[s].Plain, &rm) != nil {
			continue
		}
		resp := store.EventResponseMaybe
		switch rm.GetResponse() {
		case waE2E.EventResponseMessage_GOING:
			resp = store.EventResponseGoing
		case waE2E.EventResponseMessage_NOT_GOING:
			resp = store.EventResponseNotGoing
		}
		if s == model.Me {
			st.SelfResponse, st.SelfGuests = resp, int(rm.GetExtraGuestCount())
		}
		name := ""
		if s != model.Me {
			name, _ = w.Name(w.Now(s))
		}
		st.Responders = append(st.Responders, store.EventResponder{JID: s, DisplayName: name, Response: resp,
			ExtraGuests: int(rm.GetExtraGuestCount()), RespondedAtUnix: newest[s].T / 1000})
	}
	return st
}

func (a *Adapter) album(ctx context.Context, w *model.World, c model.Chat, m model.Message) []store.Message {
	kids, err := a.r.AlbumChildren(ctx, m.Chat, m.ID)
	if err != nil {
		return nil
	}
	out := make([]store.Message, 0, len(kids))
	for _, k := range kids {
		out = append(out, a.message(ctx, w, c, k))
	}
	return out
}

// overlay takes what only the old store knows about rows: where media was
// downloaded to, whether that failed, the sender's avatar, and where a live
// share has got to. one query for all of them.
func (a *Adapter) overlay(ctx context.Context, ms []*store.Message) {
	if a.old == nil || len(ms) == 0 {
		return
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	olds, err := a.old.MessageOverlays(ctx, ids)
	if err != nil {
		return
	}
	for _, m := range ms {
		if old, ok := olds[m.ID]; ok {
			overlayOne(m, old)
		}
	}
}

func overlayOne(m *store.Message, old store.Message) {
	if old.MediaLocalPath != "" {
		m.MediaLocalPath = old.MediaLocalPath
	}
	if m.MediaThumbnailLocalPath == "" {
		m.MediaThumbnailLocalPath = old.MediaThumbnailLocalPath
	}
	if len(m.MediaWaveform) == 0 {
		m.MediaWaveform = old.MediaWaveform
	}
	if m.MediaKind == store.MediaKindLiveLocation && old.MediaKind == store.MediaKindLiveLocation {
		m.PayloadJSON, m.PayloadSummary = old.PayloadJSON, old.PayloadSummary
	}
	m.MediaDownloadError = old.MediaDownloadError
	m.MediaPlayed = old.MediaPlayed
	m.SenderAvatarLocalPath = old.SenderAvatarLocalPath
	m.SendAttempts, m.LastSendError, m.NextSendAttempt = old.SendAttempts, old.LastSendError, old.NextSendAttempt
	if m.Direction == store.DirectionOutgoing && (old.Status == store.StatusPending || old.Status == store.StatusFailed) && m.Status == store.StatusSent {
		m.Status = old.Status
	}
}

// systemRow is a row the chat wrote about itself, names as of now.
func systemRow(w *model.World, chatID string, m model.Message) store.Message {
	person := func(j string) store.SystemParticipant {
		if w.IsSelf(j) {
			return store.SystemParticipant{JID: j, Self: true}
		}
		name, _ := w.Name(w.Key(j, m.T))
		return store.SystemParticipant{JID: j, Name: strings.TrimPrefix(name, "~")}
	}
	s := m.System
	p := store.SystemPayload{Type: s.Type, Value: s.Value, Detail: s.Detail, On: s.On, Seconds: s.Seconds}
	if s.Actor != "" {
		actor := person(s.Actor)
		p.Actor = &actor
	}
	for _, j := range s.Who {
		p.Participants = append(p.Participants, person(j))
	}
	p.AboutSelf = w.Loud(s) || s.Type == store.SystemTypeIdentityChange
	out := whatsapp.SystemMessage(jid(m.Chat), p, time.UnixMilli(m.T))
	out.ID = MessageID(chatID, m.ID)
	out.ChatID = chatID
	out.SortMS = m.T
	return out
}
