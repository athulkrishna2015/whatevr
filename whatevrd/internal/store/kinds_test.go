package store

import "testing"

func TestPreviewLineRendersEveryKind(t *testing.T) {
	cases := []struct {
		name  string
		facts PreviewFacts
		want  string
	}{
		{"text", PreviewFacts{Text: "see you then"}, "see you then"},
		{"text collapses newlines", PreviewFacts{Text: "two\nlines"}, "two lines"},
		{"empty text", PreviewFacts{}, ""},
		{"revoked beats everything", PreviewFacts{Text: "gone", MediaKind: MediaKindImage, Revoked: true}, RevokedPreview},

		{"photo", PreviewFacts{MediaKind: MediaKindImage}, "📷 Photo"},
		{"photo caption wins", PreviewFacts{MediaKind: MediaKindImage, Text: "at the beach"}, "at the beach"},
		{"sticker never yields to a caption", PreviewFacts{MediaKind: MediaKindSticker, Text: "ignored"}, "🎨 Sticker"},
		{"voice note with duration", PreviewFacts{MediaKind: MediaKindVoice, DurationSecs: 12}, "🎤 Voice message (0:12)"},
		{"voice note over a minute", PreviewFacts{MediaKind: MediaKindVoice, DurationSecs: 95}, "🎤 Voice message (1:35)"},
		{"document filename beats caption", PreviewFacts{MediaKind: MediaKindDocument, MediaFileName: "report.pdf", Text: "have a look"}, "📄 report.pdf"},
		{"document without a name", PreviewFacts{MediaKind: MediaKindDocument}, "📄 Document"},

		{"poll carries its question", PreviewFacts{MediaKind: MediaKindPoll, PayloadSummary: "dinner?"}, "📊 Poll: dinner?"},
		{"location carries its place", PreviewFacts{MediaKind: MediaKindLocation, PayloadSummary: "Cafe Noir"}, "📍 Location: Cafe Noir"},
		{"location without a name", PreviewFacts{MediaKind: MediaKindLocation}, "📍 Location"},
		{"system row is only its summary", PreviewFacts{MediaKind: MediaKindSystem, PayloadSummary: "Ana and 12 others joined"}, "Ana and 12 others joined"},
		{"waiting ignores a caption", PreviewFacts{MediaKind: MediaKindWaiting, Text: "ignored"}, "⏳ Waiting for this message"},

		// Rows written before media_kind carried anything but image and
		// sticker have only their mime type to go on.
		{"legacy image row", PreviewFacts{MediaMimeType: "image/jpeg"}, "📷 Photo"},
		{"legacy audio row", PreviewFacts{MediaMimeType: "audio/ogg"}, "🎵 Audio"},
		{"legacy unknown row", PreviewFacts{MediaMimeType: "application/x-thing"}, "📎 Media"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PreviewLine(tc.facts); got != tc.want {
				t.Fatalf("PreviewLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Every kind the ingest can write must be in the descriptor table, or its
// messages render as a blank line in the chat list and a blank `fallback` on
// the wire. This is the test that fails when someone adds a MediaKind constant
// and forgets the other half.
func TestEveryKindIsDescribed(t *testing.T) {
	kinds := []string{
		MediaKindImage, MediaKindSticker, MediaKindGIF, MediaKindVideo, MediaKindVideoNote,
		MediaKindVoice, MediaKindAudio, MediaKindDocument,
		MediaKindLocation, MediaKindLiveLocation, MediaKindContact, MediaKindContacts,
		MediaKindPoll, MediaKindGroupInvite, MediaKindEvent, MediaKindAlbum,
		MediaKindInteractive, MediaKindProduct, MediaKindOrder, MediaKindPayment,
		MediaKindStickerPack, MediaKindCallLog, MediaKindSystem, MediaKindWaiting,
		MediaKindUnsupported,
	}
	for _, kind := range kinds {
		if _, ok := DescribeKind(kind); !ok {
			t.Errorf("kind %q has no descriptor", kind)
		}
	}
	if _, ok := DescribeKind(""); ok {
		t.Error("the empty kind (a plain text message) must not have a descriptor")
	}
}

// Only kinds with something to fetch may carry a media object on the wire. A
// poll or a contact card that looked media-bearing would render a download
// button over nothing and trip the frontend's auto-download.
func TestOnlyFetchableKindsAreMediaBearing(t *testing.T) {
	bearing := map[string]bool{
		MediaKindImage: true, MediaKindSticker: true, MediaKindGIF: true,
		MediaKindVideo: true, MediaKindVideoNote: true, MediaKindVoice: true,
		MediaKindAudio: true, MediaKindDocument: true,
		// A location's map is fetched and stitched by the daemon.
		MediaKindLocation: true, MediaKindLiveLocation: true,
	}
	for kind := range kindDescriptors {
		if got := IsMediaBearingKind(kind); got != bearing[kind] {
			t.Errorf("IsMediaBearingKind(%q) = %v, want %v", kind, got, bearing[kind])
		}
	}
	if IsMediaBearingKind("") {
		t.Error("a text message is not media-bearing")
	}
}

func TestEncodeDecodePayloadRoundTrip(t *testing.T) {
	payload := MessagePayload{Location: &LocationPayload{
		Latitude: 12.9716, Longitude: 77.5946, Name: "Cafe Noir", Address: "12 MG Road",
	}}
	encoded, err := EncodePayload(payload)
	if err != nil {
		t.Fatalf("EncodePayload() error = %v", err)
	}
	if encoded == "" {
		t.Fatal("EncodePayload() dropped a non-empty payload")
	}
	decoded := DecodePayload(encoded)
	if decoded.Location == nil || decoded.Location.Name != "Cafe Noir" || decoded.Location.Latitude != 12.9716 {
		t.Fatalf("DecodePayload() = %+v", decoded.Location)
	}
}

func TestEncodePayloadKeepsTheEmptyCaseEmpty(t *testing.T) {
	encoded, err := EncodePayload(MessagePayload{})
	if err != nil {
		t.Fatalf("EncodePayload() error = %v", err)
	}
	if encoded != "" {
		t.Fatalf("EncodePayload(empty) = %q, want the empty string", encoded)
	}
}

// A payload column this build cannot parse must not take the message down with
// it: the row still has its kind, its text and its summary.
func TestDecodePayloadSurvivesGarbage(t *testing.T) {
	if got := DecodePayload("{not json"); got != (MessagePayload{}) {
		t.Fatalf("DecodePayload(garbage) = %+v, want the zero payload", got)
	}
	if got := DecodePayload("   "); got != (MessagePayload{}) {
		t.Fatalf("DecodePayload(blank) = %+v, want the zero payload", got)
	}
}
