package wa

import (
	"context"
	"testing"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// A forwarded-to-us message must keep the sender's forward marker all the way
// into the row: the phone shows its Forwarded header from
// ContextInfo.IsForwarded, and the desktop's header reads the same flag off
// the wire. Only our own forwards were flagged before (at send time), so
// inbound forwards rendered plain on desktop while the phone showed them.
func TestInboundForwardedMarkerSurvivesIngest(t *testing.T) {
	client := newMediaIngestClient(t)

	fwd := func() *waE2E.ContextInfo {
		return &waE2E.ContextInfo{IsForwarded: proto.Bool(true)}
	}

	text, ok := client.textMessageInput(context.Background(), mediaIngestEvent("fwd-text", &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String("pass it on"),
			ContextInfo: fwd(),
		},
	}), ingestOptions{source: sourceLive})
	if !ok || !text.IsForwarded {
		t.Fatalf("text forward: ok=%v forwarded=%v", ok, text.IsForwarded)
	}

	plain, ok := client.textMessageInput(context.Background(), mediaIngestEvent("plain-text", &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("mine"),
		},
	}), ingestOptions{source: sourceLive})
	if !ok || plain.IsForwarded {
		t.Fatalf("plain text must not be flagged: ok=%v forwarded=%v", ok, plain.IsForwarded)
	}

	image, ok := client.imageMessageInput(context.Background(), mediaIngestEvent("fwd-image", &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption:     proto.String("look"),
			Mimetype:    proto.String("image/jpeg"),
			ContextInfo: fwd(),
		},
	}), ingestOptions{source: sourceLive})
	if !ok || !image.IsForwarded {
		t.Fatalf("image forward: ok=%v forwarded=%v", ok, image.IsForwarded)
	}

	sticker, ok := client.stickerMessageInput(context.Background(), mediaIngestEvent("fwd-sticker", &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			Mimetype:    proto.String("image/webp"),
			ContextInfo: fwd(),
		},
	}), ingestOptions{source: sourceLive})
	if !ok || !sticker.IsForwarded {
		t.Fatalf("sticker forward: ok=%v forwarded=%v", ok, sticker.IsForwarded)
	}
}
