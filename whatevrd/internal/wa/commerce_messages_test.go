package wa

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	appstore "whatevrd/internal/store"
)

// A product is a picture, a name and a sum of money. The money crosses as the
// integer WhatsApp sent, because how a sum reads is a question about the person
// reading it and the daemon has never met them.
func TestProductKeepsItsPriceAsANumber(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := ingestBusiness(t, client, "prod1", &waE2E.Message{ProductMessage: &waE2E.ProductMessage{
		Body:   proto.String("Back in stock."),
		Footer: proto.String("Ships from Bengaluru"),
		Product: &waE2E.ProductMessage_ProductSnapshot{
			ProductID:           proto.String("prod-8821"),
			Title:               proto.String("Channapatna top"),
			Description:         proto.String("Turned from ivory wood."),
			CurrencyCode:        proto.String("inr"),
			PriceAmount1000:     proto.Int64(449000),
			SalePriceAmount1000: proto.Int64(399000),
			URL:                 proto.String("cauverycrafts.com/tops"),
			ProductImage:        &waE2E.ImageMessage{JPEGThumbnail: testThumbnailJPEG(t, 80, 80)},
		},
	}})
	if !ok {
		t.Fatal("a product must ingest")
	}
	if input.MediaKind != appstore.MediaKindProduct {
		t.Fatalf("kind = %q", input.MediaKind)
	}

	payload := appstore.DecodePayload(input.PayloadJSON).Commerce
	if payload == nil {
		t.Fatal("no commerce payload was stored")
	}
	if payload.Kind != appstore.CommerceKindProduct {
		t.Fatalf("commerce kind = %q", payload.Kind)
	}
	if payload.Amount1000 != 449000 || payload.SalePrice1000 != 399000 {
		t.Fatalf("prices = %d / %d", payload.Amount1000, payload.SalePrice1000)
	}
	// The code is uppercased because ISO 4217 is uppercase and a card should
	// not have to normalise what it was handed.
	if payload.Currency != "INR" {
		t.Fatalf("currency = %q", payload.Currency)
	}
	if payload.URL != "https://cauverycrafts.com/tops" {
		t.Fatalf("url = %q", payload.URL)
	}
	if payload.ThumbnailPath == "" {
		t.Fatal("the product picture was not written to the cache")
	}
	if input.PayloadSummary != "Channapatna top" {
		t.Fatalf("summary = %q", input.PayloadSummary)
	}
}

// Three payment shapes share one kind because they share one card: asking,
// paying and being invited to pay differ by a line on it, not by what to draw.
func TestPaymentsShareOneKind(t *testing.T) {
	client := newMediaIngestClient(t)

	cases := []struct {
		name    string
		message *waE2E.Message
		want    string
	}{
		{"request", &waE2E.Message{RequestPaymentMessage: &waE2E.RequestPaymentMessage{
			NoteMessage:         &waE2E.Message{Conversation: proto.String("Dinner on Saturday")},
			CurrencyCodeIso4217: proto.String("INR"),
			Amount1000:          proto.Uint64(1250000),
		}}, appstore.CommerceKindPaymentRequest},
		{"sent", &waE2E.Message{SendPaymentMessage: &waE2E.SendPaymentMessage{
			NoteMessage: &waE2E.Message{Conversation: proto.String("Sent")},
		}}, appstore.CommerceKindPaymentSent},
		{"invite", &waE2E.Message{PaymentInviteMessage: &waE2E.PaymentInviteMessage{
			ServiceType: waE2E.PaymentInviteMessage_UPI.Enum(),
		}}, appstore.CommerceKindPaymentInvite},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, ok := ingestBusiness(t, client, "pay-"+tc.name, tc.message)
			if !ok {
				t.Fatal("a payment message must ingest")
			}
			if input.MediaKind != appstore.MediaKindPayment {
				t.Fatalf("kind = %q", input.MediaKind)
			}
			payload := appstore.DecodePayload(input.PayloadJSON).Commerce
			if payload == nil || payload.Kind != tc.want {
				t.Fatalf("payload = %+v", payload)
			}
		})
	}
}

// A Money states its own divisor. Reading it as though it were the flat
// thousandths sitting next to it in the same message would be off by whatever
// that divisor was.
func TestMoneyScalesToThousandths(t *testing.T) {
	cases := []struct {
		value  int64
		offset uint32
		want   int64
	}{
		// 12.99, then 1299.00 at the same scale amount1000 uses, then a value
		// with no divisor at all, which is whole units.
		{1299, 100, 12990},
		{1299000, 1000, 1299000},
		{1299, 0, 1299000},
	}
	for _, tc := range cases {
		if got := paymentAmountToThousandths(tc.value, tc.offset); got != tc.want {
			t.Fatalf("paymentAmountToThousandths(%d, %d) = %d, want %d", tc.value, tc.offset, got, tc.want)
		}
	}
}

// A call is not a message anybody wrote, and what it says depends on which end
// of it you were: "missed" and "unanswered" are the same event read from two
// sides, and using one word for both accuses the wrong person.
func TestCallLogReadsFromTheRightSide(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := ingestBusiness(t, client, "call1", &waE2E.Message{CallLogMesssage: &waE2E.CallLogMessage{
		IsVideo:      proto.Bool(true),
		CallOutcome:  waE2E.CallLogMessage_MISSED.Enum(),
		DurationSecs: proto.Int64(0),
		CallType:     waE2E.CallLogMessage_REGULAR.Enum(),
	}})
	if !ok {
		t.Fatal("a call log must ingest")
	}
	if input.MediaKind != appstore.MediaKindCallLog {
		t.Fatalf("kind = %q", input.MediaKind)
	}
	payload := appstore.DecodePayload(input.PayloadJSON).CallLog
	if payload == nil || !payload.Video || payload.Outcome != appstore.CallOutcomeMissed {
		t.Fatalf("payload = %+v", payload)
	}
	if input.PayloadSummary != "Missed video call" {
		t.Fatalf("summary = %q", input.PayloadSummary)
	}

	outgoing := callLogSummary(payload, true)
	if outgoing != "Unanswered video call" {
		t.Fatalf("outgoing summary = %q", outgoing)
	}

	connected := callLogSummary(&appstore.CallLogPayload{DurationSecs: 161}, false)
	if connected != "Voice call (2:41)" {
		t.Fatalf("connected summary = %q", connected)
	}
}

// A call log's chat-list line is the summary alone. Leading it with the kind's
// own name would give "📞 Call: missed video call", which is longer and says
// less than "📞 Missed video call".
func TestCallLogPreviewIsNotPrefixedTwice(t *testing.T) {
	line := appstore.MessagePreviewLine(appstore.Message{
		MediaKind:      appstore.MediaKindCallLog,
		PayloadSummary: "Missed video call",
	})
	if line != "📞 Missed video call" {
		t.Fatalf("preview = %q", line)
	}

	business := appstore.MessagePreviewLine(appstore.Message{
		MediaKind:      appstore.MediaKindInteractive,
		PayloadSummary: "Your table is held until 8pm.",
	})
	if business != "💬 Your table is held until 8pm." {
		t.Fatalf("preview = %q", business)
	}

	// A kind that does have a label still gets it, so nothing else moved.
	product := appstore.MessagePreviewLine(appstore.Message{
		MediaKind:      appstore.MediaKindProduct,
		PayloadSummary: "Channapatna top",
	})
	if product != "🛍️ Product: Channapatna top" {
		t.Fatalf("preview = %q", product)
	}
}

// A shared pack is the one card here that does something, so what the library
// knows about it has to be right. It is joined at read time rather than stored,
// because installing a pack from the picker must not leave a card elsewhere in
// the transcript still offering to add it.
func TestStickerPackShareCarriesWhatTheShareSaid(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := ingestBusiness(t, client, "pack1", &waE2E.Message{StickerPackMessage: &waE2E.StickerPackMessage{
		StickerPackID:   proto.String("pack-1"),
		Name:            proto.String("Cats being unhelpful"),
		Publisher:       proto.String("Nobody in particular"),
		PackDescription: proto.String("Twelve reasons not to work."),
		Stickers: []*waE2E.StickerPackMessage_Sticker{
			{FileName: proto.String("a.webp")},
			{FileName: proto.String("b.webp")},
		},
	}})
	if !ok {
		t.Fatal("a sticker pack share must ingest")
	}
	if input.MediaKind != appstore.MediaKindStickerPack {
		t.Fatalf("kind = %q", input.MediaKind)
	}
	payload := appstore.DecodePayload(input.PayloadJSON).StickerPack
	if payload == nil || payload.PackID != "pack-1" || payload.Count != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	if input.PayloadSummary != "Cats being unhelpful" {
		t.Fatalf("summary = %q", input.PayloadSummary)
	}
}
