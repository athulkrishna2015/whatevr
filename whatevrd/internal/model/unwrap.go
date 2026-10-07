package model

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Unwrapped is a message out of the envelopes it travels in, with what the
// envelopes said.
type Unwrapped struct {
	Msg *waE2E.Message
	// the raw message the body holds
	Raw          *waE2E.Message
	DeviceSentTo string
	Ephemeral    bool
	ViewOnce     bool
	Edit         bool
	DocCaption   bool
	BotInvoke    bool
}

const maxUnwrap = 8

// Unwrap peels whatever whatsmeow's UnwrapRaw peels, then the wrappers that
// hold an ordinary message whatsmeow leaves alone. associatedChildMessage is
// not one: it is the hd copy of a photo that already came on its own.
func Unwrap(raw *waE2E.Message) Unwrapped {
	u := Unwrapped{Msg: raw, Raw: raw}
	m := raw
	if d := m.GetDeviceSentMessage(); d.GetMessage() != nil {
		u.DeviceSentTo = d.GetDestinationJID()
		m = d.GetMessage()
	}
	if w := m.GetBotInvokeMessage().GetMessage(); w != nil {
		m, u.BotInvoke = w, true
	}
	if w := m.GetEphemeralMessage().GetMessage(); w != nil {
		m, u.Ephemeral = w, true
	}
	for _, w := range []*waE2E.Message{
		m.GetViewOnceMessage().GetMessage(),
		m.GetViewOnceMessageV2().GetMessage(),
		m.GetViewOnceMessageV2Extension().GetMessage(),
	} {
		if w != nil {
			m, u.ViewOnce = w, true
		}
	}
	if w := m.GetLottieStickerMessage().GetMessage(); w != nil {
		m = w
	}
	if w := m.GetDocumentWithCaptionMessage().GetMessage(); w != nil {
		m, u.DocCaption = w, true
	}
	if w := m.GetEditedMessage().GetMessage(); w != nil {
		m, u.Edit = w, true
	}
	for range maxUnwrap {
		var inner *waE2E.Message
		switch {
		case m.GetGroupMentionedMessage().GetMessage() != nil:
			inner = m.GetGroupMentionedMessage().GetMessage()
		case m.GetSpoilerMessage().GetMessage() != nil:
			inner = m.GetSpoilerMessage().GetMessage()
		case m.GetPollCreationMessageV4().GetMessage() != nil:
			inner = m.GetPollCreationMessageV4().GetMessage()
		case m.GetPollCreationOptionImageMessage().GetMessage() != nil:
			inner = m.GetPollCreationOptionImageMessage().GetMessage()
		case m.GetAudioStickerMessage().GetMessage() != nil:
			inner = m.GetAudioStickerMessage().GetMessage()
		case m.GetBotForwardedMessage().GetMessage() != nil:
			inner = m.GetBotForwardedMessage().GetMessage()
		case m.GetEphemeralMessage().GetMessage() != nil:
			inner = m.GetEphemeralMessage().GetMessage()
		}
		if inner == nil {
			break
		}
		if inner.MessageContextInfo == nil && m.MessageContextInfo != nil {
			inner.MessageContextInfo = m.MessageContextInfo
		}
		m = inner
	}
	if m != nil && m.MessageContextInfo == nil && raw.GetMessageContextInfo() != nil {
		m.MessageContextInfo = raw.MessageContextInfo
	}
	u.Msg = m
	return u
}

// silent fields never make a transcript row: session plumbing, and things
// that change a message rather than being one.
var silent = map[string]bool{
	"senderKeyDistributionMessage":               true,
	"fastRatchetKeySenderKeyDistributionMessage": true,
	"protocolMessage":                            true,
	"messageContextInfo":                         true,
	"stickerSyncRmrMessage":                      true,
	"placeholderMessage":                         true,
	"groupRootKeyShare":                          true,
	"rootSecretDistributeMessage":                true,
	"botPlatformRegistrationSuccessMessage":      true,
	"acp2SettingMessage":                         true,
	"associatedChildMessage":                     true,
	"reactionMessage":                            true,
	"encReactionMessage":                         true,
	"pollUpdateMessage":                          true,
	"pollAddOptionMessage":                       true,
	"pollCreationOptionImageMessage":             true,
	"encEventResponseMessage":                    true,
	"keepInChatMessage":                          true,
	"pinInChatMessage":                           true,
	"eventCoverImage":                            true,
	"statusLinkPreviewMetadata":                  true,
	"statusAddYours":                             true,
	"statusNotificationMessage":                  true,
	"newsletterAdminProfileMessage":              true,
	"newsletterAdminProfileStatusMessage":        true,
	"secretEncryptedMessage":                     true,
}

// FieldUnknown is a message made only of fields this build has no name for:
// a kind newer than the proto, shown as a placeholder.
const FieldUnknown = "?"

// Field is the message's first field that makes it a transcript row, "" for
// one that is only plumbing or a change to another message.
func Field(m *waE2E.Message) string {
	found, num, plumbing := "", protoreflect.FieldNumber(0), false
	// range order is unspecified, the lowest field number decides
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		name := string(fd.Name())
		if silent[name] {
			plumbing = plumbing || name != "messageContextInfo"
		} else if found == "" || fd.Number() < num {
			found, num = name, fd.Number()
		}
		return true
	})
	if found == "" && !plumbing && len(m.ProtoReflect().GetUnknown()) > 0 {
		return FieldUnknown
	}
	return found
}
