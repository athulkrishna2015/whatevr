package ingest

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waServerSync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// headVersion is the layout version of every head below. bump it with any
// change to one, and teach the folds to read both.
//
// 2: MessageHead.Sent. a v1 head reads as not sent, which it was not.
const headVersion = 2

var marshal = proto.MarshalOptions{Deterministic: true}

// inputsFor is what one event puts in the log. nil for events the log has no
// use for: connection state, presence, typing.
func inputsFor(evt any) ([]core.Input, error) {
	switch evt := evt.(type) {
	case *events.Message:
		return messageInputs(evt)
	case *events.Receipt:
		return one(core.KindReceipt, core.ReceiptHead{
			Source:        source(evt.MessageSource),
			IDs:           evt.MessageIDs,
			Type:          string(evt.Type),
			T:             unix(evt.Timestamp),
			MessageSender: jid(evt.MessageSender),
		}, nil)
	case *events.UndecryptableMessage:
		return one(core.KindUndecryptable, core.UndecryptableHead{
			Source:          source(evt.Info.MessageSource),
			ID:              evt.Info.ID,
			T:               unix(evt.Info.Timestamp),
			Unavailable:     evt.IsUnavailable,
			UnavailableType: string(evt.UnavailableType),
			FailMode:        string(evt.DecryptFailMode),
		}, nil)
	case *events.PushName:
		h := core.PushNameHead{JID: jid(evt.JID), JIDAlt: jid(evt.JIDAlt), Old: evt.OldPushName, New: evt.NewPushName}
		if evt.Message != nil {
			h.ID, h.T = evt.Message.ID, unix(evt.Message.Timestamp)
		}
		return one(core.KindPushName, h, nil)
	case *events.BusinessName:
		h := core.BusinessNameHead{JID: jid(evt.JID), Old: evt.OldBusinessName, New: evt.NewBusinessName}
		if evt.Message != nil {
			h.ID, h.T = evt.Message.ID, unix(evt.Message.Timestamp)
		}
		return one(core.KindBusinessName, h, nil)
	case *events.IdentityChange:
		// no time on one found while sending: the stamp stands in, which
		// replay can repeat
		return one(core.KindIdentityChange, core.IdentityChangeHead{JID: jid(evt.JID), Implicit: evt.Implicit, T: unix(evt.Timestamp)}, nil)
	case *events.Picture:
		return one(core.KindPicture, core.PictureHead{
			JID: jid(evt.JID), Author: jid(evt.Author), T: unix(evt.Timestamp), Remove: evt.Remove, PictureID: evt.PictureID,
		}, nil)
	case *events.Blocklist:
		h := core.BlocklistHead{Action: string(evt.Action), DHash: evt.DHash, PrevDHash: evt.PrevDHash}
		for _, c := range evt.Changes {
			h.Changes = append(h.Changes, core.BlocklistChange{JID: jid(c.JID), Action: string(c.Action)})
		}
		return one(core.KindBlocklist, h, nil)
	case *events.PrivacySettings:
		return one(core.KindPrivacy, privacyHead(evt), nil)
	case *events.UserStatusMute:
		if evt.JID.IsEmpty() || evt.Action == nil {
			return nil, nil
		}
		return one(core.KindStatusMute, core.StatusMuteHead{
			Sender: jid(evt.JID), Muted: evt.Action.GetMuted(),
		}, nil)
	case *events.GroupInfo:
		return one(core.KindGroupInfo, groupChange(evt), nil)
	case *events.JoinedGroup:
		h := fullGroup(&evt.GroupInfo, evt.GroupCreated)
		if evt.Sender != nil {
			h.By = jid(*evt.Sender)
		}
		if evt.SenderPN != nil {
			h.ByPN = jid(*evt.SenderPN)
		}
		h.JoinReason = evt.Reason
		// when we came in is not given: the fold takes the receive time
		h.T = 0
		return one(core.KindGroupInfo, h, nil)
	case *events.CallOffer:
		return one(core.KindCall, callHead(evt.BasicCallMeta, "offer"), nil)
	case *events.CallOfferNotice:
		h := callHead(evt.BasicCallMeta, "offer")
		h.Video = evt.Media == "video"
		return one(core.KindCall, h, nil)
	case *events.CallAccept:
		return one(core.KindCall, callHead(evt.BasicCallMeta, "accept"), nil)
	case *events.CallReject:
		return one(core.KindCall, callHead(evt.BasicCallMeta, "reject"), nil)
	case *events.CallTerminate:
		h := callHead(evt.BasicCallMeta, "terminate")
		h.Reason = evt.Reason
		return one(core.KindCall, h, nil)
	case *events.AppStateSyncComplete:
		return one(core.KindSyncState, core.SyncStateHead{Domain: "app_state:" + string(evt.Name), Version: evt.Version,
			State: core.SyncComplete, Recovery: evt.Recovery}, nil)
	case *events.AppStateSyncError:
		msg := ""
		if evt.Error != nil {
			msg = evt.Error.Error()
		}
		return one(core.KindSyncState, core.SyncStateHead{Domain: "app_state:" + string(evt.Name), State: core.SyncError, Error: msg}, nil)
	case *events.NewsletterJoin:
		return one(core.KindNewsletter, core.NewsletterHead{JID: jid(evt.ID), Event: "join", Name: evt.ThreadMeta.Name.Text}, nil)
	case *events.NewsletterLeave:
		return one(core.KindNewsletter, core.NewsletterHead{JID: jid(evt.ID), Event: "leave", Role: string(evt.Role)}, nil)
	case *events.NewsletterMuteChange:
		return one(core.KindNewsletter, core.NewsletterHead{JID: jid(evt.ID), Event: "mute", Mute: string(evt.Mute)}, nil)
	}
	return nil, nil
}

func messageInputs(evt *events.Message) ([]core.Input, error) {
	info := evt.Info
	if n := evt.Message.GetProtocolMessage().GetHistorySyncNotification(); n != nil {
		body, err := marshal.Marshal(n)
		if err != nil {
			return nil, err
		}
		return one(core.KindHistoryNotification, core.HistoryNotificationHead{
			ID:         info.ID,
			T:          unix(info.Timestamp),
			SyncType:   n.GetSyncType().String(),
			ChunkOrder: n.GetChunkOrder(),
			Progress:   n.GetProgress(),
			SessionID:  n.GetPeerDataRequestSessionID(),
			OriginalID: n.GetOriginalMessageID(),
		}, body)
	}
	h := core.MessageHead{
		Source:       source(info.MessageSource),
		ID:           info.ID,
		ServerID:     int(info.ServerID),
		T:            unix(info.Timestamp),
		Type:         info.Type,
		PushName:     info.PushName,
		Category:     info.Category,
		Edit:         string(info.Edit),
		MediaType:    info.MediaType,
		Retry:        evt.RetryCount,
		ThreadID:     info.MsgMetaInfo.ThreadMessageID,
		ThreadSender: jid(info.MsgMetaInfo.ThreadMessageSenderJID),
	}
	if info.DeviceSentMeta != nil {
		h.DeviceSentTo = info.DeviceSentMeta.DestinationJID
	}
	if v := info.VerifiedName; v != nil {
		h.VerifiedName, h.VerifiedLevel = v.Details.GetVerifiedName(), v.VerifiedLevel
	}
	body := evt.Plaintext
	if body != nil {
		h.Exact = true
	} else {
		var err error
		if body, err = marshal.Marshal(evt.RawMessage); err != nil {
			return nil, err
		}
	}
	return one(core.KindMessage, h, body)
}

// appStateInputs is one input per mutation and a sync_state saying where the
// collection got to, appended together.
func appStateInputs(name appstate.WAPatchName, version uint64, mutations []appstate.Mutation, snapshot bool) ([]core.Input, error) {
	ins := make([]core.Input, 0, len(mutations)+1)
	for _, m := range mutations {
		op := "set"
		if m.Operation == waServerSync.SyncdMutation_REMOVE {
			op = "remove"
		}
		body, err := marshal.Marshal(m.Action)
		if err != nil {
			return nil, err
		}
		in, err := input(core.KindAppState, core.AppStateHead{
			Collection:    string(name),
			Version:       m.PatchVersion,
			Index:         m.Index,
			Op:            op,
			ActionVersion: m.Version,
			Snapshot:      snapshot,
		}, body)
		if err != nil {
			return nil, err
		}
		ins = append(ins, in)
	}
	in, err := input(core.KindSyncState, core.SyncStateHead{
		Domain: "app_state:" + string(name), Version: version, Snapshot: snapshot, Count: len(mutations),
	}, nil)
	if err != nil {
		return nil, err
	}
	return append(ins, in), nil
}

func privacyHead(evt *events.PrivacySettings) core.PrivacyHead {
	s := evt.NewSettings
	h := core.PrivacyHead{Settings: map[string]string{}}
	for _, c := range []struct {
		name    string
		value   types.PrivacySetting
		changed bool
	}{
		{"groupadd", s.GroupAdd, evt.GroupAddChanged},
		{"last", s.LastSeen, evt.LastSeenChanged},
		{"status", s.Status, evt.StatusChanged},
		{"profile", s.Profile, evt.ProfileChanged},
		{"readreceipts", s.ReadReceipts, evt.ReadReceiptsChanged},
		{"online", s.Online, evt.OnlineChanged},
		{"calladd", s.CallAdd, evt.CallAddChanged},
		{"messages", s.Messages, evt.MessagesChanged},
		{"defense", s.Defense, evt.DefenseChanged},
		{"stickers", s.Stickers, evt.StickersChanged},
	} {
		if c.value != "" {
			h.Settings[c.name] = string(c.value)
		}
		if c.changed {
			h.Changed = append(h.Changed, c.name)
		}
	}
	sort.Strings(h.Changed)
	return h
}

func one(kind string, head any, body []byte) ([]core.Input, error) {
	in, err := input(kind, head, body)
	if err != nil {
		return nil, err
	}
	return []core.Input{in}, nil
}

func input(kind string, head any, body []byte) (core.Input, error) {
	raw, err := json.Marshal(head)
	if err != nil {
		return core.Input{}, fmt.Errorf("%s head: %w", kind, err)
	}
	return core.Input{Kind: kind, V: headVersion, Head: raw, Body: body}, nil
}

func source(s types.MessageSource) core.Source {
	return core.Source{
		Chat:           jid(s.Chat),
		Sender:         jid(s.Sender),
		SenderAlt:      jid(s.SenderAlt),
		RecipientAlt:   jid(s.RecipientAlt),
		FromMe:         s.IsFromMe,
		Group:          s.IsGroup,
		Addressing:     string(s.AddressingMode),
		BroadcastOwner: jid(s.BroadcastListOwner),
	}
}

func jid(j types.JID) string {
	if j.IsEmpty() {
		return ""
	}
	return j.String()
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// sentInput is a message this device sent, once the server took it.
func sentInput(own types.JID, to types.JID, msg *waE2E.Message, resp whatsmeow.SendResponse) (core.Input, error) {
	body, err := marshal.Marshal(msg)
	if err != nil {
		return core.Input{}, err
	}
	return input(core.KindMessage, core.MessageHead{
		Source:   core.Source{Chat: jid(to.ToNonAD()), Sender: jid(own), FromMe: true, Group: to.Server == types.GroupServer},
		ID:       resp.ID,
		ServerID: int(resp.ServerID),
		T:        unix(resp.Timestamp),
		Sent:     true,
	}, body)
}
