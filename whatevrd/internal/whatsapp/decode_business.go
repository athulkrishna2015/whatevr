package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	appstore "whatevrd/internal/store"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

// interactiveCarouselCardLimit bounds a carousel. A card is not allowed to
// carry a carousel of its own, so one level is all there is, but the count is
// still capped: the daemon should not be talked into building an unbounded
// payload by a message it did not ask for.
const interactiveCarouselCardLimit = 12

func (d *Decoder) interactiveMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}

	payload, contextInfo, ok := interactivePayloadFromMessage(evt.Message)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, "", contextInfo)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	// The header picture is written now, after the base exists, because the
	// cache path is keyed on the row's own id. The thumbnails were carried
	// through the parse as bytes for exactly this reason.
	d.storeInteractiveThumbnails(chatID, base.ID, payload)

	if !payload.HasContent() {
		// A business message with no words, no picture and no buttons is a
		// husk. It still gets a row, because it is a real message somebody
		// sent, but it gets the tombstone rather than an empty card.
		return d.unsupportedMessageInput(ctx, evt, opts)
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Interactive: payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode interactive payload")
		return appstore.MediaMessageInput{}, false
	}
	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindInteractive,
		PayloadSummary:   interactiveSummary(payload),
	}, true
}

// storeInteractiveThumbnails writes every header picture the parse collected
// into the media cache and hands the payload the paths instead of the bytes.
//
// It runs after mediaInputBase rather than during the parse because a cache
// file is named for the row it belongs to, and until the base exists there is
// no id to name it after. Each carousel card gets its own suffix, so twelve
// slides do not overwrite each other twelve times.
func (d *Decoder) storeInteractiveThumbnails(chatID, messageID string, payload *appstore.InteractivePayload) {
	if payload == nil {
		return
	}
	payload.ThumbnailPath = d.saveMessageThumbnailWithExtension(chatID, messageID, payload.HeaderJPEG, ".card.jpg")
	payload.HeaderJPEG = nil
	for i := range payload.Cards {
		card := &payload.Cards[i]
		card.ThumbnailPath = d.saveMessageThumbnailWithExtension(
			chatID, messageID, card.HeaderJPEG, fmt.Sprintf(".card%d.jpg", i))
		card.HeaderJPEG = nil
	}
}

// interactivePayloadFromMessage picks whichever of the four shapes arrived and
// flattens it. The context info comes back alongside because each shape keeps
// it in a different place and the caller needs it for the reply quote.
func interactivePayloadFromMessage(message *waE2E.Message) (*appstore.InteractivePayload, *waE2E.ContextInfo, bool) {
	switch {
	case message.GetButtonsMessage() != nil:
		buttons := message.GetButtonsMessage()
		return interactiveFromButtons(buttons), buttons.GetContextInfo(), true
	case message.GetListMessage() != nil:
		list := message.GetListMessage()
		return interactiveFromList(list), list.GetContextInfo(), true
	case message.GetTemplateMessage() != nil:
		template := message.GetTemplateMessage()
		payload, ok := interactiveFromTemplate(template)
		if !ok {
			return nil, nil, false
		}
		return payload, template.GetContextInfo(), true
	case message.GetInteractiveMessage() != nil:
		interactive := message.GetInteractiveMessage()
		return interactiveFromInteractive(interactive, true), interactive.GetContextInfo(), true
	}
	return nil, nil, false
}

func interactiveFromButtons(msg *waE2E.ButtonsMessage) *appstore.InteractivePayload {
	payload := &appstore.InteractivePayload{
		Source: "buttons",
		Body:   strings.TrimSpace(msg.GetContentText()),
		Footer: strings.TrimSpace(msg.GetFooterText()),
	}
	// The header is a oneof: a line of text, or a piece of media whose
	// thumbnail is all we take.
	payload.Title = strings.TrimSpace(msg.GetText())
	applyInteractiveHeaderMedia(payload,
		msg.GetImageMessage().GetJPEGThumbnail(),
		msg.GetVideoMessage().GetJPEGThumbnail(),
		msg.GetDocumentMessage())

	for _, button := range msg.GetButtons() {
		if len(payload.Buttons) >= interactiveButtonLimit {
			break
		}
		if converted, ok := interactiveButtonFromButtons(button); ok {
			payload.Buttons = append(payload.Buttons, converted)
		}
	}
	return payload
}

func interactiveButtonFromButtons(button *waE2E.ButtonsMessage_Button) (appstore.InteractiveButton, bool) {
	if button == nil {
		return appstore.InteractiveButton{}, false
	}
	if flow := button.GetNativeFlowInfo(); flow != nil && flow.GetName() != "" {
		return nativeFlowButton(flow.GetName(), flow.GetParamsJSON())
	}
	label := strings.TrimSpace(button.GetButtonText().GetDisplayText())
	if label == "" {
		return appstore.InteractiveButton{}, false
	}
	// A response button sends its id back to the business, which is the one
	// thing whatevr cannot do for this family yet.
	return appstore.InteractiveButton{
		Kind:  appstore.InteractiveButtonReply,
		Label: label,
		ID:    strings.TrimSpace(button.GetButtonID()),
	}, true
}

func interactiveFromList(msg *waE2E.ListMessage) *appstore.InteractivePayload {
	payload := &appstore.InteractivePayload{
		Source:    "list",
		Title:     strings.TrimSpace(msg.GetTitle()),
		Body:      strings.TrimSpace(msg.GetDescription()),
		Footer:    strings.TrimSpace(msg.GetFooterText()),
		ListLabel: strings.TrimSpace(msg.GetButtonText()),
	}
	payload.Sections = interactiveSectionsFromList(msg.GetSections())

	// A product list carries ids rather than products; without the catalogue
	// behind them there is nothing to show but the picture it came with.
	payload.HeaderJPEG = msg.GetProductListInfo().GetHeaderImage().GetJPEGThumbnail()
	return payload
}

func interactiveSectionsFromList(sections []*waE2E.ListMessage_Section) []appstore.InteractiveSection {
	out := make([]appstore.InteractiveSection, 0, len(sections))
	rows := 0
	for _, section := range sections {
		if len(out) >= interactiveListSectionLimit || rows >= interactiveListRowLimit {
			break
		}
		converted := appstore.InteractiveSection{Title: strings.TrimSpace(section.GetTitle())}
		for _, row := range section.GetRows() {
			if rows >= interactiveListRowLimit {
				break
			}
			title := strings.TrimSpace(row.GetTitle())
			description := strings.TrimSpace(row.GetDescription())
			if title == "" && description == "" {
				continue
			}
			converted.Rows = append(converted.Rows, appstore.InteractiveRow{
				Title:       title,
				Description: description,
				ID:          strings.TrimSpace(row.GetRowID()),
			})
			rows++
		}
		if converted.Title == "" && len(converted.Rows) == 0 {
			continue
		}
		out = append(out, converted)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// interactiveFromTemplate handles the hydrated form only.
//
// The other form, FourRowTemplate, carries HighlyStructuredMessage values:
// a template name plus its parameters, to be looked up in a catalogue the
// business registered with WhatsApp and that a linked device is never sent.
// There is no honest way to render one, so it falls through to the tombstone
// rather than to a card full of placeholders.
func interactiveFromTemplate(msg *waE2E.TemplateMessage) (*appstore.InteractivePayload, bool) {
	// The interactive form of a template is an InteractiveMessage in a
	// different envelope, and reading it as one is the whole difference
	// between a card and a grey box.
	if interactive := msg.GetInteractiveMessageTemplate(); interactive != nil {
		payload := interactiveFromInteractive(interactive, true)
		payload.Source = "template"
		return payload, true
	}

	hydrated := msg.GetHydratedTemplate()
	if hydrated == nil {
		hydrated = msg.GetHydratedFourRowTemplate()
	}
	if hydrated == nil {
		return nil, false
	}

	payload := &appstore.InteractivePayload{
		Source: "template",
		Title:  strings.TrimSpace(hydrated.GetHydratedTitleText()),
		Body:   strings.TrimSpace(hydrated.GetHydratedContentText()),
		Footer: strings.TrimSpace(hydrated.GetHydratedFooterText()),
	}
	applyInteractiveHeaderMedia(payload,
		hydrated.GetImageMessage().GetJPEGThumbnail(),
		hydrated.GetVideoMessage().GetJPEGThumbnail(),
		hydrated.GetDocumentMessage())

	for _, button := range hydrated.GetHydratedButtons() {
		if len(payload.Buttons) >= interactiveButtonLimit {
			break
		}
		if converted, ok := interactiveButtonFromHydrated(button); ok {
			payload.Buttons = append(payload.Buttons, converted)
		}
	}
	return payload, true
}

func interactiveButtonFromHydrated(button *waE2E.HydratedTemplateButton) (appstore.InteractiveButton, bool) {
	if button == nil {
		return appstore.InteractiveButton{}, false
	}
	switch {
	case button.GetUrlButton() != nil:
		url := button.GetUrlButton()
		return urlButton(url.GetDisplayText(), url.GetURL())
	case button.GetCallButton() != nil:
		call := button.GetCallButton()
		return callButton(call.GetDisplayText(), call.GetPhoneNumber())
	case button.GetQuickReplyButton() != nil:
		reply := button.GetQuickReplyButton()
		label := strings.TrimSpace(reply.GetDisplayText())
		if label == "" {
			return appstore.InteractiveButton{}, false
		}
		return appstore.InteractiveButton{
			Kind:  appstore.InteractiveButtonReply,
			Label: label,
			ID:    strings.TrimSpace(reply.GetID()),
		}, true
	}
	return appstore.InteractiveButton{}, false
}

// interactiveFromInteractive reads the current shape. `top` is false for a
// carousel's cards, which is what stops a card from carrying a carousel of its
// own: one level is what the format has, and recursion without a floor is how a
// message becomes a denial of service.
func interactiveFromInteractive(msg *waE2E.InteractiveMessage, top bool) *appstore.InteractivePayload {
	payload := &appstore.InteractivePayload{
		Source:   "interactive",
		Body:     strings.TrimSpace(msg.GetBody().GetText()),
		Footer:   strings.TrimSpace(msg.GetFooter().GetText()),
		Title:    strings.TrimSpace(msg.GetHeader().GetTitle()),
		Subtitle: strings.TrimSpace(msg.GetHeader().GetSubtitle()),
	}

	header := msg.GetHeader()
	applyInteractiveHeaderMedia(payload,
		header.GetImageMessage().GetJPEGThumbnail(),
		header.GetVideoMessage().GetJPEGThumbnail(),
		header.GetDocumentMessage())
	// A header may also carry a bare JPEG with no message around it.
	if len(payload.HeaderJPEG) == 0 {
		payload.HeaderJPEG = header.GetJPEGThumbnail()
	}
	// A product header names what is being offered; the price and the catalogue
	// live in the product message the commerce path handles.
	if product := header.GetProductMessage().GetProduct(); product != nil {
		if payload.Title == "" {
			payload.Title = strings.TrimSpace(product.GetTitle())
		}
		if payload.Subtitle == "" {
			payload.Subtitle = strings.TrimSpace(product.GetDescription())
		}
	}

	if flow := msg.GetNativeFlowMessage(); flow != nil {
		for _, button := range flow.GetButtons() {
			if len(payload.Buttons) >= interactiveButtonLimit {
				break
			}
			converted, ok := nativeFlowButton(button.GetName(), button.GetButtonParamsJSON())
			if !ok {
				continue
			}
			// A single-select is a list wearing a button's clothes: its rows
			// are the message, so they are lifted out rather than left inside
			// a button nobody can press.
			if sections := nativeFlowSections(button.GetName(), button.GetButtonParamsJSON()); len(sections) > 0 {
				payload.Sections = append(payload.Sections, sections...)
				if payload.ListLabel == "" {
					payload.ListLabel = converted.Label
				}
				continue
			}
			payload.Buttons = append(payload.Buttons, converted)
		}
	}

	if carousel := msg.GetCarouselMessage(); carousel != nil && top {
		payload.Source = "carousel"
		for _, card := range carousel.GetCards() {
			if len(payload.Cards) >= interactiveCarouselCardLimit {
				break
			}
			converted := interactiveFromInteractive(card, false)
			if !converted.HasContent() {
				continue
			}
			// A slide does not name its own wire shape: the carousel around it
			// already did, and repeating it on every card is noise.
			converted.Source = ""
			payload.Cards = append(payload.Cards, *converted)
		}
	}

	return payload
}

// applyInteractiveHeaderMedia takes whichever header media a shape carried. The
// pictures come in as bytes and are written to the cache later, once the row
// has an id to be keyed on.
func applyInteractiveHeaderMedia(payload *appstore.InteractivePayload, imageJPEG, videoJPEG []byte, document *waE2E.DocumentMessage) {
	switch {
	case len(imageJPEG) > 0:
		payload.HeaderJPEG = imageJPEG
	case len(videoJPEG) > 0:
		payload.HeaderJPEG = videoJPEG
	}
	if document == nil {
		return
	}
	name := strings.TrimSpace(document.GetFileName())
	if name == "" {
		name = strings.TrimSpace(document.GetTitle())
	}
	payload.DocumentName = name
	if len(payload.HeaderJPEG) == 0 {
		payload.HeaderJPEG = document.GetJPEGThumbnail()
	}
}

// nativeFlowButton reads one of WhatsApp's native flows.
//
// A native flow is a name plus a blob of JSON that WhatsApp never documented,
// and the blob is where the label lives: a button parsed without it renders as
// "cta_url" or as nothing. The known names are handled by name; everything else
// keeps whatever text the blob had and says out loud that it cannot be pressed,
// which is the honest rendering of a booking form or a payment sheet.
func nativeFlowButton(name, paramsJSON string) (appstore.InteractiveButton, bool) {
	name = strings.TrimSpace(name)
	params := decodeNativeFlowParams(paramsJSON)
	label := nativeFlowLabel(name, params)

	// A recognised flow that will not build is dropped rather than demoted to a
	// dead button. A link button whose link we refused is not a button somebody
	// cannot press yet: it is a link we are not going to open, and drawing it
	// greyed out would say the wrong thing about why.
	switch name {
	case "cta_url", "open_webview":
		return urlButton(label, nativeFlowString(params, "url", "merchant_url", "webview_url"))
	case "cta_call", "call_permission_request":
		return callButton(label, nativeFlowString(params, "phone_number", "phone", "id"))
	case "cta_copy":
		code := nativeFlowString(params, "copy_code", "code")
		if code == "" {
			return appstore.InteractiveButton{}, false
		}
		return appstore.InteractiveButton{
			Kind:  appstore.InteractiveButtonCopy,
			Label: label,
			Copy:  code,
			Live:  true,
		}, true
	case "quick_reply", "cta_reminder", "cta_cancel_reminder":
		if label == "" {
			return appstore.InteractiveButton{}, false
		}
		return appstore.InteractiveButton{
			Kind:  appstore.InteractiveButtonReply,
			Label: label,
			ID:    nativeFlowString(params, "id"),
		}, true
	}

	if label == "" {
		return appstore.InteractiveButton{}, false
	}
	return appstore.InteractiveButton{Kind: appstore.InteractiveButtonOther, Label: label}, true
}

// nativeFlowSections lifts a single-select flow's menu out of its button. The
// rows are the message in that case, not a detail of a control.
func nativeFlowSections(name, paramsJSON string) []appstore.InteractiveSection {
	if strings.TrimSpace(name) != "single_select" {
		return nil
	}
	var parsed struct {
		Sections []struct {
			Title string `json:"title"`
			Rows  []struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				ID          string `json:"id"`
			} `json:"rows"`
		} `json:"sections"`
	}
	if err := json.Unmarshal([]byte(paramsJSON), &parsed); err != nil {
		return nil
	}

	out := make([]appstore.InteractiveSection, 0, len(parsed.Sections))
	rows := 0
	for _, section := range parsed.Sections {
		if len(out) >= interactiveListSectionLimit || rows >= interactiveListRowLimit {
			break
		}
		converted := appstore.InteractiveSection{Title: strings.TrimSpace(section.Title)}
		for _, row := range section.Rows {
			if rows >= interactiveListRowLimit {
				break
			}
			title := strings.TrimSpace(row.Title)
			description := strings.TrimSpace(row.Description)
			if title == "" && description == "" {
				continue
			}
			converted.Rows = append(converted.Rows, appstore.InteractiveRow{
				Title:       title,
				Description: description,
				ID:          strings.TrimSpace(row.ID),
			})
			rows++
		}
		if converted.Title == "" && len(converted.Rows) == 0 {
			continue
		}
		out = append(out, converted)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeNativeFlowParams(paramsJSON string) map[string]any {
	paramsJSON = strings.TrimSpace(paramsJSON)
	if paramsJSON == "" {
		return nil
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return nil
	}
	return params
}

// nativeFlowLabel finds the words on the button. Every flow spells it
// differently, and a button with no label at all falls back to its own name
// made readable, because "Review and pay" is still better than nothing.
func nativeFlowLabel(name string, params map[string]any) string {
	if label := nativeFlowString(params, "display_text", "title", "flow_cta", "header", "text", "button_text"); label != "" {
		return label
	}
	return prettifyFlowName(name)
}

func nativeFlowString(params map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := params[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case float64:
			// A phone number arriving as a number rather than a string is a
			// thing that happens, and dropping it would leave a dead button.
			return strings.TrimSuffix(fmt.Sprintf("%.0f", typed), ".0")
		}
	}
	return ""
}

// prettifyFlowName turns "review_and_pay" into "Review and pay".
func prettifyFlowName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// urlButton builds a link button, or refuses. Only http and https are accepted:
// a button is a thing somebody presses without reading, and handing an
// arbitrary scheme to the desktop is handing it to whatever claims that scheme.
func urlButton(label, raw string) (appstore.InteractiveButton, bool) {
	canonical, host := linkPreviewURL(raw)
	if canonical == "" {
		return appstore.InteractiveButton{}, false
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = host
	}
	return appstore.InteractiveButton{
		Kind:  appstore.InteractiveButtonURL,
		Label: label,
		URL:   canonical,
		Live:  true,
	}, true
}

// callButton builds a dial button. The number is reduced to what a tel: URI may
// contain, so a "phone number" carrying anything else cannot become one.
func callButton(label, raw string) (appstore.InteractiveButton, bool) {
	phone := sanitizePhoneNumber(raw)
	if phone == "" {
		return appstore.InteractiveButton{}, false
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = phone
	}
	return appstore.InteractiveButton{
		Kind:  appstore.InteractiveButtonCall,
		Label: label,
		Phone: phone,
		Live:  true,
	}, true
}

// sanitizePhoneNumber keeps digits, and a plus only in the lead. Everything
// else a sender might have put in the field is dropped rather than passed on to
// the desktop's tel: handler.
func sanitizePhoneNumber(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for i, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		}
	}
	phone := b.String()
	if len(strings.TrimPrefix(phone, "+")) < 3 {
		return ""
	}
	return phone
}

// interactiveSummary is the one-line rendering: what the message says, falling
// back through everything else it has. It is never empty, because a chat list
// row reading nothing at all is worse than one reading "Message".
func interactiveSummary(payload *appstore.InteractivePayload) string {
	if payload == nil {
		return "Message"
	}
	for _, candidate := range []string{payload.Body, payload.Title, payload.Subtitle, payload.Footer, payload.DocumentName} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	for _, card := range payload.Cards {
		if summary := interactiveSummary(&card); summary != "Message" {
			return summary
		}
	}
	if len(payload.Buttons) > 0 && payload.Buttons[0].Label != "" {
		return payload.Buttons[0].Label
	}
	return "Message"
}

// interactiveResponseText renders the four "somebody pressed a button" shapes
// as the line they amount to.
//
// They are not cards. A response is a message whose whole content is the words
// on the button that was tapped, which is exactly what WhatsApp shows for one,
// so it lands as an ordinary text row and every path that handles text handles
// it: search, quoting, copying, the chat-list preview.
func interactiveResponseText(message *waE2E.Message) string {
	if message == nil {
		return ""
	}
	if reply := message.GetTemplateButtonReplyMessage(); reply != nil {
		return strings.TrimSpace(reply.GetSelectedDisplayText())
	}
	if reply := message.GetButtonsResponseMessage(); reply != nil {
		return strings.TrimSpace(reply.GetSelectedDisplayText())
	}
	if reply := message.GetListResponseMessage(); reply != nil {
		if title := strings.TrimSpace(reply.GetTitle()); title != "" {
			return title
		}
		return strings.TrimSpace(reply.GetDescription())
	}
	if reply := message.GetInteractiveResponseMessage(); reply != nil {
		return strings.TrimSpace(reply.GetBody().GetText())
	}
	return ""
}

// businessContextInfo finds the reply quote and the @-mentions on every shape
// this file and its commerce neighbour handle. Without it a business message
// answering one of yours would lose the quote, and a mention inside one would
// lose its name.
//
// It is spelled out rather than routed through the parsers next door because
// this runs on every message that reaches ingest, quoted messages included, and
// building a whole payload to read one field off it is not what that is for.
func businessContextInfo(message *waE2E.Message) *waE2E.ContextInfo {
	if message == nil {
		return nil
	}
	switch {
	case message.GetButtonsMessage() != nil:
		return message.GetButtonsMessage().GetContextInfo()
	case message.GetListMessage() != nil:
		return message.GetListMessage().GetContextInfo()
	case message.GetTemplateMessage() != nil:
		return message.GetTemplateMessage().GetContextInfo()
	case message.GetInteractiveMessage() != nil:
		return message.GetInteractiveMessage().GetContextInfo()
	case message.GetProductMessage() != nil:
		return message.GetProductMessage().GetContextInfo()
	case message.GetOrderMessage() != nil:
		return message.GetOrderMessage().GetContextInfo()
	case message.GetStickerPackMessage() != nil:
		return message.GetStickerPackMessage().GetContextInfo()
	case message.GetTemplateButtonReplyMessage() != nil:
		return message.GetTemplateButtonReplyMessage().GetContextInfo()
	case message.GetButtonsResponseMessage() != nil:
		return message.GetButtonsResponseMessage().GetContextInfo()
	case message.GetListResponseMessage() != nil:
		return message.GetListResponseMessage().GetContextInfo()
	case message.GetInteractiveResponseMessage() != nil:
		return message.GetInteractiveResponseMessage().GetContextInfo()
	}
	return nil
}

func (d *Decoder) commerceMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}

	payload, contextInfo, jpeg, ok := commercePayloadFromMessage(evt.Message)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := d.mediaInputBase(ctx, evt, opts, "", contextInfo)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}
	payload.ThumbnailPath = d.saveMessageThumbnailWithExtension(chatID, base.ID, jpeg, ".commerce.jpg")

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Commerce: payload})
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode commerce payload")
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

func (d *Decoder) stickerPackMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	pack := evt.Message.GetStickerPackMessage()
	if pack == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := d.mediaInputBase(ctx, evt, opts, "", pack.GetContextInfo())
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
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode sticker pack payload")
		return appstore.MediaMessageInput{}, false
	}
	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindStickerPack,
		PayloadSummary:   payload.Name,
	}, true
}

func (d *Decoder) callLogMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	// The accessor is misspelled on the wire type. It is whatsmeow's field name
	// generated from WhatsApp's own proto, not a typo to fix here.
	log := evt.Message.GetCallLogMesssage()
	if log == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := d.mediaInputBase(ctx, evt, opts, "", nil)
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
		zerolog.Ctx(ctx).Warn().Err(err).Str("msg", base.ID).Msg("encode call log payload")
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

// past these a list is not a menu anybody reads
const (
	interactiveListSectionLimit = 12
	interactiveListRowLimit     = 40
	interactiveButtonLimit      = 12
)
