package whatsapp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/url"
	"strings"
	"time"
	appstore "whatevrd/internal/store"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func (d *Decoder) albumMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	album := evt.Message.GetAlbumMessage()
	if album == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := d.mediaInputBase(ctx, evt, opts, "", album.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.AlbumPayload{
		ExpectedImages: int(album.GetExpectedImageCount()),
		ExpectedVideos: int(album.GetExpectedVideoCount()),
	}
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Album: payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode album payload")
		return appstore.MediaMessageInput{}, false
	}

	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindAlbum,
		PayloadSummary:   albumSummary(payload),
	}, true
}

// albumSummary describes an album by what the header promised, which is all it
// knows at the moment it lands: its pictures have not arrived yet, and the chat
// list wants a line now rather than after the last one turns up.
func albumSummary(payload *appstore.AlbumPayload) string {
	if payload == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if n := payload.ExpectedImages; n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, "photo", "photos")))
	}
	if n := payload.ExpectedVideos; n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, "video", "videos")))
	}
	return strings.Join(parts, " and ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// applyAlbumAssociation tags a media row with the album that owns it, if any.
//
// The index is the sender's, not ours: it is what keeps the pictures in the
// order they were laid out even when they arrive out of order or a resend
// arrives days later. Only MEDIA_ALBUM is honoured; the same field also carries
// bot plugins and event cover images, which are associations to something that
// is not an album and must not be grouped as one.
func applyAlbumAssociation(input *appstore.MediaMessageInput, message *waE2E.Message) {
	association := message.GetMessageContextInfo().GetMessageAssociation()
	if association.GetAssociationType() != waE2E.MessageAssociation_MEDIA_ALBUM {
		return
	}
	parentID := strings.TrimSpace(association.GetParentMessageKey().GetID())
	if parentID == "" || input.ChatID == "" {
		return
	}
	input.AlbumParentID = internalMessageIDForChat(input.ChatID, types.MessageID(parentID))
	input.AlbumIndex = association.GetMessageIndex()
}

func (d *Decoder) contactMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}

	var (
		payload     appstore.ContactsPayload
		contextInfo *waE2E.ContextInfo
		kind        string
	)
	switch {
	case evt.Message.GetContactMessage() != nil:
		contact := evt.Message.GetContactMessage()
		contextInfo = contact.GetContextInfo()
		kind = appstore.MediaKindContact
		payload.Cards = []appstore.ContactCard{cardFromContactMessage(contact)}
	case evt.Message.GetContactsArrayMessage() != nil:
		array := evt.Message.GetContactsArrayMessage()
		contextInfo = array.GetContextInfo()
		kind = appstore.MediaKindContacts
		payload.DisplayName = strings.TrimSpace(array.GetDisplayName())
		for _, contact := range array.GetContacts() {
			payload.Cards = append(payload.Cards, cardFromContactMessage(contact))
		}
		// An array that arrived with exactly one card is a single contact as
		// far as anybody reading it is concerned.
		if len(payload.Cards) == 1 {
			kind = appstore.MediaKindContact
		}
	default:
		return appstore.MediaMessageInput{}, false
	}
	if len(payload.Cards) == 0 {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := d.mediaInputBase(ctx, evt, opts, "", contextInfo)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Contacts: &payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode contacts payload")
		return appstore.MediaMessageInput{}, false
	}

	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        kind,
		PayloadSummary:   contactsSummary(payload),
	}, true
}

// cardFromContactMessage parses a contact's vCard, falling back to the display
// name WhatsApp sends alongside it when the card itself carries no name.
func cardFromContactMessage(contact *waE2E.ContactMessage) appstore.ContactCard {
	card := parseVCard(contact.GetVcard())
	if card.DisplayName == "" {
		card.DisplayName = strings.TrimSpace(contact.GetDisplayName())
	}
	return card
}

// contactsSummary is the detail on the one-line rendering: the person's name
// for a single card, and a count for several, because listing four names in a
// chat-list row helps nobody.
func contactsSummary(payload appstore.ContactsPayload) string {
	if len(payload.Cards) == 1 {
		if name := payload.Cards[0].DisplayName; name != "" {
			return name
		}
		if phone := primaryPhone(payload.Cards[0]); phone != "" {
			return phone
		}
		return ""
	}
	if payload.DisplayName != "" {
		return payload.DisplayName
	}
	// English-only, like every other daemon-side summary: the wire's `fallback`
	// is a last resort for frontends that cannot render the kind, and the ones
	// that can render their own localized text from the payload.
	return fmt.Sprintf("%d contacts", len(payload.Cards))
}

// primaryPhone is the number a card leads with: the one WhatsApp vouched for
// if there is one, otherwise simply the first.
func primaryPhone(card appstore.ContactCard) string {
	for _, phone := range card.Phones {
		if phone.JID != "" {
			return phone.Value
		}
	}
	if len(card.Phones) > 0 {
		return card.Phones[0].Value
	}
	return ""
}

// locationMessageInput turns a LocationMessage into a row. A share with IsLive
// set opens a live share instead of a static pin, and the updates that follow
// are folded into this same row (see live_location.go).
func (d *Decoder) locationMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	location := evt.Message.GetLocationMessage()
	if location == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, location.GetComment(), location.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := locationPayloadFromMessage(location)
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Location: payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode location payload")
	}

	base.PayloadJSON = encoded

	kind := appstore.MediaKindLocation
	if payload.Live {
		kind = appstore.MediaKindLiveLocation
	}

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        kind,
		// The stitched map is a PNG. The row claims that mime up front so a
		// frontend knows what it will get before the fetch finishes.
		MediaMimeType:           "image/png",
		MediaThumbnailLocalPath: d.saveMessageThumbnail(chatID, base.ID, location.GetJPEGThumbnail()),
		MediaWidth:              mapOutputWidth,
		MediaHeight:             mapOutputHeight,
		PayloadSummary:          locationSummary(payload),
	}, true
}

// liveUpdateInput is a live share's position as a row of its own, for an
// update whose opener never reached us. the model shows only such an update;
// the rest fold into the row their share opened with.
func (d *Decoder) liveUpdateInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	update := evt.Message.GetLiveLocationMessage()
	if update == nil {
		return appstore.MediaMessageInput{}, false
	}
	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, update.GetCaption(), update.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}
	payload := &appstore.LocationPayload{
		Latitude:       update.GetDegreesLatitude(),
		Longitude:      update.GetDegreesLongitude(),
		AccuracyMeters: update.GetAccuracyInMeters(),
		Live:           true,
	}
	base.PayloadJSON, _ = appstore.EncodePayload(appstore.MessagePayload{Location: payload})
	return appstore.MediaMessageInput{
		TextMessageInput:        base,
		MediaKind:               appstore.MediaKindLiveLocation,
		MediaMimeType:           "image/png",
		MediaThumbnailLocalPath: d.saveMessageThumbnail(chatID, base.ID, update.GetJPEGThumbnail()),
		MediaWidth:              mapOutputWidth,
		MediaHeight:             mapOutputHeight,
		PayloadSummary:          locationSummary(payload),
	}, true
}

func locationPayloadFromMessage(location *waE2E.LocationMessage) *appstore.LocationPayload {
	return &appstore.LocationPayload{
		Latitude:       location.GetDegreesLatitude(),
		Longitude:      location.GetDegreesLongitude(),
		Name:           strings.TrimSpace(location.GetName()),
		Address:        strings.TrimSpace(location.GetAddress()),
		URL:            strings.TrimSpace(location.GetURL()),
		AccuracyMeters: location.GetAccuracyInMeters(),
		Live:           location.GetIsLive(),
	}
}

// LocationSummary is the one line a location row carries, see
// locationSummary.
func LocationSummary(payload *appstore.LocationPayload) string { return locationSummary(payload) }

// locationSummary is the detail that rides the one-line rendering: the place
// name if the sender named one, its address if not, and the coordinates as a
// last resort, because "📍 Location" alone tells you nothing about which one.
func locationSummary(payload *appstore.LocationPayload) string {
	if payload == nil {
		return ""
	}
	if payload.Name != "" {
		return payload.Name
	}
	if payload.Address != "" {
		return payload.Address
	}
	return formatCoordinates(payload.Latitude, payload.Longitude)
}

// formatCoordinates renders a position at roughly one-metre resolution, which
// is as precise as a shared pin ever is and short enough to read in a chat list.
func formatCoordinates(lat, lng float64) string {
	if lat == 0 && lng == 0 {
		return ""
	}
	return fmt.Sprintf("%.5f, %.5f", lat, lng)
}

// isLocationKind reports whether a row's media is a map we draw rather than a
// blob WhatsApp holds. An event counts when it named a venue: its map is the
// same map, drawn the same way, and giving events their own lesser one would
// be the only reason an event's place ever looked different from a shared one.
func isLocationKind(message appstore.Message) bool {
	switch message.MediaKind {
	case appstore.MediaKindLocation, appstore.MediaKindLiveLocation:
		return true
	case appstore.MediaKindEvent:
		return appstore.DecodePayload(message.PayloadJSON).Event.HasVenue()
	default:
		return false
	}
}

// mapLocationFor picks the place a row's map should be drawn around, whichever
// kind of row it is.
func mapLocationFor(message appstore.Message) *appstore.LocationPayload {
	payload := appstore.DecodePayload(message.PayloadJSON)
	if payload.Location != nil {
		return payload.Location
	}
	if payload.Event != nil {
		return payload.Event.Location
	}
	return nil
}

// pollCreationFromMessage finds the poll in whichever slot it arrived in.
func pollCreationFromMessage(msg *waE2E.Message) *waE2E.PollCreationMessage {
	if msg == nil {
		return nil
	}
	// Deliberately not V4: that slot holds a FutureProofMessage, and reading it
	// as a poll would be a type error waiting to happen.
	for _, candidate := range []*waE2E.PollCreationMessage{
		msg.GetPollCreationMessage(),
		msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(),
		msg.GetPollCreationMessageV5(),
		msg.GetPollCreationMessageV6(),
	} {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func (d *Decoder) pollMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	poll := pollCreationFromMessage(evt.Message)
	if poll == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := d.mediaInputBase(ctx, evt, opts, "", poll.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.PollPayload{
		Question:        strings.TrimSpace(poll.GetName()),
		SelectableCount: int(poll.GetSelectableOptionsCount()),
		AllowAddOption:  poll.GetAllowAddOption(),
		EndsAt:          poll.GetEndTime(),
		Quiz:            poll.GetPollType() == waE2E.PollType_QUIZ,
	}
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Poll: payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode poll payload")
		return appstore.MediaMessageInput{}, false
	}

	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindPoll,
		PayloadSummary:   payload.Question,
	}, true
}

func (d *Decoder) eventMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	event := evt.Message.GetEventMessage()
	if event == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, "", event.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.EventPayload{
		Name:               strings.TrimSpace(event.GetName()),
		Description:        strings.TrimSpace(event.GetDescription()),
		StartsAt:           event.GetStartTime(),
		EndsAt:             event.GetEndTime(),
		Canceled:           event.GetIsCanceled(),
		JoinLink:           strings.TrimSpace(event.GetJoinLink()),
		ExtraGuestsAllowed: event.GetExtraGuestsAllowed(),
		ScheduleCall:       event.GetIsScheduleCall(),
		ReminderOffsetSecs: event.GetReminderOffsetSec(),
	}

	input := appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindEvent,
		PayloadSummary:   payload.Name,
	}

	// An event's venue is an ordinary shared place, so it gets the same map the
	// location bubble gets: same payload shape, same stitched PNG, same
	// download lifecycle. Writing a second, lesser map for events would be the
	// only reason events ever looked different from places.
	if location := event.GetLocation(); location != nil {
		payload.Location = locationPayloadFromMessage(location)
		input.MediaMimeType = "image/png"
		input.MediaThumbnailLocalPath = d.saveMessageThumbnail(chatID, base.ID, location.GetJPEGThumbnail())
		input.MediaWidth = mapOutputWidth
		input.MediaHeight = mapOutputHeight
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Event: payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode event payload")
		return appstore.MediaMessageInput{}, false
	}
	input.PayloadJSON = encoded
	return input, true
}

// eventResponseValue maps the wire enum to the string the store and the wire
// both use. UNKNOWN becomes "maybe" rather than an empty answer, because a
// response we cannot name is still somebody having answered.
func eventResponseValue(response waE2E.EventResponseMessage_EventResponseType) string {
	switch response {
	case waE2E.EventResponseMessage_GOING:
		return appstore.EventResponseGoing
	case waE2E.EventResponseMessage_NOT_GOING:
		return appstore.EventResponseNotGoing
	default:
		return appstore.EventResponseMaybe
	}
}

// eventResponseType is the inverse, for our own outgoing answer.
func eventResponseType(value string) (waE2E.EventResponseMessage_EventResponseType, bool) {
	switch value {
	case appstore.EventResponseGoing:
		return waE2E.EventResponseMessage_GOING, true
	case appstore.EventResponseNotGoing:
		return waE2E.EventResponseMessage_NOT_GOING, true
	case appstore.EventResponseMaybe:
		return waE2E.EventResponseMessage_MAYBE, true
	default:
		return waE2E.EventResponseMessage_UNKNOWN, false
	}
}

// eventSummary is the detail on the one-line rendering: the event's name, which
// is the whole point of it.
func eventSummary(event *waE2E.EventMessage) string {
	if event == nil {
		return ""
	}
	return strings.TrimSpace(event.GetName())
}

func ptrTo[T any](value T) *T {
	return &value
}

func (d *Decoder) groupInviteMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	invite := evt.Message.GetGroupInviteMessage()
	if invite == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, invite.GetCaption(), invite.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.GroupInvitePayload{
		GroupJID:  strings.TrimSpace(invite.GetGroupJID()),
		Code:      strings.TrimSpace(invite.GetInviteCode()),
		ExpiresAt: invite.GetInviteExpiration(),
		Name:      strings.TrimSpace(invite.GetGroupName()),
		Caption:   strings.TrimSpace(invite.GetCaption()),
		// The thumbnail is saved with the group-invite suffix rather than the
		// generic one so it cannot collide with a row's media thumbnail if this
		// message id is ever reused for another kind.
		PhotoPath: d.saveMessageThumbnailWithExtension(chatID, base.ID, invite.GetJPEGThumbnail(), ".invite.jpg"),
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{GroupInvite: payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode group invite payload")
		return appstore.MediaMessageInput{}, false
	}

	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindGroupInvite,
		PayloadSummary:   payload.DisplayName(),
	}, true
}

// groupInviteExpired reports whether the code has lapsed. An invite with no
// stated expiry never does.
func groupInviteExpired(payload *appstore.GroupInvitePayload, now time.Time) bool {
	return payload != nil && payload.ExpiresAt > 0 && now.Unix() >= payload.ExpiresAt
}

// groupInviteSummary is the detail on the one-line rendering. The name is the
// whole point of the invite, so a chat list reads "👥 Group invite: Wow3".
func groupInviteSummary(invite *waE2E.GroupInviteMessage) string {
	if invite == nil {
		return ""
	}
	if name := strings.TrimSpace(invite.GetGroupName()); name != "" {
		return name
	}
	return strings.TrimSpace(invite.GetGroupJID())
}

// linkPreviewFromMessage builds the preview a message carries, or nil for a
// message that carries none worth drawing.
func (d *Decoder) linkPreviewFromMessage(chatID, messageID string, message *waE2E.Message) *appstore.LinkPreviewPayload {
	extended := message.GetExtendedTextMessage()
	if extended == nil {
		return nil
	}

	canonical, host := linkPreviewURL(extended.GetMatchedText())
	if canonical == "" {
		return nil
	}

	payload := &appstore.LinkPreviewPayload{
		URL:         canonical,
		Host:        host,
		Title:       strings.TrimSpace(extended.GetTitle()),
		Description: strings.TrimSpace(extended.GetDescription()),
		Type:        linkPreviewType(extended.GetPreviewType()),
	}

	if thumbnail := extended.GetJPEGThumbnail(); len(thumbnail) > 0 {
		payload.ThumbnailPath = d.saveMessageThumbnailWithExtension(chatID, messageID, thumbnail, ".link.jpg")
	}
	// The shape is the full-size picture's, which the message states, not the
	// inline JPEG's. The inline one is a ~90px square placeholder whatever the
	// real picture looks like, so believing it drew every hero as a square: a
	// 16:9 still arrived cropped to a box and blown up five times over. Reserve
	// the true shape now and the card does not reflow when the real picture
	// lands; fall back to measuring the inline one when nothing is stated.
	if width, height := int(extended.GetThumbnailWidth()), int(extended.GetThumbnailHeight()); width > 0 && height > 0 {
		payload.ThumbnailWidth = width
		payload.ThumbnailHeight = height
	} else if payload.ThumbnailPath != "" {
		if config, _, err := image.DecodeConfig(bytes.NewReader(extended.GetJPEGThumbnail())); err == nil {
			payload.ThumbnailWidth = config.Width
			payload.ThumbnailHeight = config.Height
		}
	}

	if !payload.HasCard() {
		return nil
	}
	return payload
}

// linkPreviewURL normalizes the matched text into something the desktop can be
// handed, and pulls out the host worth showing beside the title.
//
// The sender's spelling is left in the message text; this is only what a click
// should open. A URL with no scheme is assumed https, which is what every
// client that produced one of these assumed when it built the preview.
func linkPreviewURL(matched string) (canonical, host string) {
	matched = strings.TrimSpace(matched)
	if matched == "" {
		return "", ""
	}

	parsed, err := url.Parse(matched)
	if err != nil {
		return "", ""
	}
	if parsed.Scheme == "" {
		parsed, err = url.Parse("https://" + matched)
		if err != nil {
			return "", ""
		}
	}
	// A preview is a preview of a page. Anything else that happens to parse
	// (a mailto:, a tel:, a bare word with a colon in it) has no site to name
	// and nothing to open in a browser.
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", ""
	}
	if parsed.Host == "" {
		return "", ""
	}

	host = strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	return parsed.String(), host
}

// linkPreviewType carries the sender's own word for what it previewed. It
// decides the layout, so it is passed through rather than guessed from the URL:
// only the client that fetched the page knows whether the thumbnail is a video
// still or a favicon-sized logo.
func linkPreviewType(previewType waE2E.ExtendedTextMessage_PreviewType) string {
	switch previewType {
	case waE2E.ExtendedTextMessage_VIDEO:
		return "video"
	case waE2E.ExtendedTextMessage_IMAGE:
		return "image"
	case waE2E.ExtendedTextMessage_PLACEHOLDER:
		return "placeholder"
	case waE2E.ExtendedTextMessage_PAYMENT_LINKS:
		return "payment_links"
	case waE2E.ExtendedTextMessage_PROFILE:
		return "profile"
	default:
		return ""
	}
}

// Extension of the fetched picture. Deliberately not the inline one's: the
// frontend addresses a picture by path, so overwriting in place would leave
// every open card showing the cached placeholder it already decoded.
const linkPreviewFullExtension = ".link.full.jpg"

// a map preview is a 3x2 grid of 256px tiles cropped so the pin sits in the
// middle and the tile seams fall outside
const (
	mapOutputWidth  = 640
	mapOutputHeight = 400
)
