package wa

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	"net/url"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	appstore "whatevrd/internal/store"
)

// Link previews. A message with a link in it arrives as an ExtendedTextMessage
// carrying the card the sender's client already built: a title, a description,
// the matched URL and a thumbnail JPEG, all inline. None of it costs a request.
//
// This is the one payload that does not stand for its message. The text is
// still the message, so the row's kind stays `text` and the preview rides
// alongside it: quoting it quotes the words, searching finds the words, and a
// frontend that has never heard of a link preview still renders the message
// exactly as it did before.

// linkPreviewFromMessage builds the preview a message carries, or nil for a
// message that carries none worth drawing.
func (c *Client) linkPreviewFromMessage(chatID, messageID string, message *waE2E.Message) *appstore.LinkPreviewPayload {
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
		payload.ThumbnailPath = c.saveMessageThumbnailWithExtension(chatID, messageID, thumbnail, ".link.jpg")
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

// How long the full-size preview picture gets. It is a small file and the card
// already draws without it, so a slow fetch is dropped rather than queued.
const linkPreviewThumbnailTimeout = 30 * time.Second

// Extension of the fetched picture. Deliberately not the inline one's: the
// frontend addresses a picture by path, so overwriting in place would leave
// every open card showing the cached placeholder it already decoded.
const linkPreviewFullExtension = ".link.full.jpg"

// maybeFetchLinkPreviewThumbnail starts the fetch of the picture the card
// actually wants, in the background: ingest must not wait on a round trip.
//
// Only for a card that draws its picture large, and only for a live message.
// The compact layout puts the picture in a three-unit square, where the inline
// thumbnail is already more than enough, and a history sync carrying hundreds
// of old links would otherwise open hundreds of downloads on first login for
// cards nobody has scrolled to.
func (c *Client) maybeFetchLinkPreviewThumbnail(ctx context.Context, message appstore.Message, waMsg *waE2E.Message, live bool) {
	if !live {
		return
	}
	extended := waMsg.GetExtendedTextMessage()
	if extended == nil || extended.GetThumbnailDirectPath() == "" || len(extended.GetMediaKey()) == 0 {
		return
	}
	payload := appstore.DecodePayload(message.PayloadJSON).LinkPreview
	if payload == nil {
		return
	}
	// The same two types the card draws a hero for; see LinkPreviewCard.qml.
	if payload.Type != "video" && payload.Type != "image" {
		return
	}
	c.spawn(func(ctx context.Context) {
		c.fetchLinkPreviewThumbnail(ctx, message.ID, message.ChatID, extended)
	})
}

// fetchLinkPreviewThumbnail downloads the full-size preview picture and folds
// it back into the row.
func (c *Client) fetchLinkPreviewThumbnail(ctx context.Context, messageID, chatID string, extended *waE2E.ExtendedTextMessage) {
	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, linkPreviewThumbnailTimeout)
	defer cancel()

	data, err := client.DownloadThumbnail(ctx, extended)
	if err != nil {
		// The card is already drawn with the inline picture, so this is a
		// missed sharpening rather than a missing preview.
		c.log.Debugf("Failed to fetch the full link preview picture for %s: %v", messageID, err)
		return
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		c.log.Debugf("The full link preview picture for %s did not decode: %v", messageID, err)
		return
	}
	localPath := c.saveMessageThumbnailWithExtension(chatID, messageID, data, linkPreviewFullExtension)
	if localPath == "" {
		return
	}

	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		c.log.Warnf("Failed to read message %s back for its link preview picture: %v", messageID, err)
		return
	}
	payload := appstore.DecodePayload(message.PayloadJSON)
	if payload.LinkPreview == nil {
		return
	}
	payload.LinkPreview.ThumbnailPath = localPath
	payload.LinkPreview.ThumbnailWidth = config.Width
	payload.LinkPreview.ThumbnailHeight = config.Height
	encoded, err := appstore.EncodePayload(payload)
	if err != nil {
		c.log.Warnf("Failed to encode the link preview picture for %s: %v", messageID, err)
		return
	}
	updated, err := c.store.UpdateMessagePayload(ctx, messageID, encoded, message.PayloadSummary)
	if err != nil {
		c.log.Warnf("Failed to store the link preview picture for %s: %v", messageID, err)
		return
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
}
