package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// Albums on the wire.
//
// An AlbumMessage is a header and nothing else: it says how many pictures are
// coming and stops. The pictures follow as ordinary image and video messages,
// each pointing back at the header through MessageContextInfo.MessageAssociation
// rather than through anything on the album itself.
//
// That is why the association is read here, centrally, for every media kind
// instead of inside imageMessageInput and videoMessageInput: the link is on the
// envelope, not on the payload, and a kind that starts appearing in albums
// later gets grouped without anyone remembering to add it.

func (c *Client) albumMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	album := evt.Message.GetAlbumMessage()
	if album == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := c.mediaInputBase(ctx, evt, opts, "", album.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.AlbumPayload{
		ExpectedImages: int(album.GetExpectedImageCount()),
		ExpectedVideos: int(album.GetExpectedVideoCount()),
	}
	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Album: payload})
	if err != nil {
		c.log.Warnf("Failed to encode album payload for %s: %v", base.ID, err)
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

// publishAlbumChild announces a picture that landed inside an album, and
// reports whether it did. The album row is what the transcript shows, so that
// is the row a frontend has to be told about: the child itself is in no window
// and an insert naming it would be an insert nobody is holding a place for.
//
// false means this row is not grouped after all, either because it named no
// album or because the album it names never arrived. Then it is an ordinary
// photo and the caller announces it as one, which is what keeps a lost header
// from swallowing its pictures.
func (c *Client) publishAlbumChild(ctx context.Context, child appstore.Message) bool {
	if child.AlbumParentID == "" {
		return false
	}
	message, err := c.store.GetMessage(ctx, child.AlbumParentID)
	if err != nil {
		c.log.Debugf("Album %s has a picture but no header yet: %v", child.AlbumParentID, err)
		return false
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(message))
	return true
}
