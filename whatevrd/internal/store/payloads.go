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
	Interactive *InteractivePayload `json:"interactive,omitempty"`
	Commerce    *CommercePayload    `json:"commerce,omitempty"`
	StickerPack *StickerPackPayload `json:"sticker_pack,omitempty"`
	CallLog     *CallLogPayload     `json:"call_log,omitempty"`
	System      *SystemPayload      `json:"system,omitempty"`
	Waiting     *WaitingPayload     `json:"waiting,omitempty"`
}

// WaitingPayload is the hole a message left: it arrived, it could not be
// decrypted, and it has been asked for again.
//
// The row exists so the transcript says so. Before this the message simply was
// not there, which is indistinguishable from nobody having sent one, and a
// reader had no way to know they were missing something or that anything was
// being done about it.
type WaitingPayload struct {
	// FirstSeen is when the hole appeared, which is also the message's real
	// place in the transcript.
	FirstSeen int64 `json:"first_seen,omitempty"`
	// RetryAt is when the next attempt happens by itself: whatsmeow asks the
	// sender immediately and our own phone a few seconds later. Once it passes
	// with nothing to show, the only thing left is to ask again by hand.
	RetryAt int64 `json:"retry_at,omitempty"`
	// Requests counts how many times the message has been asked for, including
	// the automatic ones.
	Requests int `json:"requests,omitempty"`
	// Asked marks a request we made on purpose, so the wait can say whether it
	// is still the automatic one or a person's.
	Asked bool `json:"asked,omitempty"`
}

// InteractivePayload is a business message: a header, some words, a footer and
// a set of things the reader is invited to do.
//
// WhatsApp has four wire shapes for that one idea (ButtonsMessage, ListMessage,
// a hydrated TemplateMessage and InteractiveMessage, the last of which nests
// itself for carousels), and they differ in field names rather than in what
// they say. They are flattened here, because a frontend that had to know which
// of the four it received would be reimplementing this decision four times over
// and getting a different card each time.
//
// Nothing in here is fetched. A header that arrived as an ImageMessage or a
// VideoMessage contributes only the JPEG thumbnail it carried inline: these are
// promotional headers, not pictures somebody sent you to look at, and making
// the kind downloadable would put business broadcasts on the auto-download path.
type InteractivePayload struct {
	// Source is which wire shape this came from ("buttons", "list", "template",
	// "interactive", "carousel"). It exists so a bug report can say what
	// arrived, not so a renderer can branch on it.
	Source   string `json:"source,omitempty"`
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
	Footer   string `json:"footer,omitempty"`
	// ThumbnailPath is the header picture the message carried inline, written
	// into the media cache. Like a link preview's, it is not the row's `media`
	// object: there is nothing here to download.
	ThumbnailPath string `json:"thumbnail_path,omitempty"`
	// HeaderJPEG is that same picture as bytes, carried from parsing the
	// message to writing the cache file and no further. It is tagged out of
	// the JSON deliberately: a payload column is not where a picture goes, and
	// a field that could put one there by accident is a field worth spelling
	// out.
	HeaderJPEG []byte `json:"-"`
	// DocumentName names a header that arrived as a document. The document
	// itself is not fetched, but a card that cannot say what it is offering is
	// worse than one that names the file.
	DocumentName string              `json:"document_name,omitempty"`
	Buttons      []InteractiveButton `json:"buttons,omitempty"`
	// Sections are a ListMessage's contents, or a native-flow single-select's.
	// The label is the button that opens them.
	Sections  []InteractiveSection `json:"sections,omitempty"`
	ListLabel string               `json:"list_label,omitempty"`
	// Cards are a carousel's slides, each a whole card in its own right. The
	// recursion is one level deep in practice and bounded here regardless, so a
	// hostile message cannot make the daemon walk forever.
	Cards []InteractivePayload `json:"cards,omitempty"`
}

// Interactive button kinds. The first two are a handoff to the desktop and work
// exactly as well here as on a phone; the rest need a message sent back, which
// is what whatevr cannot do yet.
const (
	InteractiveButtonURL   = "url"
	InteractiveButtonCall  = "call"
	InteractiveButtonCopy  = "copy"
	InteractiveButtonReply = "reply"
	// InteractiveButtonOther is a native flow this build does not recognise: a
	// booking form, a payment sheet, a Bloks widget. It has a label and nothing
	// else honest to say.
	InteractiveButtonOther = "other"
)

// InteractiveButton is one thing a business message offers to do.
type InteractiveButton struct {
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
	URL   string `json:"url,omitempty"`
	Phone string `json:"phone,omitempty"`
	// Copy is the code a copy button puts on the clipboard, which is the whole
	// content of that button: a discount code, a one-time password.
	Copy string `json:"copy,omitempty"`
	ID   string `json:"id,omitempty"`
	// Live says the desktop can actually carry the button out. Opening a link,
	// dialling a number and copying a code are all local; everything else means
	// sending a message back to a business, which is section 2. Saying so on
	// the wire is what lets the card show a dead button as dead rather than
	// letting somebody press it and wonder.
	Live bool `json:"live"`
}

// InteractiveSection is one titled group of rows in a list.
type InteractiveSection struct {
	Title string           `json:"title,omitempty"`
	Rows  []InteractiveRow `json:"rows,omitempty"`
}

// InteractiveRow is one choice inside a list section.
type InteractiveRow struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	ID          string `json:"id,omitempty"`
}

// HasContent reports a card worth drawing. A business message stripped of every
// word and every button is a row with nothing on it, and its `fallback` says
// more than an empty box would.
func (p *InteractivePayload) HasContent() bool {
	if p == nil {
		return false
	}
	return p.Title != "" || p.Body != "" || p.Footer != "" || p.Subtitle != "" ||
		p.ThumbnailPath != "" || len(p.HeaderJPEG) > 0 || p.DocumentName != "" ||
		len(p.Buttons) > 0 || len(p.Sections) > 0 || len(p.Cards) > 0
}

// Commerce kinds, spelled as they cross the wire.
const (
	CommerceKindProduct = "product"
	CommerceKindOrder   = "order"
	// CommerceKindPaymentRequest is somebody asking to be paid,
	// CommerceKindPaymentSent is somebody saying they paid, and
	// CommerceKindPaymentInvite is an offer to set payments up at all.
	CommerceKindPaymentRequest = "payment_request"
	CommerceKindPaymentSent    = "payment_sent"
	CommerceKindPaymentInvite  = "payment_invite"
)

// CommercePayload is a product, an order or a payment: three wire shapes that
// render as one card, because they are the same card. Each is a picture, a
// name, a sum of money and a line saying where it stands.
//
// The money crosses as the integer WhatsApp sends (thousandths of a currency
// unit) plus its ISO 4217 code, not as a formatted string. Formatting a sum is
// a question about the reader's locale, and the daemon does not have one.
type CommercePayload struct {
	Kind        string `json:"kind"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	Footer      string `json:"footer,omitempty"`
	// ThumbnailPath is the inline picture, written into the media cache.
	ThumbnailPath string `json:"thumbnail_path,omitempty"`
	// Amount1000 is thousandths of a unit: 1299000 is 1299.00 in Currency.
	Amount1000 int64  `json:"amount_1000,omitempty"`
	Currency   string `json:"currency,omitempty"`
	// SalePrice1000 is a discounted price when the seller set one, so a card
	// can strike through the original rather than quietly showing one number.
	SalePrice1000 int64 `json:"sale_price_1000,omitempty"`

	// Product.
	ProductID  string `json:"product_id,omitempty"`
	RetailerID string `json:"retailer_id,omitempty"`
	URL        string `json:"url,omitempty"`
	ImageCount int    `json:"image_count,omitempty"`
	SellerJID  string `json:"seller_jid,omitempty"`

	// Order.
	OrderID   string `json:"order_id,omitempty"`
	ItemCount int    `json:"item_count,omitempty"`
	// Status is "inquiry", "accepted" or "declined", empty when unstated.
	Status string `json:"status,omitempty"`

	// Payment.
	Note string `json:"note,omitempty"`
	// ExpiresAt is when a request or an invite lapses, 0 for one that does not.
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// Service is the payment rail ("upi", "pix", "fbpay", "novi"), which is
	// worth naming: it is the difference between a card somebody can act on and
	// one they cannot.
	Service string `json:"service,omitempty"`
	// RequestedFrom is who a payment request was addressed to.
	RequestedFrom string `json:"requested_from,omitempty"`
}

// StickerPackPayload is a shared sticker pack.
//
// It is the one card in this family that does something rather than describing
// something: whatevr already keeps a sticker library and already has a command
// to install a pack into it. Installable says whether this particular pack is
// one the daemon can find, because a pack made by hand on somebody's phone is
// not in WhatsApp's index and cannot be added by id.
type StickerPackPayload struct {
	PackID      string `json:"pack_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	Description string `json:"description,omitempty"`
	Caption     string `json:"caption,omitempty"`
	// Count is how many stickers the pack holds, as the share claimed.
	Count       int  `json:"count,omitempty"`
	Installable bool `json:"installable,omitempty"`
	Installed   bool `json:"installed,omitempty"`
	// ResolvedAt is when the daemon last checked the two flags above against
	// its own library, 0 for never.
	ResolvedAt int64 `json:"resolved_at,omitempty"`
}

// Call outcomes, spelled as they cross the wire.
const (
	CallOutcomeConnected = "connected"
	CallOutcomeMissed    = "missed"
	CallOutcomeFailed    = "failed"
	CallOutcomeRejected  = "rejected"
	CallOutcomeElsewhere = "accepted_elsewhere"
	CallOutcomeOngoing   = "ongoing"
	CallOutcomeSilenced  = "silenced"
)

// CallLogPayload is a call that happened, recorded in the chat it happened in.
//
// It is not a message anybody wrote, which is why it renders as a centered pill
// rather than a bubble: putting it in a plate on one side would claim somebody
// said something, and nobody did.
type CallLogPayload struct {
	Video   bool   `json:"video,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	// DurationSecs is how long it lasted, 0 for a call that never connected.
	DurationSecs int64 `json:"duration_secs,omitempty"`
	// Group marks a call with more than the two of you in it.
	Group bool `json:"group,omitempty"`
	// Scheduled marks a planned call rather than one somebody just placed.
	Scheduled bool `json:"scheduled,omitempty"`
	VoiceChat bool `json:"voice_chat,omitempty"`
	// Participants is how many people the log named, 0 when it named none.
	Participants int `json:"participants,omitempty"`
}

// System event types, spelled as they cross the wire. They name what happened,
// not how it should look: the glyph a pill draws is the frontend's choice.
const (
	SystemTypeGroupJoin       = "group_join"
	SystemTypeGroupLeave      = "group_leave"
	SystemTypeGroupPromote    = "group_promote"
	SystemTypeGroupDemote     = "group_demote"
	SystemTypeGroupName       = "group_name"
	SystemTypeGroupTopic      = "group_topic"
	SystemTypeGroupPhoto      = "group_photo"
	SystemTypeGroupLocked     = "group_locked"
	SystemTypeGroupAnnounce   = "group_announce"
	SystemTypeGroupApproval   = "group_approval"
	SystemTypeGroupInviteLink = "group_invite_link"
	SystemTypeGroupLink       = "group_link"
	SystemTypeGroupUnlink     = "group_unlink"
	SystemTypeGroupDelete     = "group_delete"
	SystemTypeEphemeral       = "ephemeral"
	SystemTypeIdentityChange  = "identity_change"
)

// SystemParticipant is one person a system event named, with their name
// resolved at ingest so a pill never has to look anybody up.
type SystemParticipant struct {
	JID  string `json:"jid,omitempty"`
	Name string `json:"name,omitempty"`
	// Self marks us, which is what lets a sentence say "you" and what makes a
	// row loud enough to reorder the chat list.
	Self bool `json:"self,omitempty"`
}

// SystemPayload is something the chat did rather than something somebody said:
// a membership change, a setting, a security code. It renders as a centered
// pill, because putting it in a plate on one side would claim an author it does
// not have.
//
// The whole participant list is kept here even though a pill shows three names
// and a count. It is what makes the coalescing below possible (a second add has
// to know who the first one named), and it is what a "who exactly" affordance
// would need later.
type SystemPayload struct {
	Type string `json:"type"`
	// Actor is who did it. Absent for events the server reports with no author,
	// which is normal for a join through an invite link.
	Actor *SystemParticipant `json:"actor,omitempty"`
	// Participants are who it was done to, in arrival order.
	Participants []SystemParticipant `json:"participants,omitempty"`
	// Value is the new subject, description or invite link, depending on Type.
	Value string `json:"value,omitempty"`
	// Detail is the type's own qualifier: which kind of community link changed,
	// or the reason a group was deleted. It is recorded rather than rendered.
	Detail string `json:"detail,omitempty"`
	// On carries the direction of a two-state change: disappearing messages
	// turned on or off, the group locked or unlocked, announcements restricted
	// or opened up.
	On bool `json:"on,omitempty"`
	// Seconds is the new disappearing-message timer, meaningful when On.
	Seconds uint32 `json:"seconds,omitempty"`
	// AboutSelf is set when the event named us. It is decided once, here, so
	// that every reader (the chat list, the unread count, the pill's emphasis)
	// agrees about which events are worth interrupting somebody for.
	AboutSelf bool `json:"about_self,omitempty"`
}

// NamesParticipant reports whether a JID is already in the list, so a repeat of
// the same event does not name somebody twice.
func (p *SystemPayload) NamesParticipant(jid string) bool {
	if p == nil {
		return false
	}
	for _, participant := range p.Participants {
		if participant.JID == jid {
			return true
		}
	}
	return false
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
		p.LinkPreview == nil && p.Interactive == nil && p.Commerce == nil &&
		p.StickerPack == nil && p.CallLog == nil && p.System == nil && p.Waiting == nil
}
