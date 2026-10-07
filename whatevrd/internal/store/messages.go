// package store is the decoded message: what the whatsapp decoder makes of
// one and the views turn into rows. no storage left in it, the model owns that.
package store

import (
	"time"
)

const (
	DirectionIncoming = "incoming"
	DirectionOutgoing = "outgoing"

	MediaKindImage   = "image"
	MediaKindSticker = "sticker"
	// MediaKindGIF is a VideoMessage with GifPlayback set: a muted, looping
	// clip that WhatsApp presents as a GIF even though the wire carries video.
	MediaKindGIF   = "gif"
	MediaKindVideo = "video"
	// MediaKindVideoNote is a PtvMessage: the round "instant video" recording.
	MediaKindVideoNote = "video_note"
	// MediaKindVoice is an AudioMessage with PTT set (a recorded voice note),
	// as opposed to MediaKindAudio, which is a shared audio file.
	MediaKindVoice    = "voice"
	MediaKindAudio    = "audio"
	MediaKindDocument = "document"

	// MediaKindLocation is a LocationMessage. The "media" it carries is the map
	// the daemon stitches for it, which is why it goes through the ordinary
	// download lifecycle rather than inventing a second one.
	MediaKindLocation = "location"
	// MediaKindLiveLocation is the opening LocationMessage of a live share. The
	// LiveLocationMessage updates that follow do not become rows of their own;
	// they move this one. See live_locations.go.
	MediaKindLiveLocation = "live_location"
	MediaKindContact      = "contact"
	MediaKindContacts     = "contacts"
	MediaKindPoll         = "poll"
	MediaKindGroupInvite  = "group_invite"
	MediaKindEvent        = "event"
	// MediaKindAlbum is the AlbumMessage header. Its children are ordinary rows
	// carrying album_parent_id, hidden from the transcript and delivered inside
	// this row instead.
	MediaKindAlbum = "album"
	// MediaKindInteractive covers the business message family: buttons, lists,
	// hydrated templates and interactive messages, which differ in wire shape
	// but render as the same card.
	MediaKindInteractive = "interactive"
	MediaKindProduct     = "product"
	MediaKindOrder       = "order"
	MediaKindPayment     = "payment"
	MediaKindStickerPack = "sticker_pack"
	MediaKindCallLog     = "call_log"
	// MediaKindSystem is a synthetic row for something that happened to the
	// chat rather than in it: a join, a subject change, a disappearing-timer
	// change, a security-code change. Never counted as unread.
	MediaKindSystem = "system"
	// MediaKindWaiting is a message we could not decrypt and have asked the
	// phone to resend. Unlike every other kind, this row is expected to be
	// replaced in place when the real message arrives under the same id.
	MediaKindWaiting = "waiting"
	// MediaKindUnsupported marks a real message whose payload whatevr cannot
	// render yet. the text carries a label for humans.
	MediaKindUnsupported = "unsupported"

	StatusPending   = "pending"
	StatusDelivered = "delivered"
	StatusRead      = "read"
	StatusFailed    = "failed"
	StatusSent      = "sent"
)

// MessageMention is one @-mention: the mentioned participant's JID plus the
// display name resolved when the message was ingested. The display name may be
// empty when the participant was unknown at ingest; the renderer then falls
// back to the JID's user-part, exactly as WhatsApp shows an unknown number.
type MessageMention struct {
	JID         string
	DisplayName string
}

type Message struct {
	ID                      string
	ChatID                  string
	SenderID                string
	SenderName              string
	SenderAvatarLocalPath   string
	Text                    string
	TimestampUnix           int64
	SortMS                  int64
	Direction               string
	IsRead                  bool
	Status                  string
	MediaKind               string
	MediaMimeType           string
	MediaLocalPath          string
	MediaThumbnailLocalPath string
	MediaWidth              int32
	MediaHeight             int32
	MediaAnimated           bool
	MediaDownloadError      string
	MediaPayload            []byte
	MediaCacheKey           string
	MediaDurationSecs       int32
	MediaSizeBytes          int64
	MediaFileName           string
	MediaPageCount          int32
	// MediaWaveform holds the 64 amplitude buckets (0-100) WhatsApp ships with
	// a voice note, or the ones the daemon derived after download when the
	// sender omitted them. Empty for every other kind.
	MediaWaveform []byte
	// MediaPlayed records that we already sent a played receipt for an inbound
	// voice note, so marking it played twice is a no-op.
	MediaPlayed bool
	// PayloadJSON is the kind-specific structured payload (a location's
	// coordinates, a vCard's parsed fields, a poll's settings, a system event's
	// participants). Opaque here; decoded by the protocol layer into the
	// message item's nested object for that kind.
	PayloadJSON string
	// PayloadSummary is the one-line detail the ingest derived from that
	// payload: a poll's question, a place name, "Ana and 12 others joined". It
	// is outside PayloadJSON so a preview never parses json.
	PayloadSummary string
	// AlbumParentID names the AlbumMessage this media belongs to. A row with a
	// parent that exists is hidden from the transcript and delivered inside the
	// album row instead.
	AlbumParentID string
	AlbumIndex    int32
	// IsKept records a KeepInChatMessage naming this row: a disappearing
	// message somebody asked to keep.
	IsKept          bool
	IsRevoked       bool
	IsForwarded     bool
	IsEdited        bool
	IsStarred       bool
	PinnedAt        int64
	PinnedUntil     int64
	SendAttempts    int32
	LastSendError   string
	NextSendAttempt int64
	ReplyTo         MessageReply
	Reactions       []Reaction
	// @-mentioned participants with names resolved at ingest. Empty for the
	// vast majority of messages.
	Mentions []MessageMention
	// Poll is the live tally, attached at read time for poll rows only. It is
	// joined rather than denormalized because a snapshot rewritten on every
	// vote is a snapshot that can be stale.
	Poll *PollState
	// Event is the RSVPs, attached at read time for event rows only, and joined
	// for the same reason a poll's tally is.
	Event *EventState
	// Album is the pictures this row groups, attached at read time for album
	// rows only. Each one is a whole message: the tiles are the children, not a
	// copy of them.
	Album []Message
	// StickerPack is what the local library currently knows about the pack a
	// sticker-pack row shares, attached at read time for the same reason a
	// poll's tally is: installing a pack from the picker must not leave a card
	// elsewhere in the transcript still offering to add it.
	StickerPack *StickerPackState
}

// StickerPackState is the library's answer about a shared pack. Known is false
// for a pack the daemon cannot find at all, which is the normal case for one
// somebody made on their own phone: there is nothing to install by id, and a
// card that offered anyway would be offering a button that fails.
type StickerPackState struct {
	Known     bool
	Installed bool
}

type MessageReply struct {
	MessageID     string
	SenderID      string
	SenderName    string
	Text          string
	MediaKind     string
	MediaMimeType string
	Direction     string
}

type MediaMessageInput struct {
	TextMessageInput
	MediaMimeType           string
	MediaKind               string
	MediaLocalPath          string
	MediaThumbnailLocalPath string
	MediaWidth              int32
	MediaHeight             int32
	MediaAnimated           bool
	MediaPayload            []byte
	MediaCacheKey           string
	MediaDurationSecs       int32
	MediaSizeBytes          int64
	MediaFileName           string
	MediaPageCount          int32
	MediaWaveform           []byte
	PayloadSummary          string
	AlbumParentID           string
	AlbumIndex              int32
}

type TextMessageInput struct {
	ID             string
	ChatID         string
	ChatName       string
	ChatNameSource string
	SenderID       string
	SenderName     string
	// SenderNameSource ranks the claim SenderName makes on the senders row, so
	// a push name never overwrites an address book name. See
	// SenderNameSourceContact and friends.
	SenderNameSource string
	Text             string
	Timestamp        time.Time
	Direction        string
	Status           string
	IsGroup          bool
	CountUnread      bool
	IsForwarded      bool
	ReplyTo          MessageReply
	Mentions         []MessageMention
	// PayloadJSON is the row's structured payload. It sits here rather than on
	// MediaMessageInput because a payload belongs to the message, not to its
	// media: a link preview rides an ordinary text row, whose kind stays
	// `text` because the text is still the message.
	PayloadJSON string
}
