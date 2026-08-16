package wa

import (
	"bytes"
	"image"
	_ "image/jpeg"
	"net/url"
	"strings"

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
		if payload.ThumbnailPath != "" {
			// Measured from the picture rather than believed from the message:
			// the width and height an ExtendedTextMessage states describe the
			// full-size thumbnail it offers to fetch, not the one inline, and a
			// card that reserved the wrong shape would resize on decode.
			if config, _, err := image.DecodeConfig(bytes.NewReader(thumbnail)); err == nil {
				payload.ThumbnailWidth = config.Width
				payload.ThumbnailHeight = config.Height
			}
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
