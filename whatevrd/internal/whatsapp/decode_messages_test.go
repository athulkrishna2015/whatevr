package whatsapp

import (
	"context"
	"testing"
	"time"
	appstore "whatevrd/internal/store"

	"go.mau.fi/whatsmeow/proto/waE2E"
	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestMapWebMessageStatusKnownValues(t *testing.T) {
	cases := []struct {
		in   waWeb.WebMessageInfo_Status
		want string
	}{
		{waWeb.WebMessageInfo_PENDING, appstore.StatusPending},
		{waWeb.WebMessageInfo_SERVER_ACK, appstore.StatusSent},
		{waWeb.WebMessageInfo_DELIVERY_ACK, appstore.StatusDelivered},
		{waWeb.WebMessageInfo_READ, appstore.StatusRead},
		{waWeb.WebMessageInfo_PLAYED, appstore.StatusRead},
		{waWeb.WebMessageInfo_ERROR, appstore.StatusFailed},
	}

	for _, tc := range cases {
		status := tc.in
		webMsg := &waWeb.WebMessageInfo{Status: &status}
		got := mapWebMessageStatus(webMsg)
		if got != tc.want {
			t.Errorf("mapWebMessageStatus(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMapWebMessageStatusReturnsEmptyForNil(t *testing.T) {
	if got := mapWebMessageStatus(nil); got != "" {
		t.Errorf("mapWebMessageStatus(nil) = %q, want empty", got)
	}

	if got := mapWebMessageStatus(&waWeb.WebMessageInfo{}); got != "" {
		t.Errorf("mapWebMessageStatus(empty) = %q, want empty", got)
	}
}

func TestMessageTimestampPrefersOutgoingHistoryC2STimestamp(t *testing.T) {
	c2s := uint64(1_700_000_100)
	info := types.MessageInfo{
		MessageSource: types.MessageSource{IsFromMe: true},
		Timestamp:     time.Unix(1_700_000_200, 0),
	}
	webMsg := &waWeb.WebMessageInfo{MessageC2STimestamp: &c2s}

	got := messageTimestamp(info, ingestOptions{source: sourceHistorySync}, webMsg)
	if want := time.Unix(1_700_000_100, 0); !got.Equal(want) {
		t.Fatalf("messageTimestamp() = %v, want %v", got, want)
	}
}

func TestMessageTimestampParsesMillisecondC2STimestamp(t *testing.T) {
	c2s := uint64(1_700_000_100_123)
	info := types.MessageInfo{
		MessageSource: types.MessageSource{IsFromMe: true},
		Timestamp:     time.Unix(1_700_000_200, 0),
	}
	webMsg := &waWeb.WebMessageInfo{MessageC2STimestamp: &c2s}

	got := messageTimestamp(info, ingestOptions{source: sourceHistorySync}, webMsg)
	if want := time.UnixMilli(1_700_000_100_123); !got.Equal(want) {
		t.Fatalf("messageTimestamp() = %v, want %v", got, want)
	}
}

func TestMessageTimestampFallsBackForIncomingLiveAndInvalidC2S(t *testing.T) {
	fallback := time.Unix(1_700_000_200, 0)
	c2s := uint64(1_700_000_100)
	invalidC2S := uint64(9_999_999_999_999)

	cases := []struct {
		name   string
		info   types.MessageInfo
		opts   ingestOptions
		webMsg *waWeb.WebMessageInfo
	}{
		{
			name:   "incoming history",
			info:   types.MessageInfo{MessageSource: types.MessageSource{IsFromMe: false}, Timestamp: fallback},
			opts:   ingestOptions{source: sourceHistorySync},
			webMsg: &waWeb.WebMessageInfo{MessageC2STimestamp: &c2s},
		},
		{
			name:   "live outgoing",
			info:   types.MessageInfo{MessageSource: types.MessageSource{IsFromMe: true}, Timestamp: fallback},
			opts:   ingestOptions{source: sourceLive},
			webMsg: &waWeb.WebMessageInfo{MessageC2STimestamp: &c2s},
		},
		{
			name:   "invalid c2s",
			info:   types.MessageInfo{MessageSource: types.MessageSource{IsFromMe: true}, Timestamp: fallback},
			opts:   ingestOptions{source: sourceHistorySync},
			webMsg: &waWeb.WebMessageInfo{MessageC2STimestamp: &invalidC2S},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageTimestamp(tc.info, tc.opts, tc.webMsg); !got.Equal(fallback) {
				t.Fatalf("messageTimestamp() = %v, want fallback %v", got, fallback)
			}
		})
	}
}

func TestMessageTimestampPrefersRetryTimestampOverride(t *testing.T) {
	fallback := time.Unix(1_700_000_200, 0)
	override := time.Unix(1_700_000_100, 0)
	c2s := uint64(1_700_000_050)
	info := types.MessageInfo{
		MessageSource: types.MessageSource{IsFromMe: true},
		Timestamp:     fallback,
	}
	webMsg := &waWeb.WebMessageInfo{MessageC2STimestamp: &c2s}

	got := messageTimestamp(info, ingestOptions{source: sourceHistorySync, timestampOverride: override}, webMsg)
	if !got.Equal(override) {
		t.Fatalf("messageTimestamp() = %v, want override %v", got, override)
	}
}

func TestQuotedReplyPreviewExtractsImageCaption(t *testing.T) {
	text, mediaKind, mimeType := quotedReplyPreview(&waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption:  proto.String("caption"),
			Mimetype: proto.String("image/png"),
		},
	})
	if text != "caption" || mediaKind != appstore.MediaKindImage || mimeType != "image/png" {
		t.Fatalf("quotedReplyPreview() = %q, %q, %q", text, mediaKind, mimeType)
	}
}

func TestFormatPhoneDisplayNameFormatsInternationalNumber(t *testing.T) {
	jid := types.NewJID("917060029183", types.DefaultUserServer)
	if got, want := formatPhoneDisplayName(jid), "+91 70600 29183"; got != want {
		t.Fatalf("formatPhoneDisplayName() = %q, want %q", got, want)
	}
}

func TestFormatPhoneDisplayNameRejectsLID(t *testing.T) {
	jid := types.NewJID("123456", types.HiddenUserServer)
	if got := formatPhoneDisplayName(jid); got != "" {
		t.Fatalf("formatPhoneDisplayName(lid) = %q, want empty", got)
	}
}

func TestUnsupportedMessageLabel(t *testing.T) {
	cases := []struct {
		name   string
		evt    *events.Message
		want   string
		wantOK bool
	}{
		// Documents, audio, video, GIFs and video notes are no longer
		// tombstoned: they have real builders and real bubbles, so the label
		// whitelist must not claim them.
		{
			name: "document is rendered, not tombstoned",
			evt: &events.Message{Message: &waE2E.Message{
				DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("report.pdf")},
			}},
			wantOK: false,
		},
		{
			name: "voice note is rendered, not tombstoned",
			evt: &events.Message{Message: &waE2E.Message{
				AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true)},
			}},
			wantOK: false,
		},
		{
			name: "gif playback video is rendered, not tombstoned",
			evt: &events.Message{Message: &waE2E.Message{
				VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true)},
			}},
			wantOK: false,
		},
		{
			name: "view once video is still tombstoned",
			evt: &events.Message{
				IsViewOnce: true,
				Message: &waE2E.Message{
					VideoMessage: &waE2E.VideoMessage{},
				},
			},
			want:   "View once video",
			wantOK: true,
		},
		{
			name: "poll v3",
			evt: &events.Message{Message: &waE2E.Message{
				PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("Lunch?")},
			}},
			// Polls render for real now, so they must fall off the
			// tombstone whitelist.
			want:   "",
			wantOK: false,
		},
		{
			name: "view once photo",
			evt: &events.Message{
				IsViewOnce: true,
				Message: &waE2E.Message{
					ImageMessage: &waE2E.ImageMessage{},
				},
			},
			want:   "View once photo",
			wantOK: true,
		},
		{
			// A location renders for real now, so it must fall off the
			// tombstone whitelist: a kind on both lists would ingest as a
			// location and be labelled as unsupported at the same time.
			name: "location is no longer a tombstone",
			evt: &events.Message{Message: &waE2E.Message{
				LocationMessage: &waE2E.LocationMessage{Name: proto.String("Cafe")},
			}},
			want:   "",
			wantOK: false,
		},
		{
			// Same reason as the location above: a group invite has a card of
			// its own now, and a kind on both lists ingests as an invite while
			// being labelled unsupported.
			name: "group invite is no longer a tombstone",
			evt: &events.Message{Message: &waE2E.Message{
				GroupInviteMessage: &waE2E.GroupInviteMessage{GroupName: proto.String("Wow3")},
			}},
			want:   "",
			wantOK: false,
		},
		{
			name: "poll update stays invisible",
			evt: &events.Message{Message: &waE2E.Message{
				PollUpdateMessage: &waE2E.PollUpdateMessage{},
			}},
			wantOK: false,
		},
		{
			name: "protocol message stays invisible",
			evt: &events.Message{Message: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{},
			}},
			wantOK: false,
		},
		{
			name:   "empty message stays invisible",
			evt:    &events.Message{Message: &waE2E.Message{}},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := unsupportedMessageLabel(tc.evt)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("unsupportedMessageLabel() = %q, %v; want %q, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func mediaIngestEvent(id string, message *waE2E.Message) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			ID: types.MessageID(id),
			MessageSource: types.MessageSource{
				Chat:   types.JID{User: "5551234", Server: types.DefaultUserServer},
				Sender: types.JID{User: "5551234", Server: types.DefaultUserServer},
			},
			Timestamp: time.Unix(1_700_000_000, 0),
		},
		Message: message,
	}
}

// TestMediaMessageInputCoversPlayableKinds locks in that every kind whatevr can
// now render takes a real media row rather than the unsupported tombstone.
func TestMediaMessageInputCoversPlayableKinds(t *testing.T) {
	d := newTestDecoder(t)

	waveform := make([]byte, 64)
	for i := range waveform {
		waveform[i] = byte(i)
	}

	cases := []struct {
		name          string
		message       *waE2E.Message
		wantKind      string
		wantMime      string
		wantText      string
		wantDuration  int32
		wantSize      int64
		wantFileName  string
		wantPageCount int32
		wantWaveform  int
		wantAnimated  bool
	}{
		{
			name: "video with caption",
			message: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
				Mimetype:   proto.String("video/mp4"),
				Caption:    proto.String("look at this"),
				Seconds:    proto.Uint32(42),
				FileLength: proto.Uint64(1 << 20),
				Width:      proto.Uint32(1280),
				Height:     proto.Uint32(720),
			}},
			wantKind:     appstore.MediaKindVideo,
			wantMime:     "video/mp4",
			wantText:     "look at this",
			wantDuration: 42,
			wantSize:     1 << 20,
		},
		{
			name: "gif playback",
			message: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
				GifPlayback: proto.Bool(true),
				Seconds:     proto.Uint32(3),
			}},
			wantKind:     appstore.MediaKindGIF,
			wantMime:     "video/mp4",
			wantDuration: 3,
			wantAnimated: true,
		},
		{
			name: "video note",
			message: &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
				Seconds: proto.Uint32(7),
			}},
			wantKind:     appstore.MediaKindVideoNote,
			wantMime:     "video/mp4",
			wantDuration: 7,
		},
		{
			name: "voice note with waveform",
			message: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
				PTT:        proto.Bool(true),
				Seconds:    proto.Uint32(12),
				FileLength: proto.Uint64(8192),
				Waveform:   waveform,
			}},
			wantKind:     appstore.MediaKindVoice,
			wantMime:     "audio/ogg; codecs=opus",
			wantDuration: 12,
			wantSize:     8192,
			wantWaveform: 64,
		},
		{
			name: "audio file",
			message: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
				Mimetype: proto.String("audio/mpeg"),
				Seconds:  proto.Uint32(200),
			}},
			wantKind:     appstore.MediaKindAudio,
			wantMime:     "audio/mpeg",
			wantDuration: 200,
		},
		{
			name: "document with caption and page count",
			message: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
				Mimetype:   proto.String("application/pdf"),
				FileName:   proto.String("report.pdf"),
				Caption:    proto.String("see page 3"),
				PageCount:  proto.Uint32(11),
				FileLength: proto.Uint64(4096),
			}},
			wantKind:      appstore.MediaKindDocument,
			wantMime:      "application/pdf",
			wantText:      "see page 3",
			wantSize:      4096,
			wantFileName:  "report.pdf",
			wantPageCount: 11,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evt := mediaIngestEvent(types.MessageID(string(rune('A'+i))), tc.message)
			input, ok := d.mediaMessageInput(context.Background(), evt, ingestOptions{})
			if !ok {
				t.Fatalf("mediaMessageInput() returned ok=false")
			}
			if input.MediaKind != tc.wantKind {
				t.Errorf("kind = %q, want %q", input.MediaKind, tc.wantKind)
			}
			if input.MediaMimeType != tc.wantMime {
				t.Errorf("mime = %q, want %q", input.MediaMimeType, tc.wantMime)
			}
			if input.Text != tc.wantText {
				t.Errorf("text = %q, want %q", input.Text, tc.wantText)
			}
			if input.MediaDurationSecs != tc.wantDuration {
				t.Errorf("duration = %d, want %d", input.MediaDurationSecs, tc.wantDuration)
			}
			if input.MediaSizeBytes != tc.wantSize {
				t.Errorf("size = %d, want %d", input.MediaSizeBytes, tc.wantSize)
			}
			if input.MediaFileName != tc.wantFileName {
				t.Errorf("filename = %q, want %q", input.MediaFileName, tc.wantFileName)
			}
			if input.MediaPageCount != tc.wantPageCount {
				t.Errorf("page count = %d, want %d", input.MediaPageCount, tc.wantPageCount)
			}
			if len(input.MediaWaveform) != tc.wantWaveform {
				t.Errorf("waveform length = %d, want %d", len(input.MediaWaveform), tc.wantWaveform)
			}
			if input.MediaAnimated != tc.wantAnimated {
				t.Errorf("animated = %v, want %v", input.MediaAnimated, tc.wantAnimated)
			}
			if len(input.MediaPayload) == 0 {
				t.Error("media payload is empty, so the message can never be downloaded")
			}
		})
	}
}

// TestViewOnceMediaStaysTombstoned guards the one case that must keep falling
// through to the unsupported path now that video and audio have builders.
func TestViewOnceMediaStaysTombstoned(t *testing.T) {
	d := newTestDecoder(t)

	evt := mediaIngestEvent("VO1", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{}})
	evt.IsViewOnce = true

	input, ok := d.mediaMessageInput(context.Background(), evt, ingestOptions{})
	if !ok {
		t.Fatal("mediaMessageInput() returned ok=false for view-once video")
	}
	if input.MediaKind != appstore.MediaKindUnsupported {
		t.Fatalf("kind = %q, want %q", input.MediaKind, appstore.MediaKindUnsupported)
	}
	if input.Text != "View once video" {
		t.Fatalf("label = %q, want %q", input.Text, "View once video")
	}
}

// TestNormalizedWaveformRejectsWrongShapes keeps garbage out of the bubble: a
// waveform is 64 buckets of 0-100 or it is not stored at all.
func TestNormalizedWaveformRejectsWrongShapes(t *testing.T) {
	if got := normalizedWaveform(nil); got != nil {
		t.Errorf("nil waveform = %v, want nil", got)
	}
	if got := normalizedWaveform(make([]byte, 32)); got != nil {
		t.Errorf("short waveform = %v, want nil", got)
	}
	oversized := make([]byte, waveformBuckets)
	oversized[0] = 250
	got := normalizedWaveform(oversized)
	if len(got) != waveformBuckets {
		t.Fatalf("length = %d, want %d", len(got), waveformBuckets)
	}
	if got[0] != 100 {
		t.Errorf("clamped value = %d, want 100", got[0])
	}
}

// A message made only of spaces is a message. Trimming it away lost a row that
// really arrived, and the tombstone path then claimed the daemon had never
// heard of a plain text payload.
func TestWhitespaceOnlyTextIsStillText(t *testing.T) {
	ctx := context.Background()
	d := newTestDecoder(t)

	evt := &events.Message{
		Info: types.MessageInfo{
			ID: "WHITESPACE1",
			MessageSource: types.MessageSource{
				Chat:   types.JID{User: "111", Server: types.DefaultUserServer},
				Sender: types.JID{User: "111", Server: types.DefaultUserServer},
			},
		},
		Message: &waE2E.Message{Conversation: proto.String("   \t ")},
	}
	input, ok := d.textMessageInput(ctx, evt, ingestOptions{})
	if !ok {
		t.Fatal("a whitespace only message was declined")
	}
	if input.Text != "   \t " {
		t.Fatalf("text = %q, want the spaces it was sent with", input.Text)
	}
}

// An empty conversation is an empty envelope, not a payload nobody has written
// code for. It gets no row, and above all no grey "Unsupported message" bubble.
func TestEmptyTextIsNotTombstoned(t *testing.T) {
	ctx := context.Background()
	d := newTestDecoder(t)

	for name, message := range map[string]*waE2E.Message{
		"conversation":  {Conversation: proto.String("")},
		"extended text": {ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("")}},
	} {
		evt := &events.Message{
			Info: types.MessageInfo{
				ID: "EMPTY1",
				MessageSource: types.MessageSource{
					Chat:   types.JID{User: "111", Server: types.DefaultUserServer},
					Sender: types.JID{User: "111", Server: types.DefaultUserServer},
				},
			},
			Message: message,
		}
		if _, ok := d.textMessageInput(ctx, evt, ingestOptions{}); ok {
			t.Fatalf("%s: an empty message was stored as text", name)
		}
		if _, ok := d.unsupportedMessageInput(ctx, evt, ingestOptions{}); ok {
			t.Fatalf("%s: an empty message was stored as a tombstone", name)
		}
	}
}
