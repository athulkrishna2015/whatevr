package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// Commerce, sticker packs and call logs.
//
// The first three (a product, an order, a payment) are one card wearing three
// wire shapes: a picture, a name, a sum of money and a line saying where it
// stands. They are flattened into CommercePayload for the same reason the
// business messages next door are flattened, and the money crosses as the
// integer WhatsApp sent rather than as text, because formatting a sum is a
// question about the reader and the daemon does not know the reader's locale.
//
// The other two are here because they arrive through the same door. A shared
// sticker pack is the only card in the family that does something: whatevr
// already keeps a sticker library, so the pack is a real offer rather than a
// description of one. A call log is not a message at all, which is why it ends
// up as a centered pill rather than in somebody's bubble.

func (c *Client) commerceMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}

	payload, contextInfo, jpeg, ok := commercePayloadFromMessage(evt.Message)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := c.mediaInputBase(ctx, evt, opts, "", contextInfo)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}
	payload.ThumbnailPath = c.saveMessageThumbnailWithExtension(chatID, base.ID, jpeg, ".commerce.jpg")

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Commerce: payload})
	if err != nil {
		c.log.Warnf("Failed to encode commerce payload for %s: %v", base.ID, err)
		return appstore.MediaMessageInput{}, false
	}
	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        commerceMediaKind(payload.Kind),
		PayloadSummary:   commerceSummary(payload),
	}, true
}

// commerceMediaKind maps the five commerce shapes onto the three kinds the wire
// has. The three payment shapes share one kind because they share one card: the
// difference between asking, paying and being invited to pay is a line on it,
// not a different thing to draw.
func commerceMediaKind(kind string) string {
	switch kind {
	case appstore.CommerceKindProduct:
		return appstore.MediaKindProduct
	case appstore.CommerceKindOrder:
		return appstore.MediaKindOrder
	default:
		return appstore.MediaKindPayment
	}
}

// commercePayloadFromMessage picks whichever commerce shape arrived. The inline
// picture comes back separately because it is written to the cache by the
// caller, which is the only one that knows the row's id.
func commercePayloadFromMessage(message *waE2E.Message) (*appstore.CommercePayload, *waE2E.ContextInfo, []byte, bool) {
	switch {
	case message.GetProductMessage() != nil:
		msg := message.GetProductMessage()
		payload, jpeg := commerceFromProduct(msg)
		return payload, msg.GetContextInfo(), jpeg, true
	case message.GetOrderMessage() != nil:
		msg := message.GetOrderMessage()
		payload := commerceFromOrder(msg)
		return payload, msg.GetContextInfo(), msg.GetThumbnail(), true
	case message.GetRequestPaymentMessage() != nil:
		return commerceFromPaymentRequest(message.GetRequestPaymentMessage()), nil, nil, true
	case message.GetSendPaymentMessage() != nil:
		return commerceFromPaymentSent(message.GetSendPaymentMessage()), nil, nil, true
	case message.GetPaymentInviteMessage() != nil:
		return commerceFromPaymentInvite(message.GetPaymentInviteMessage()), nil, nil, true
	}
	return nil, nil, nil, false
}

func commerceFromProduct(msg *waE2E.ProductMessage) (*appstore.CommercePayload, []byte) {
	payload := &appstore.CommercePayload{
		Kind:      appstore.CommerceKindProduct,
		Body:      strings.TrimSpace(msg.GetBody()),
		Footer:    strings.TrimSpace(msg.GetFooter()),
		SellerJID: strings.TrimSpace(msg.GetBusinessOwnerJID()),
	}

	if product := msg.GetProduct(); product != nil {
		payload.Title = strings.TrimSpace(product.GetTitle())
		payload.Description = strings.TrimSpace(product.GetDescription())
		payload.ProductID = strings.TrimSpace(product.GetProductID())
		payload.RetailerID = strings.TrimSpace(product.GetRetailerID())
		payload.Amount1000 = product.GetPriceAmount1000()
		payload.SalePrice1000 = product.GetSalePriceAmount1000()
		payload.Currency = strings.ToUpper(strings.TrimSpace(product.GetCurrencyCode()))
		payload.ImageCount = int(product.GetProductImageCount())
		// Only http(s) survives: a product URL becomes a button, and a button
		// is a thing people press without reading.
		payload.URL, _ = linkPreviewURL(product.GetURL())
		return payload, product.GetProductImage().GetJPEGThumbnail()
	}

	// A catalogue share carries no product at all: it is an invitation to
	// browse, and its own picture and title are the whole card.
	if catalog := msg.GetCatalog(); catalog != nil {
		payload.Title = strings.TrimSpace(catalog.GetTitle())
		payload.Description = strings.TrimSpace(catalog.GetDescription())
		return payload, catalog.GetCatalogImage().GetJPEGThumbnail()
	}
	return payload, nil
}

func commerceFromOrder(msg *waE2E.OrderMessage) *appstore.CommercePayload {
	return &appstore.CommercePayload{
		Kind:        appstore.CommerceKindOrder,
		Title:       strings.TrimSpace(msg.GetOrderTitle()),
		Description: strings.TrimSpace(msg.GetMessage()),
		OrderID:     strings.TrimSpace(msg.GetOrderID()),
		ItemCount:   int(msg.GetItemCount()),
		Status:      orderStatusName(msg.GetStatus()),
		Amount1000:  msg.GetTotalAmount1000(),
		Currency:    strings.ToUpper(strings.TrimSpace(msg.GetTotalCurrencyCode())),
		SellerJID:   strings.TrimSpace(msg.GetSellerJID()),
	}
}

func orderStatusName(status waE2E.OrderMessage_OrderStatus) string {
	switch status {
	case waE2E.OrderMessage_INQUIRY:
		return "inquiry"
	case waE2E.OrderMessage_ACCEPTED:
		return "accepted"
	case waE2E.OrderMessage_DECLINED:
		return "declined"
	default:
		return ""
	}
}

func commerceFromPaymentRequest(msg *waE2E.RequestPaymentMessage) *appstore.CommercePayload {
	payload := &appstore.CommercePayload{
		Kind:          appstore.CommerceKindPaymentRequest,
		Amount1000:    int64(msg.GetAmount1000()),
		Currency:      strings.ToUpper(strings.TrimSpace(msg.GetCurrencyCodeIso4217())),
		RequestedFrom: strings.TrimSpace(msg.GetRequestFrom()),
		ExpiresAt:     msg.GetExpiryTimestamp(),
		// The note is an ordinary message nested inside the request, which is
		// where "for dinner" lives.
		Note: strings.TrimSpace(textFromMessage(msg.GetNoteMessage())),
	}
	// The newer Money field is authoritative when both are present: the flat
	// pair above predates it and is not always filled in.
	if money := msg.GetAmount(); money != nil {
		if value := money.GetValue(); value != 0 {
			payload.Amount1000 = paymentAmountToThousandths(value, money.GetOffset())
		}
		if code := strings.ToUpper(strings.TrimSpace(money.GetCurrencyCode())); code != "" {
			payload.Currency = code
		}
	}
	return payload
}

// paymentAmountToThousandths converts a Money into the thousandths every other
// amount in this family uses, so a card has one number to render rather than a
// number and a scale to apply to it.
//
// A Money is an integer and the divisor it was scaled by: {1299, 100} is 12.99.
// The rest of the proto states amounts in thousandths already, which is the
// same statement with the divisor fixed at 1000.
func paymentAmountToThousandths(value int64, offset uint32) int64 {
	if offset == 0 {
		// Nothing to divide by means the value is already whole units.
		return value * 1000
	}
	return value * 1000 / int64(offset)
}

func commerceFromPaymentSent(msg *waE2E.SendPaymentMessage) *appstore.CommercePayload {
	return &appstore.CommercePayload{
		Kind: appstore.CommerceKindPaymentSent,
		Note: strings.TrimSpace(textFromMessage(msg.GetNoteMessage())),
	}
}

func commerceFromPaymentInvite(msg *waE2E.PaymentInviteMessage) *appstore.CommercePayload {
	return &appstore.CommercePayload{
		Kind:      appstore.CommerceKindPaymentInvite,
		Service:   paymentServiceName(msg.GetServiceType()),
		ExpiresAt: msg.GetExpiryTimestamp(),
	}
}

func paymentServiceName(service waE2E.PaymentInviteMessage_ServiceType) string {
	switch service {
	case waE2E.PaymentInviteMessage_FBPAY:
		return "fbpay"
	case waE2E.PaymentInviteMessage_NOVI:
		return "novi"
	case waE2E.PaymentInviteMessage_UPI:
		return "upi"
	case waE2E.PaymentInviteMessage_PIX:
		return "pix"
	default:
		return ""
	}
}

// commerceSummary is the detail on the one-line rendering, after the kind's own
// label: "🛍️ Product: Blue mug", "🧾 Order: 3 items".
func commerceSummary(payload *appstore.CommercePayload) string {
	if payload == nil {
		return ""
	}
	switch payload.Kind {
	case appstore.CommerceKindOrder:
		if payload.Title != "" {
			return payload.Title
		}
		if payload.ItemCount == 1 {
			return "1 item"
		}
		if payload.ItemCount > 1 {
			return fmt.Sprintf("%d items", payload.ItemCount)
		}
	case appstore.CommerceKindPaymentRequest:
		return "requested"
	case appstore.CommerceKindPaymentSent:
		return "sent"
	case appstore.CommerceKindPaymentInvite:
		return "invitation"
	}
	for _, candidate := range []string{payload.Title, payload.Description, payload.Body, payload.Note} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c *Client) stickerPackMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	pack := evt.Message.GetStickerPackMessage()
	if pack == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := c.mediaInputBase(ctx, evt, opts, "", pack.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.StickerPackPayload{
		PackID:      strings.TrimSpace(pack.GetStickerPackID()),
		Name:        strings.TrimSpace(pack.GetName()),
		Publisher:   strings.TrimSpace(pack.GetPublisher()),
		Description: strings.TrimSpace(pack.GetPackDescription()),
		Caption:     strings.TrimSpace(pack.GetCaption()),
		Count:       len(pack.GetStickers()),
	}
	if payload.Count == 0 {
		payload.Count = int(pack.GetStickerPackSize())
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{StickerPack: payload})
	if err != nil {
		c.log.Warnf("Failed to encode sticker pack payload for %s: %v", base.ID, err)
		return appstore.MediaMessageInput{}, false
	}
	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindStickerPack,
		PayloadSummary:   payload.Name,
	}, true
}

func (c *Client) callLogMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	// The accessor is misspelled on the wire type. It is whatsmeow's field name
	// generated from WhatsApp's own proto, not a typo to fix here.
	log := evt.Message.GetCallLogMesssage()
	if log == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := c.mediaInputBase(ctx, evt, opts, "", nil)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.CallLogPayload{
		Video:        log.GetIsVideo(),
		Outcome:      callOutcomeName(log.GetCallOutcome()),
		DurationSecs: log.GetDurationSecs(),
		Participants: len(log.GetParticipants()),
		Scheduled:    log.GetCallType() == waE2E.CallLogMessage_SCHEDULED_CALL,
		VoiceChat:    log.GetCallType() == waE2E.CallLogMessage_VOICE_CHAT,
	}
	// Two participants is the pair on the call, which is not a group call. The
	// distinction is worth drawing: "missed group call" is a different thing to
	// have missed.
	payload.Group = payload.Participants > 2 || evt.Info.IsGroup

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{CallLog: payload})
	if err != nil {
		c.log.Warnf("Failed to encode call log payload for %s: %v", base.ID, err)
		return appstore.MediaMessageInput{}, false
	}
	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindCallLog,
		PayloadSummary:   callLogSummary(payload, base.Direction == appstore.DirectionOutgoing),
	}, true
}

func callOutcomeName(outcome waE2E.CallLogMessage_CallOutcome) string {
	switch outcome {
	case waE2E.CallLogMessage_CONNECTED:
		return appstore.CallOutcomeConnected
	case waE2E.CallLogMessage_MISSED:
		return appstore.CallOutcomeMissed
	case waE2E.CallLogMessage_FAILED:
		return appstore.CallOutcomeFailed
	case waE2E.CallLogMessage_REJECTED:
		return appstore.CallOutcomeRejected
	case waE2E.CallLogMessage_ACCEPTED_ELSEWHERE:
		return appstore.CallOutcomeElsewhere
	case waE2E.CallLogMessage_ONGOING:
		return appstore.CallOutcomeOngoing
	case waE2E.CallLogMessage_SILENCED_BY_DND, waE2E.CallLogMessage_SILENCED_UNKNOWN_CALLER:
		return appstore.CallOutcomeSilenced
	default:
		return ""
	}
}

// callLogSummary is the whole one-line rendering for a call, glyph aside: the
// kind contributes no label of its own, because "Call: missed" says less than
// "Missed video call" and takes longer to read.
func callLogSummary(payload *appstore.CallLogPayload, outgoing bool) string {
	if payload == nil {
		return ""
	}
	medium := "voice call"
	if payload.Video {
		medium = "video call"
	}
	if payload.Group {
		medium = "group " + medium
	}

	switch payload.Outcome {
	case appstore.CallOutcomeMissed, appstore.CallOutcomeSilenced:
		// A call you did not answer is missed; one they did not answer went
		// unanswered. Same event, two sides, and calling both "missed" reads as
		// an accusation in the wrong direction.
		if outgoing {
			return "Unanswered " + medium
		}
		return "Missed " + medium
	case appstore.CallOutcomeRejected:
		return "Declined " + medium
	case appstore.CallOutcomeFailed:
		return "Failed " + medium
	case appstore.CallOutcomeOngoing:
		return "Ongoing " + medium
	case appstore.CallOutcomeElsewhere:
		return "Answered on another device"
	}

	line := strings.ToUpper(medium[:1]) + medium[1:]
	if payload.DurationSecs > 0 {
		line += fmt.Sprintf(" (%d:%02d)", payload.DurationSecs/60, payload.DurationSecs%60)
	}
	return line
}
