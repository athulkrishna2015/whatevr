package wa

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	appstore "whatevrd/internal/store"
)

func TestAlbumHeaderKeepsWhatItPromised(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := client.mediaMessageInput(context.Background(), mediaIngestEvent("al1", &waE2E.Message{
		AlbumMessage: &waE2E.AlbumMessage{
			ExpectedImageCount: proto.Uint32(3),
			ExpectedVideoCount: proto.Uint32(1),
		},
	}), ingestOptions{source: sourceLive})
	if !ok {
		t.Fatal("an album header must ingest as its own kind, not a tombstone")
	}
	if input.MediaKind != appstore.MediaKindAlbum {
		t.Fatalf("kind = %q", input.MediaKind)
	}
	if input.PayloadSummary != "3 photos and 1 video" {
		t.Fatalf("summary = %q", input.PayloadSummary)
	}
	payload := appstore.DecodePayload(input.PayloadJSON).Album
	if payload == nil {
		t.Fatal("no album payload was stored")
	}
	if payload.ExpectedImages != 3 || payload.ExpectedVideos != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	// The header itself has nothing to fetch. Its pictures do.
	if appstore.MessageCarriesMedia(appstore.Message{MediaKind: input.MediaKind, PayloadJSON: input.PayloadJSON}) {
		t.Error("an album header must not look like something waiting to be downloaded")
	}
}

// The link from a picture to its album is on the envelope rather than on the
// payload, which is why it is read once for every media kind instead of inside
// each one. A photo that names an album has to come out of the ingest already
// tagged, or the album has no way to find it.
func TestPictureCarriesTheAlbumItBelongsTo(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := client.mediaMessageInput(context.Background(), mediaIngestEvent("pic1", &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Mimetype: proto.String("image/jpeg"),
			Caption:  proto.String("first"),
		},
		MessageContextInfo: &waE2E.MessageContextInfo{
			MessageAssociation: &waE2E.MessageAssociation{
				AssociationType:  waE2E.MessageAssociation_MEDIA_ALBUM.Enum(),
				ParentMessageKey: &waCommon.MessageKey{ID: proto.String("al1")},
				MessageIndex:     proto.Int32(2),
			},
		},
	}), ingestOptions{source: sourceLive})
	if !ok {
		t.Fatal("a picture inside an album is still an ordinary image row")
	}
	if input.MediaKind != appstore.MediaKindImage {
		t.Fatalf("kind = %q: a tile must stay a photo in every respect", input.MediaKind)
	}
	want := internalMessageIDForChat(input.ChatID, "al1")
	if input.AlbumParentID != want {
		t.Fatalf("album parent = %q, want %q", input.AlbumParentID, want)
	}
	// The index is the sender's layout, not our arrival order, which is what
	// keeps the tiles in place when they arrive out of order or one is resent.
	if input.AlbumIndex != 2 {
		t.Fatalf("album index = %d, want 2", input.AlbumIndex)
	}
}

// The same field carries bot plugins and event cover images. Those are
// associations to something that is not an album, and grouping them as one
// would hide a message behind a parent that never draws it.
func TestOnlyAMediaAlbumAssociationGroupsAnything(t *testing.T) {
	client := newMediaIngestClient(t)

	for _, association := range []waE2E.MessageAssociation_AssociationType{
		waE2E.MessageAssociation_UNKNOWN,
		waE2E.MessageAssociation_BOT_PLUGIN,
		waE2E.MessageAssociation_EVENT_COVER_IMAGE,
	} {
		input, ok := client.mediaMessageInput(context.Background(), mediaIngestEvent("pic2", &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")},
			MessageContextInfo: &waE2E.MessageContextInfo{
				MessageAssociation: &waE2E.MessageAssociation{
					AssociationType:  association.Enum(),
					ParentMessageKey: &waCommon.MessageKey{ID: proto.String("al1")},
				},
			},
		}), ingestOptions{source: sourceLive})
		if !ok {
			t.Fatalf("%s: the image must still ingest", association)
		}
		if input.AlbumParentID != "" {
			t.Errorf("%s grouped a message into an album it is not part of", association)
		}
	}
}

func TestAlbumSummaryCountsWhatItHas(t *testing.T) {
	cases := []struct {
		images, videos int
		want           string
	}{
		{1, 0, "1 photo"},
		{5, 0, "5 photos"},
		{0, 1, "1 video"},
		{0, 3, "3 videos"},
		{2, 1, "2 photos and 1 video"},
		{0, 0, ""},
	}
	for _, tc := range cases {
		got := albumSummary(&appstore.AlbumPayload{ExpectedImages: tc.images, ExpectedVideos: tc.videos})
		if got != tc.want {
			t.Errorf("albumSummary(%d, %d) = %q, want %q", tc.images, tc.videos, got, tc.want)
		}
	}
}

func TestQuotedAlbumPreviewCountsItsPictures(t *testing.T) {
	text, kind, _ := quotedReplyPreview(&waE2E.Message{
		AlbumMessage: &waE2E.AlbumMessage{ExpectedImageCount: proto.Uint32(4)},
	})
	if kind != appstore.MediaKindAlbum {
		t.Fatalf("kind = %q", kind)
	}
	if text != "4 photos" {
		t.Fatalf("quote = %q", text)
	}
}
