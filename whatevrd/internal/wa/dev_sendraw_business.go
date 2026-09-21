package wa

import (
	"context"
	"encoding/json"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// Development-only builders for the business, commerce and call-log kinds. See
// dev_sendraw.go: this is the half of that file that would have made it twice
// as long, and it is reachable only when WHATEVR_DEV_COMMANDS=1.
//
// These kinds cannot be produced by a phone at all. Nobody's WhatsApp app sends
// a hydrated template or an order; a business platform account does, and there
// is no way to ask one to send a test message on demand. So the only way to see
// what the ingest and the card do with one is to build the proto WhatsApp would
// have delivered and push it through the real handler.

func buildRawButtons(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Text    string   `json:"text"`
		Title   string   `json:"title"`
		Footer  string   `json:"footer"`
		Buttons []string `json:"buttons"`
		Thumb   bool     `json:"thumb"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Text == "" {
		p.Text = "Your table for four is held until 8pm. Anything else we can do?"
	}
	if p.Title == "" {
		p.Title = "Koshy's"
	}
	if p.Footer == "" {
		p.Footer = "Replies are handled by a human between 9am and 9pm"
	}
	if len(p.Buttons) == 0 {
		p.Buttons = []string{"Confirm", "Change the time", "Cancel"}
	}

	msg := &waE2E.ButtonsMessage{
		ContentText: proto.String(p.Text),
		FooterText:  proto.String(p.Footer),
		HeaderType:  waE2E.ButtonsMessage_TEXT.Enum(),
		Header:      &waE2E.ButtonsMessage_Text{Text: p.Title},
	}
	if p.Thumb {
		picture, err := generateTestPicture(3, 4)
		if err != nil {
			return nil, err
		}
		msg.HeaderType = waE2E.ButtonsMessage_IMAGE.Enum()
		msg.Header = &waE2E.ButtonsMessage_ImageMessage{
			ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: outgoingImageThumbnail(picture)},
		}
	}
	for i, label := range p.Buttons {
		msg.Buttons = append(msg.Buttons, &waE2E.ButtonsMessage_Button{
			ButtonID:   proto.String(rawButtonID(i)),
			ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String(label)},
			Type:       waE2E.ButtonsMessage_Button_RESPONSE.Enum(),
		})
	}
	return &waE2E.Message{ButtonsMessage: msg}, nil
}

func buildRawList(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		ButtonText  string `json:"button_text"`
		Footer      string `json:"footer"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Title == "" {
		p.Title = "Today's menu"
	}
	if p.Description == "" {
		p.Description = "Everything is served until we run out, which on a Friday is early."
	}
	if p.ButtonText == "" {
		p.ButtonText = "See the menu"
	}
	if p.Footer == "" {
		p.Footer = "Prices include tax"
	}

	return &waE2E.Message{ListMessage: &waE2E.ListMessage{
		Title:       proto.String(p.Title),
		Description: proto.String(p.Description),
		ButtonText:  proto.String(p.ButtonText),
		FooterText:  proto.String(p.Footer),
		ListType:    waE2E.ListMessage_SINGLE_SELECT.Enum(),
		Sections: []*waE2E.ListMessage_Section{
			{
				Title: proto.String("Breakfast"),
				Rows: []*waE2E.ListMessage_Row{
					{RowID: proto.String("b1"), Title: proto.String("Masala dosa"), Description: proto.String("With coconut chutney and sambar")},
					{RowID: proto.String("b2"), Title: proto.String("Idli vada"), Description: proto.String("Two of each")},
				},
			},
			{
				Title: proto.String("All day"),
				Rows: []*waE2E.ListMessage_Row{
					{RowID: proto.String("a1"), Title: proto.String("Filter coffee"), Description: proto.String("Strong, in a steel tumbler")},
					{RowID: proto.String("a2"), Title: proto.String("Rava kesari")},
				},
			},
		},
	}}, nil
}

func buildRawTemplate(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Title  string `json:"title"`
		Body   string `json:"body"`
		Footer string `json:"footer"`
		URL    string `json:"url"`
		Phone  string `json:"phone"`
		// NoThumb drops the header picture, which is the layout a plain
		// notification template actually gets.
		NoThumb bool `json:"no_thumb"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Title == "" {
		p.Title = "Your parcel is out for delivery"
	}
	if p.Body == "" {
		p.Body = "BLR-4471 left the Koramangala hub at 08:12 and is the fourth stop on today's route."
	}
	if p.Footer == "" {
		p.Footer = "Sent by Bluedart"
	}
	if p.URL == "" {
		p.URL = "https://www.bluedart.com/tracking"
	}
	if p.Phone == "" {
		p.Phone = "+911234567890"
	}

	hydrated := &waE2E.TemplateMessage_HydratedFourRowTemplate{
		HydratedContentText: proto.String(p.Body),
		HydratedFooterText:  proto.String(p.Footer),
		Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{HydratedTitleText: p.Title},
		HydratedButtons: []*waE2E.HydratedTemplateButton{
			{
				Index: proto.Uint32(0),
				HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{
					UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{
						DisplayText: proto.String("Track parcel"),
						URL:         proto.String(p.URL),
					},
				},
			},
			{
				Index: proto.Uint32(1),
				HydratedButton: &waE2E.HydratedTemplateButton_CallButton{
					CallButton: &waE2E.HydratedTemplateButton_HydratedCallButton{
						DisplayText: proto.String("Call the driver"),
						PhoneNumber: proto.String(p.Phone),
					},
				},
			},
			{
				Index: proto.Uint32(2),
				HydratedButton: &waE2E.HydratedTemplateButton_QuickReplyButton{
					QuickReplyButton: &waE2E.HydratedTemplateButton_HydratedQuickReplyButton{
						DisplayText: proto.String("Leave with a neighbour"),
						ID:          proto.String("neighbour"),
					},
				},
			},
		},
	}
	if !p.NoThumb {
		picture, err := generateTestPicture(3, 4)
		if err != nil {
			return nil, err
		}
		hydrated.Title = &waE2E.TemplateMessage_HydratedFourRowTemplate_ImageMessage{
			ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: outgoingImageThumbnail(picture)},
		}
		hydrated.HydratedContentText = proto.String(p.Title + "\n\n" + p.Body)
	}

	return &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
		Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{HydratedFourRowTemplate: hydrated},
	}}, nil
}

func buildRawInteractive(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Body     string `json:"body"`
		Footer   string `json:"footer"`
		// Carousel produces the nesting case: several cards in one message.
		Carousel int `json:"carousel"`
		// Select produces the native-flow single-select, whose menu lives
		// inside a button's JSON blob rather than anywhere a parser would
		// naturally look.
		Select bool `json:"select"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Title == "" {
		p.Title = "Renew your pass"
	}
	if p.Body == "" {
		p.Body = "Your monthly pass expires on the 31st. Renewing now keeps the same seat."
	}
	if p.Footer == "" {
		p.Footer = "Namma Metro"
	}

	if p.Carousel > 0 {
		carousel := &waE2E.InteractiveMessage_CarouselMessage{}
		for i := 0; i < p.Carousel; i++ {
			picture, err := generateTestPicture(i, p.Carousel)
			if err != nil {
				return nil, err
			}
			carousel.Cards = append(carousel.Cards, &waE2E.InteractiveMessage{
				Header: &waE2E.InteractiveMessage_Header{
					Title:    proto.String(rawCarouselTitle(i)),
					Subtitle: proto.String("From ₹1,299"),
					Media: &waE2E.InteractiveMessage_Header_JPEGThumbnail{
						JPEGThumbnail: outgoingImageThumbnail(picture),
					},
				},
				Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Free cancellation up to 24 hours before.")},
				InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
					NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
						Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
							Name:             proto.String("cta_url"),
							ButtonParamsJSON: proto.String(`{"display_text":"Book","url":"https://www.irctc.co.in/"}`),
						}},
					},
				},
			})
		}
		return &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
			Body:               &waE2E.InteractiveMessage_Body{Text: proto.String(p.Body)},
			Footer:             &waE2E.InteractiveMessage_Footer{Text: proto.String(p.Footer)},
			InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{CarouselMessage: carousel},
		}}, nil
	}

	buttons := []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
		{
			Name:             proto.String("cta_url"),
			ButtonParamsJSON: proto.String(`{"display_text":"Renew online","url":"https://english.bmrc.co.in/"}`),
		},
		{
			Name:             proto.String("cta_copy"),
			ButtonParamsJSON: proto.String(`{"display_text":"Copy pass number","copy_code":"BLR-88213-M"}`),
		},
		{
			Name:             proto.String("review_and_pay"),
			ButtonParamsJSON: proto.String(`{"currency":"INR","total_amount":{"value":129900,"offset":100}}`),
		},
	}
	if p.Select {
		buttons = []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
			Name: proto.String("single_select"),
			ButtonParamsJSON: proto.String(`{"title":"Pick a station","sections":[` +
				`{"title":"Green line","rows":[` +
				`{"id":"gl1","title":"Majestic","description":"Interchange"},` +
				`{"id":"gl2","title":"Lalbagh"}]},` +
				`{"title":"Purple line","rows":[` +
				`{"id":"pl1","title":"Indiranagar"},` +
				`{"id":"pl2","title":"MG Road","description":"Nearest to the office"}]}]}`),
		}}
	}

	return &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
		Header: &waE2E.InteractiveMessage_Header{
			Title:    proto.String(p.Title),
			Subtitle: proto.String(p.Subtitle),
		},
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String(p.Body)},
		Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String(p.Footer)},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{Buttons: buttons},
		},
	}}, nil
}

func buildRawProduct(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Currency    string `json:"currency"`
		Price1000   int64  `json:"price_1000"`
		Sale1000    int64  `json:"sale_1000"`
		URL         string `json:"url"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Title == "" {
		p.Title = "Channapatna lacquered top"
	}
	if p.Description == "" {
		p.Description = "Turned from ivory wood and coloured with vegetable dyes. Spins for about forty seconds."
	}
	if p.Currency == "" {
		p.Currency = "INR"
	}
	if p.Price1000 == 0 {
		p.Price1000 = 449000
	}
	if p.URL == "" {
		p.URL = "https://www.cauverycrafts.com/"
	}

	picture, err := generateTestPicture(2, 4)
	if err != nil {
		return nil, err
	}

	return &waE2E.Message{ProductMessage: &waE2E.ProductMessage{
		BusinessOwnerJID: proto.String("919999999999@s.whatsapp.net"),
		Body:             proto.String("Back in stock, three left."),
		Footer:           proto.String("Ships from Bengaluru"),
		Product: &waE2E.ProductMessage_ProductSnapshot{
			ProductID:           proto.String("prod-8821"),
			Title:               proto.String(p.Title),
			Description:         proto.String(p.Description),
			CurrencyCode:        proto.String(p.Currency),
			PriceAmount1000:     proto.Int64(p.Price1000),
			SalePriceAmount1000: proto.Int64(p.Sale1000),
			RetailerID:          proto.String("CC-TOP-01"),
			URL:                 proto.String(p.URL),
			ProductImageCount:   proto.Uint32(4),
			ProductImage:        &waE2E.ImageMessage{JPEGThumbnail: outgoingImageThumbnail(picture)},
		},
	}}, nil
}

func buildRawOrder(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Title     string `json:"title"`
		Message   string `json:"message"`
		Items     int32  `json:"items"`
		Total1000 int64  `json:"total_1000"`
		Currency  string `json:"currency"`
		Status    string `json:"status"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Title == "" {
		p.Title = "Order #4471"
	}
	if p.Message == "" {
		p.Message = "Two tops and a set of nesting dolls."
	}
	if p.Items == 0 {
		p.Items = 3
	}
	if p.Total1000 == 0 {
		p.Total1000 = 1347000
	}
	if p.Currency == "" {
		p.Currency = "INR"
	}

	status := waE2E.OrderMessage_ACCEPTED
	switch p.Status {
	case "inquiry":
		status = waE2E.OrderMessage_INQUIRY
	case "declined":
		status = waE2E.OrderMessage_DECLINED
	}

	picture, err := generateTestPicture(0, 4)
	if err != nil {
		return nil, err
	}

	return &waE2E.Message{OrderMessage: &waE2E.OrderMessage{
		OrderID:           proto.String("ord-4471"),
		OrderTitle:        proto.String(p.Title),
		Message:           proto.String(p.Message),
		ItemCount:         proto.Int32(p.Items),
		Status:            status.Enum(),
		Surface:           waE2E.OrderMessage_CATALOG.Enum(),
		TotalAmount1000:   proto.Int64(p.Total1000),
		TotalCurrencyCode: proto.String(p.Currency),
		SellerJID:         proto.String("919999999999@s.whatsapp.net"),
		Thumbnail:         outgoingImageThumbnail(picture),
	}}, nil
}

func buildRawPayment(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		// Mode is "request", "sent" or "invite".
		Mode       string `json:"mode"`
		Note       string `json:"note"`
		Amount1000 int64  `json:"amount_1000"`
		Currency   string `json:"currency"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Note == "" {
		p.Note = "Dinner on Saturday"
	}
	if p.Amount1000 == 0 {
		p.Amount1000 = 1250000
	}
	if p.Currency == "" {
		p.Currency = "INR"
	}
	note := &waE2E.Message{Conversation: proto.String(p.Note)}

	switch p.Mode {
	case "sent":
		return &waE2E.Message{SendPaymentMessage: &waE2E.SendPaymentMessage{NoteMessage: note}}, nil
	case "invite":
		return &waE2E.Message{PaymentInviteMessage: &waE2E.PaymentInviteMessage{
			ServiceType: waE2E.PaymentInviteMessage_UPI.Enum(),
		}}, nil
	default:
		return &waE2E.Message{RequestPaymentMessage: &waE2E.RequestPaymentMessage{
			NoteMessage:         note,
			CurrencyCodeIso4217: proto.String(p.Currency),
			Amount1000:          proto.Uint64(uint64(p.Amount1000)),
			RequestFrom:         proto.String("917060029183@s.whatsapp.net"),
		}}, nil
	}
}

func buildRawStickerPack(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		PackID    string `json:"pack_id"`
		Name      string `json:"name"`
		Publisher string `json:"publisher"`
		Caption   string `json:"caption"`
		Count     int    `json:"count"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.PackID == "" {
		p.PackID = "dev-pack-1"
	}
	if p.Name == "" {
		p.Name = "Cats being unhelpful"
	}
	if p.Publisher == "" {
		p.Publisher = "Nobody in particular"
	}
	if p.Count == 0 {
		p.Count = 12
	}

	pack := &waE2E.StickerPackMessage{
		StickerPackID:   proto.String(p.PackID),
		Name:            proto.String(p.Name),
		Publisher:       proto.String(p.Publisher),
		Caption:         proto.String(p.Caption),
		PackDescription: proto.String("Twelve reasons not to get any work done."),
		StickerPackSize: proto.Uint64(uint64(p.Count)),
	}
	for i := 0; i < p.Count; i++ {
		pack.Stickers = append(pack.Stickers, &waE2E.StickerPackMessage_Sticker{
			FileName: proto.String(rawButtonID(i) + ".webp"),
			Emojis:   []string{"🐈"},
		})
	}
	return &waE2E.Message{StickerPackMessage: pack}, nil
}

func buildRawCallLog(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Video bool `json:"video"`
		// Outcome is "connected", "missed", "rejected", "failed" or "ongoing".
		Outcome  string `json:"outcome"`
		Duration int64  `json:"duration"`
		Group    bool   `json:"group"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}

	outcome := waE2E.CallLogMessage_CONNECTED
	switch p.Outcome {
	case "missed":
		outcome = waE2E.CallLogMessage_MISSED
	case "rejected":
		outcome = waE2E.CallLogMessage_REJECTED
	case "failed":
		outcome = waE2E.CallLogMessage_FAILED
	case "ongoing":
		outcome = waE2E.CallLogMessage_ONGOING
	}
	if p.Duration == 0 && outcome == waE2E.CallLogMessage_CONNECTED {
		p.Duration = 161
	}

	log := &waE2E.CallLogMessage{
		IsVideo:      proto.Bool(p.Video),
		CallOutcome:  outcome.Enum(),
		DurationSecs: proto.Int64(p.Duration),
		CallType:     waE2E.CallLogMessage_REGULAR.Enum(),
	}
	participants := 2
	if p.Group {
		participants = 4
	}
	for i := 0; i < participants; i++ {
		log.Participants = append(log.Participants, &waE2E.CallLogMessage_CallParticipant{
			JID:         proto.String(rawParticipantJID(i)),
			CallOutcome: outcome.Enum(),
		})
	}
	return &waE2E.Message{CallLogMesssage: log}, nil
}

// buildRawButtonReply is the other half of a button: what arrives when somebody
// presses one. It is a text row, and the point of being able to produce one is
// to prove that.
func buildRawButtonReply(_ context.Context, _ *Client, params json.RawMessage) (*waE2E.Message, error) {
	var p struct {
		Text string `json:"text"`
		ID   string `json:"id"`
	}
	if err := decodeRawParams(params, &p); err != nil {
		return nil, err
	}
	if p.Text == "" {
		p.Text = "Leave with a neighbour"
	}
	if p.ID == "" {
		p.ID = "neighbour"
	}
	return &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
		SelectedID:          proto.String(p.ID),
		SelectedDisplayText: proto.String(p.Text),
		SelectedIndex:       proto.Uint32(2),
	}}, nil
}

func rawButtonID(i int) string {
	return "btn-" + string(rune('a'+i%26))
}

func rawCarouselTitle(i int) string {
	titles := []string{"Mysuru", "Hampi", "Coorg", "Gokarna", "Chikmagalur"}
	return titles[i%len(titles)]
}

func rawParticipantJID(i int) string {
	jids := []string{
		"917060029183@s.whatsapp.net",
		"919999999999@s.whatsapp.net",
		"918888888888@s.whatsapp.net",
		"917777777777@s.whatsapp.net",
	}
	return jids[i%len(jids)]
}
