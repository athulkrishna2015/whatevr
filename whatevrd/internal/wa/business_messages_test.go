package wa

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	appstore "whatevrd/internal/store"
)

func ingestBusiness(t *testing.T, client *Client, id string, message *waE2E.Message) (appstore.MediaMessageInput, bool) {
	t.Helper()
	return client.mediaMessageInput(context.Background(), mediaIngestEvent(id, message), ingestOptions{source: sourceLive})
}

// The four wire shapes are one card. A frontend that had to know which of them
// arrived would be making this decision four times and getting a different card
// each time, so the ingest makes it once.
func TestBusinessShapesFlattenToOneCard(t *testing.T) {
	client := newMediaIngestClient(t)

	cases := []struct {
		name    string
		message *waE2E.Message
		source  string
	}{
		{
			name:   "buttons",
			source: "buttons",
			message: &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{
				Header:      &waE2E.ButtonsMessage_Text{Text: "Koshy's"},
				ContentText: proto.String("Your table is held until 8pm."),
				FooterText:  proto.String("Replies go to a human"),
				Buttons: []*waE2E.ButtonsMessage_Button{{
					ButtonID:   proto.String("ok"),
					ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Confirm")},
					Type:       waE2E.ButtonsMessage_Button_RESPONSE.Enum(),
				}},
			}},
		},
		{
			name:   "hydrated template",
			source: "template",
			message: &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
				Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{
					HydratedFourRowTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
						Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{HydratedTitleText: "Koshy's"},
						HydratedContentText: proto.String("Your table is held until 8pm."),
						HydratedFooterText:  proto.String("Replies go to a human"),
						HydratedButtons: []*waE2E.HydratedTemplateButton{{
							HydratedButton: &waE2E.HydratedTemplateButton_QuickReplyButton{
								QuickReplyButton: &waE2E.HydratedTemplateButton_HydratedQuickReplyButton{
									DisplayText: proto.String("Confirm"),
									ID:          proto.String("ok"),
								},
							},
						}},
					},
				},
			}},
		},
		{
			name:   "interactive",
			source: "interactive",
			message: &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
				Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Koshy's")},
				Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("Your table is held until 8pm.")},
				Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String("Replies go to a human")},
				InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
					NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
						Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
							Name:             proto.String("quick_reply"),
							ButtonParamsJSON: proto.String(`{"display_text":"Confirm","id":"ok"}`),
						}},
					},
				},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, ok := ingestBusiness(t, client, "biz-"+tc.name, tc.message)
			if !ok {
				t.Fatal("a business message must ingest")
			}
			if input.MediaKind != appstore.MediaKindInteractive {
				t.Fatalf("kind = %q", input.MediaKind)
			}
			payload := appstore.DecodePayload(input.PayloadJSON).Interactive
			if payload == nil {
				t.Fatal("no interactive payload was stored")
			}
			if payload.Source != tc.source {
				t.Fatalf("source = %q, want %q", payload.Source, tc.source)
			}
			if payload.Title != "Koshy's" || payload.Body != "Your table is held until 8pm." ||
				payload.Footer != "Replies go to a human" {
				t.Fatalf("payload = %+v", payload)
			}
			if len(payload.Buttons) != 1 {
				t.Fatalf("buttons = %+v", payload.Buttons)
			}
			button := payload.Buttons[0]
			if button.Kind != appstore.InteractiveButtonReply || button.Label != "Confirm" {
				t.Fatalf("button = %+v", button)
			}
			// A quick reply sends a message back to a business, which whatevr
			// cannot do yet. The card has to be able to say so rather than
			// letting somebody press a button that quietly does nothing.
			if button.Live {
				t.Fatal("a quick-reply button must not claim it can be pressed")
			}
			// The one-line rendering is what the message says, not the word
			// "Message" with the message hidden behind it.
			if input.PayloadSummary != "Your table is held until 8pm." {
				t.Fatalf("summary = %q", input.PayloadSummary)
			}
		})
	}
}

// The label on a native flow lives inside an undocumented JSON blob. A button
// parsed without reading it renders as "cta_url" or as nothing at all.
func TestNativeFlowButtonsComeOutOfTheirJSON(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := ingestBusiness(t, client, "flow1", &waE2E.Message{
		InteractiveMessage: &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Renew your pass.")},
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
					Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
						{
							Name:             proto.String("cta_url"),
							ButtonParamsJSON: proto.String(`{"display_text":"Renew online","url":"english.bmrc.co.in/renew"}`),
						},
						{
							Name:             proto.String("cta_call"),
							ButtonParamsJSON: proto.String(`{"display_text":"Call us","phone_number":"+91 (80) 2296 9300"}`),
						},
						{
							Name:             proto.String("cta_copy"),
							ButtonParamsJSON: proto.String(`{"display_text":"Copy pass number","copy_code":"BLR-88213-M"}`),
						},
						{
							Name:             proto.String("review_and_pay"),
							ButtonParamsJSON: proto.String(`{"currency":"INR"}`),
						},
						{
							// A scheme the desktop must never be handed. A
							// button is pressed without being read.
							Name:             proto.String("cta_url"),
							ButtonParamsJSON: proto.String(`{"display_text":"Trust me","url":"javascript:alert(1)"}`),
						},
					},
				},
			},
		},
	})
	if !ok {
		t.Fatal("an interactive message must ingest")
	}

	payload := appstore.DecodePayload(input.PayloadJSON).Interactive
	if len(payload.Buttons) != 4 {
		t.Fatalf("buttons = %+v", payload.Buttons)
	}

	url := payload.Buttons[0]
	if url.Kind != appstore.InteractiveButtonURL || url.Label != "Renew online" || !url.Live {
		t.Fatalf("url button = %+v", url)
	}
	// A bare host in a params blob is a URL a phone opens, so it is one here.
	if url.URL != "https://english.bmrc.co.in/renew" {
		t.Fatalf("url = %q", url.URL)
	}

	call := payload.Buttons[1]
	if call.Kind != appstore.InteractiveButtonCall || !call.Live || call.Phone != "+918022969300" {
		t.Fatalf("call button = %+v", call)
	}

	copyButton := payload.Buttons[2]
	if copyButton.Kind != appstore.InteractiveButtonCopy || copyButton.Copy != "BLR-88213-M" || !copyButton.Live {
		t.Fatalf("copy button = %+v", copyButton)
	}

	// An unrecognised flow keeps a readable label made from its own name and
	// says out loud that it cannot be carried out.
	other := payload.Buttons[3]
	if other.Kind != appstore.InteractiveButtonOther || other.Label != "Review and pay" || other.Live {
		t.Fatalf("other button = %+v", other)
	}
}

// A single-select flow is a menu wearing a button's clothes. Its rows are the
// message, so they come out of the blob and become the card's list.
func TestSingleSelectFlowBecomesAList(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := ingestBusiness(t, client, "sel1", &waE2E.Message{
		InteractiveMessage: &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Pick a station.")},
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
					Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
						Name: proto.String("single_select"),
						ButtonParamsJSON: proto.String(`{"title":"Stations","sections":[` +
							`{"title":"Green line","rows":[{"id":"gl1","title":"Majestic","description":"Interchange"}]}]}`),
					}},
				},
			},
		},
	})
	if !ok {
		t.Fatal("a single-select message must ingest")
	}

	payload := appstore.DecodePayload(input.PayloadJSON).Interactive
	if len(payload.Buttons) != 0 {
		t.Fatalf("the menu must not also be left inside a button: %+v", payload.Buttons)
	}
	if len(payload.Sections) != 1 || len(payload.Sections[0].Rows) != 1 {
		t.Fatalf("sections = %+v", payload.Sections)
	}
	if payload.Sections[0].Title != "Green line" {
		t.Fatalf("section title = %q", payload.Sections[0].Title)
	}
	row := payload.Sections[0].Rows[0]
	if row.Title != "Majestic" || row.Description != "Interchange" || row.ID != "gl1" {
		t.Fatalf("row = %+v", row)
	}
	if payload.ListLabel != "Stations" {
		t.Fatalf("list label = %q", payload.ListLabel)
	}
}

// A carousel is the one shape that nests. Each slide keeps its own picture, and
// a slide may not carry a carousel of its own: a format that recurses without a
// floor is a message that can make the daemon walk forever.
func TestCarouselKeepsItsCardsAndDoesNotRecurse(t *testing.T) {
	client := newMediaIngestClient(t)

	card := func(title string) *waE2E.InteractiveMessage {
		return &waE2E.InteractiveMessage{
			Header: &waE2E.InteractiveMessage_Header{
				Title: proto.String(title),
				Media: &waE2E.InteractiveMessage_Header_JPEGThumbnail{
					JPEGThumbnail: testThumbnailJPEG(t, 60, 40),
				},
			},
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Free cancellation.")},
		}
	}
	nested := card("Nested")
	nested.InteractiveMessage = &waE2E.InteractiveMessage_CarouselMessage_{
		CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{Cards: []*waE2E.InteractiveMessage{card("Deeper")}},
	}

	input, ok := ingestBusiness(t, client, "car1", &waE2E.Message{
		InteractiveMessage: &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Weekend trips.")},
			InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{
				CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{
					Cards: []*waE2E.InteractiveMessage{card("Mysuru"), nested},
				},
			},
		},
	})
	if !ok {
		t.Fatal("a carousel must ingest")
	}

	payload := appstore.DecodePayload(input.PayloadJSON).Interactive
	if payload.Source != "carousel" {
		t.Fatalf("source = %q", payload.Source)
	}
	if len(payload.Cards) != 2 {
		t.Fatalf("cards = %d", len(payload.Cards))
	}
	if payload.Cards[1].Cards != nil {
		t.Fatal("a carousel card must not carry a carousel of its own")
	}
	// Every slide gets its own file. One shared name would have meant twelve
	// slides overwriting each other twelve times and all showing the last one.
	if payload.Cards[0].ThumbnailPath == "" || payload.Cards[1].ThumbnailPath == "" {
		t.Fatalf("card thumbnails = %q, %q", payload.Cards[0].ThumbnailPath, payload.Cards[1].ThumbnailPath)
	}
	if payload.Cards[0].ThumbnailPath == payload.Cards[1].ThumbnailPath {
		t.Fatal("two slides were written to the same file")
	}
	// The picture bytes are carried from the parse to the write and no
	// further: a payload column is not where a picture goes.
	if len(payload.Cards[0].HeaderJPEG) != 0 {
		t.Fatal("the raw JPEG survived into the stored payload")
	}
}

// A non-hydrated template names a template in a catalogue a linked device is
// never sent. There is nothing honest to render, so it stays a tombstone rather
// than becoming a card full of placeholders.
func TestNonHydratedTemplateStaysATombstone(t *testing.T) {
	client := newMediaIngestClient(t)

	input, ok := ingestBusiness(t, client, "tpl1", &waE2E.Message{
		TemplateMessage: &waE2E.TemplateMessage{
			Format: &waE2E.TemplateMessage_FourRowTemplate_{
				FourRowTemplate: &waE2E.TemplateMessage_FourRowTemplate{
					Content: &waE2E.HighlyStructuredMessage{
						ElementName: proto.String("order_update"),
					},
				},
			},
		},
	})
	if !ok {
		t.Fatal("the message is real and must still get a row")
	}
	if input.MediaKind != appstore.MediaKindUnsupported {
		t.Fatalf("kind = %q, want the tombstone", input.MediaKind)
	}
}

// Pressing a button sends back a message whose whole content is the words that
// were on it. That is text, and it has to land as text or search, quoting and
// the chat-list preview would each need a special case for it.
func TestButtonResponsesAreText(t *testing.T) {
	client := newMediaIngestClient(t)

	cases := []struct {
		name    string
		message *waE2E.Message
		want    string
	}{
		{"template reply", &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
			SelectedDisplayText: proto.String("Leave with a neighbour"),
			SelectedID:          proto.String("neighbour"),
		}}, "Leave with a neighbour"},
		{"buttons response", &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Confirm"},
			SelectedButtonID: proto.String("ok"),
		}}, "Confirm"},
		{"list response", &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
			Title: proto.String("Masala dosa"),
		}}, "Masala dosa"},
		{"interactive response", &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			Body: &waE2E.InteractiveResponseMessage_Body{Text: proto.String("Majestic")},
		}}, "Majestic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, ok := client.textMessageInput(context.Background(),
				mediaIngestEvent("resp-"+tc.name, tc.message), ingestOptions{source: sourceLive})
			if !ok {
				t.Fatal("a button response must ingest as a message")
			}
			if input.Text != tc.want {
				t.Fatalf("text = %q, want %q", input.Text, tc.want)
			}
			if input.PayloadJSON != "" {
				t.Fatalf("a response is text and nothing else: %q", input.PayloadJSON)
			}
		})
	}
}

// A phone number is handed to the desktop's tel: handler. Anything in the field
// that is not a number has no business being passed on.
func TestPhoneNumbersAreReducedToDialableDigits(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"+91 (80) 2296 9300", "+918022969300"},
		{"080-2296-9300", "08022969300"},
		{"tel:+1234567890", "1234567890"},
		{"12", ""},
		{"", ""},
		{"not a number", ""},
	}
	for _, tc := range cases {
		if got := sanitizePhoneNumber(tc.in); got != tc.want {
			t.Fatalf("sanitizePhoneNumber(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
