package store

import (
	"encoding/json"
	"strings"
)

// The kind-specific payloads a message row can carry in its payload_json
// column. They live here, in the store, because the store writes them and the
// protocol layer reads them back out; the json tags are the wire shape and
// PROTOCOL.md is their contract.
//
// Anything static enough to be written once at ingest belongs here. State that
// keeps moving after the message lands (a poll's tally, a live share's trail,
// an event's RSVPs) gets a real table instead, because it has to be queried and
// updated rather than replaced wholesale.

// MessagePayload is the envelope stored in payload_json. Exactly one field is
// set, chosen by the row's media_kind, so decoding never has to guess.
type MessagePayload struct {
	Location    *LocationPayload    `json:"location,omitempty"`
	LiveShare   *LiveSharePayload   `json:"live,omitempty"`
	Contacts    *ContactsPayload    `json:"contacts,omitempty"`
	Poll        *PollPayload        `json:"poll,omitempty"`
	GroupInvite *GroupInvitePayload `json:"invite,omitempty"`
	Event       *EventPayload       `json:"event,omitempty"`
	Album       *AlbumPayload       `json:"album,omitempty"`
	LinkPreview *LinkPreviewPayload `json:"link_preview,omitempty"`
}

// LinkPreviewPayload is the card a sender's client built for a link in their
// message. It is the one payload that does not stand for the message: the text
// is still the message, and this describes something the text points at, so it
// rides a row whose kind stays `text`.
//
// Every field arrived inside the message. Nothing here is fetched, and the
// hi-res thumbnail WhatsApp offers alongside it is deliberately left alone:
// resolving a link to draw a preview of it would tell a stranger's server that
// this account read this message, which is precisely the leak that having the
// sender build the preview avoids.
type LinkPreviewPayload struct {
	// URL is what the preview points at, normalized to carry a scheme so it can
	// be handed to the desktop as-is. The message text keeps the sender's own
	// spelling of it.
	URL string `json:"url"`
	// Host is the site, lowercased and stripped of a leading "www.". It is the
	// one part of a URL worth showing at a glance, and the part that says
	// whether a link goes where its title implies.
	Host  string `json:"host,omitempty"`
	Title string `json:"title,omitempty"`
	// Description is the page's own summary. Senders' clients truncate it
	// already; a card elides whatever is left.
	Description string `json:"description,omitempty"`
	// Type is what the sender's client thought it was previewing: "video",
	// "image", "profile", "payment_links", "placeholder" or empty for a plain
	// page. It decides the layout, which is why it crosses as the sender's
	// word rather than being guessed from the URL.
	Type string `json:"type,omitempty"`
	// ThumbnailPath is the inline JPEG, written into the media cache. It is not
	// the row's `media` object: there is nothing to fetch here, and a kind
	// carrying a media object looks downloadable to everything that asks.
	ThumbnailPath string `json:"thumbnail_path,omitempty"`
	// ThumbnailWidth and ThumbnailHeight are the picture's real pixel size,
	// read back from the JPEG rather than believed from the message, so a card
	// can reserve the right shape before the image decodes and never jumps.
	ThumbnailWidth  int `json:"thumb_width,omitempty"`
	ThumbnailHeight int `json:"thumb_height,omitempty"`
}

// HasCard reports a preview worth drawing. A link with nothing but its own URL
// is not one: the text already contains it and already renders as a link, so a
// card would be an empty box repeating what is above it.
func (p *LinkPreviewPayload) HasCard() bool {
	if p == nil || p.URL == "" {
		return false
	}
	return p.Title != "" || p.Description != "" || p.ThumbnailPath != ""
}

// AlbumPayload is everything an AlbumMessage carries on the wire, which is only
// how many pictures to expect. The pictures themselves are separate messages
// pointing back at this one; they are joined from the messages table at read
// time rather than listed here, because each one keeps its own id, download
// state and receipts and none of that could survive being snapshotted.
//
// The counts are still worth keeping: they are what the sender promised, so a
// half-arrived album can say it is still filling instead of quietly rendering
// three of five.
type AlbumPayload struct {
	ExpectedImages int `json:"expected_images,omitempty"`
	ExpectedVideos int `json:"expected_videos,omitempty"`
}

// Expected is how many pictures the album header promised in total, 0 when it
// promised nothing.
func (p *AlbumPayload) Expected() int {
	if p == nil {
		return 0
	}
	return p.ExpectedImages + p.ExpectedVideos
}

// EventPayload is a scheduled event's fixed description. The RSVPs are not
// here: they change after the message lands and are joined from event_responses
// at read time, so a snapshot can never go stale.
type EventPayload struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// StartsAt and EndsAt are unix seconds. An event with no stated end is a
	// point in time rather than a range, which is how WhatsApp's own composer
	// leaves it.
	StartsAt int64 `json:"starts_at,omitempty"`
	EndsAt   int64 `json:"ends_at,omitempty"`
	// Canceled events keep their row: "this was called off" is information, and
	// deleting the message would leave people wondering whether it is still on.
	Canceled bool `json:"canceled,omitempty"`
	// JoinLink is a call link for a remote event, empty for one with a place.
	JoinLink string `json:"join_link,omitempty"`
	// Location is where it is, when the author attached one. It is the same
	// shape a shared place uses, so an event's venue gets the map treatment
	// without a second code path.
	Location *LocationPayload `json:"location,omitempty"`
	// ExtraGuestsAllowed lets a responder say they are bringing people.
	ExtraGuestsAllowed bool `json:"extra_guests_allowed,omitempty"`
	// ScheduleCall marks an event that is really a planned call rather than a
	// gathering, which is a different thing to say and a different glyph.
	ScheduleCall bool `json:"schedule_call,omitempty"`
	// ReminderOffsetSecs is how long before the start the author's clients will
	// remind, 0 for no reminder.
	ReminderOffsetSecs int64 `json:"reminder_offset_secs,omitempty"`
}

// GroupInvitePayload is an invitation to a group.
//
// The first block is what the sender's client put in the message and is all we
// are guaranteed. The second is what the daemon resolved by asking WhatsApp
// about the code, which is a strictly better description of the group: a name
// the sender's copy may have been stale about, how many people are in it, and
// whether we are one of them. A card renders from whichever it has, so an
// invite that never resolved still shows the sender's version rather than
// nothing.
type GroupInvitePayload struct {
	GroupJID string `json:"group_jid,omitempty"`
	Code     string `json:"code,omitempty"`
	// ExpiresAt is a unix second, and it is the invite that expires, not the
	// group: past it the code is dead and the card is a record of a door that
	// closed.
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// Name is the subject as the sender's client wrote it into the message.
	Name    string `json:"name,omitempty"`
	Caption string `json:"caption,omitempty"`
	// PhotoPath is the group picture the invite carried, written into the media
	// cache. It crosses as a path like every other image (rule 4), but not as
	// the row's `media` object: there is nothing to download here, and a kind
	// with a media object looks fetchable to every part of the frontend that
	// asks "is there anything to get".
	PhotoPath string `json:"photo_path,omitempty"`

	// Subject, Topic and MemberCount come from resolving the invite code.
	Subject     string `json:"subject,omitempty"`
	Topic       string `json:"topic,omitempty"`
	MemberCount int    `json:"member_count,omitempty"`
	// Joined says we are already in this group, which turns the card's action
	// from Join into Open. It is the one fact a phone's invite card never
	// tells you, and the one that decides what the button should do.
	Joined bool `json:"joined,omitempty"`
	// ResolvedAt is when the lookup last succeeded, 0 for never. A card with
	// nothing here is showing the sender's copy and should not claim otherwise.
	ResolvedAt int64 `json:"resolved_at,omitempty"`
	// ResolveError explains a lookup that failed, which is usually the invite
	// having been revoked. It is worth saying out loud: an invite that cannot
	// be resolved cannot be joined either.
	ResolveError string `json:"resolve_error,omitempty"`
}

// DisplayName is the best name the invite has for its group: what the lookup
// said, falling back to what the sender's client claimed.
func (p *GroupInvitePayload) DisplayName() string {
	if p == nil {
		return ""
	}
	if subject := strings.TrimSpace(p.Subject); subject != "" {
		return subject
	}
	return strings.TrimSpace(p.Name)
}

// PollPayload is a poll's fixed settings. The tally is not here: it changes on
// every vote and is joined from poll_options/poll_votes at read time, so a
// snapshot can never go stale.
type PollPayload struct {
	Question string `json:"question,omitempty"`
	// SelectableCount is how many options a voter may choose. 1 is the usual
	// radio-button poll; 0 means WhatsApp did not say, which in practice also
	// means one.
	SelectableCount int  `json:"selectable_count,omitempty"`
	AllowAddOption  bool `json:"allow_add_option,omitempty"`
	// EndsAt is when the poll closes, 0 for a poll that never does.
	EndsAt int64 `json:"ends_at,omitempty"`
	Quiz   bool  `json:"quiz,omitempty"`
}

// ContactsPayload is one or more shared contact cards. WhatsApp sends a single
// ContactMessage and a multi-card ContactsArrayMessage; both land here, so a
// renderer has one shape to deal with and the count tells it which it is.
type ContactsPayload struct {
	// DisplayName is the array's own label, set only by ContactsArrayMessage.
	DisplayName string        `json:"display_name,omitempty"`
	Cards       []ContactCard `json:"cards"`
}

// ContactCard is one person, parsed out of their vCard.
type ContactCard struct {
	DisplayName string         `json:"display_name,omitempty"`
	Org         string         `json:"org,omitempty"`
	Title       string         `json:"title,omitempty"`
	Birthday    string         `json:"birthday,omitempty"`
	Phones      []ContactField `json:"phones,omitempty"`
	Emails      []ContactField `json:"emails,omitempty"`
	URLs        []ContactField `json:"urls,omitempty"`
	Addresses   []ContactField `json:"addresses,omitempty"`
	// VCard is the sender's original text, kept verbatim so exporting the card
	// hands on exactly what arrived rather than a lossy re-serialization.
	VCard string `json:"vcard,omitempty"`
}

// ContactField is one labelled value on a card.
type ContactField struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value"`
	// JID is set when the vCard carried a `waid=` parameter, which is WhatsApp
	// stating that this number is on WhatsApp and what its jid is. It means a
	// "Message" action needs no lookup, and in particular no usync round trip
	// telling the server which contacts somebody forwarded us.
	JID string `json:"jid,omitempty"`
}

// LiveSharePayload is the state of a live-location share, kept on the message
// that opened it. It lives in the row's payload rather than being joined from
// live_location_shares so that listing a conversation stays one query: the
// state only changes on the same writes that rewrite the payload anyway.
type LiveSharePayload struct {
	// Active is false once the share has ended, whether by expiry, by the
	// sender stopping it, or by going quiet with no stop message. The bubble
	// settles from a live pin into a summary of where the share went.
	Active    bool  `json:"active"`
	StartedAt int64 `json:"started_at,omitempty"`
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// UpdatedAt is when the last position landed, so a bubble can say "updated
	// 8s ago" instead of implying a pin is current when it is minutes old.
	UpdatedAt int64 `json:"updated_at,omitempty"`
	// SpeedMPS and HeadingDegrees come from the newest position, when the
	// sender reported them at all.
	SpeedMPS       float64 `json:"speed_mps,omitempty"`
	HeadingDegrees int32   `json:"heading_deg,omitempty"`
	// PointCount is how long the trail is, which is what makes a finished share
	// worth looking at rather than just a stale pin.
	PointCount int `json:"point_count,omitempty"`
}

// LocationPayload is a shared place: a plain LocationMessage, the opening
// message of a live share, or the venue attached to an event.
type LocationPayload struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
	URL       string  `json:"url,omitempty"`
	// AccuracyMeters is the sender's reported precision, 0 when unknown. A
	// large value is worth showing: "within 500 m" is a different claim from a
	// pin on a doorway.
	AccuracyMeters uint32 `json:"accuracy_m,omitempty"`
	// Live marks the opening message of a live share. The moving state itself
	// arrives through the `live` object, not here.
	Live bool `json:"live,omitempty"`
}

// EncodePayload serializes a payload for the payload_json column. An empty
// payload encodes as the empty string rather than "{}", so the common case (a
// message with no structured payload at all) costs nothing on disk.
func EncodePayload(payload MessagePayload) (string, error) {
	if payload.isZero() {
		return "", nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// DecodePayload reads a payload_json column back. A row written by an older
// build, or by a newer one carrying a payload this build has never heard of,
// decodes into whatever fields it does recognise and drops the rest, which is
// the same forward-compatibility rule the wire follows.
func DecodePayload(raw string) MessagePayload {
	if strings.TrimSpace(raw) == "" {
		return MessagePayload{}
	}
	var payload MessagePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		// A payload that will not parse is a daemon bug, not a reason to lose
		// the message: the row still has its kind, its text and its summary.
		return MessagePayload{}
	}
	return payload
}

// isZero reports an envelope with nothing in it. Written out rather than
// compared with == so adding a pointer field here never silently changes what
// counts as empty.
func (p MessagePayload) isZero() bool {
	return p.Location == nil && p.LiveShare == nil && p.Contacts == nil &&
		p.Poll == nil && p.GroupInvite == nil && p.Event == nil && p.Album == nil &&
		p.LinkPreview == nil
}
