package wa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// Business messages: the family a company's WhatsApp account sends when it
// wants you to press something.
//
// WhatsApp has four wire shapes for one idea. ButtonsMessage is the oldest,
// ListMessage is the same thing with a menu instead of buttons, TemplateMessage
// wraps either in an approved template, and InteractiveMessage is the current
// one and nests itself for carousels. They say the same things in different
// field names, so ingest flattens them into a single InteractivePayload and the
// frontend draws one card.
//
// Alongside them ride the commerce kinds (a product, an order, a payment) and
// two things that are neither: a shared sticker pack, and a call log. Those
// last two are here because they arrive through the same door, not because they
// have anything to do with business.
//
// Nothing in this file fetches anything. A header that arrived as an
// ImageMessage contributes the JPEG it carried inline and no more: these are
// promotional headers rather than pictures somebody sent you, and making the
// kind downloadable would put marketing broadcasts on the auto-download path.

// interactiveCarouselCardLimit bounds a carousel. A card is not allowed to
// carry a carousel of its own, so one level is all there is, but the count is
// still capped: the daemon should not be talked into building an unbounded
// payload by a message it did not ask for.
const interactiveCarouselCardLimit = 12

// interactiveListRowLimit bounds a list for the same reason. Anything past this
// is not a menu somebody is going to read.
const (
	interactiveListSectionLimit = 12
	interactiveListRowLimit     = 40
	interactiveButtonLimit      = 12
)

func (c *Client) interactiveMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}

	payload, contextInfo, ok := interactivePayloadFromMessage(evt.Message)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := c.mediaInputBase(ctx, evt, opts, "", contextInfo)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	// The header picture is written now, after the base exists, because the
	// cache path is keyed on the row's own id. The thumbnails were carried
	// through the parse as bytes for exactly this reason.
	c.storeInteractiveThumbnails(chatID, base.ID, payload)

	if !payload.HasContent() {
		// A business message with no words, no picture and no buttons is a
		// husk. It still gets a row, because it is a real message somebody
		// sent, but it gets the tombstone rather than an empty card.
		return c.unsupportedMessageInput(ctx, evt, opts)
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Interactive: payload})
	if err != nil {
		c.log.Warnf("Failed to encode interactive payload for %s: %v", base.ID, err)
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
func (c *Client) storeInteractiveThumbnails(chatID, messageID string, payload *appstore.InteractivePayload) {
	if payload == nil {
		return
	}
	payload.ThumbnailPath = c.saveMessageThumbnailWithExtension(chatID, messageID, payload.HeaderJPEG, ".card.jpg")
	payload.HeaderJPEG = nil
	for i := range payload.Cards {
		card := &payload.Cards[i]
		card.ThumbnailPath = c.saveMessageThumbnailWithExtension(
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
