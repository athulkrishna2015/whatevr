package wa

import (
	"testing"

	appstore "whatevrd/internal/store"
)

// The shape WhatsApp really sends: grouped properties, an X-ABLabel carrying
// the human label, and the waid= parameter that says the number is on WhatsApp.
const whatsAppVCard = "BEGIN:VCARD\n" +
	"VERSION:3.0\n" +
	"N:;Aditi Rao;;;\n" +
	"FN:Aditi Rao\n" +
	"ORG:Kanteerava Studios;\n" +
	"TITLE:Sound Engineer\n" +
	"item1.TEL;waid=917060029183:+91 70600 29183\n" +
	"item1.X-ABLabel:Mobile\n" +
	"item2.TEL;type=WORK:+91 80 4123 4567\n" +
	"item3.EMAIL;type=INTERNET:aditi@example.com\n" +
	"item3.X-ABLabel:Work\n" +
	"END:VCARD"

func TestParseVCardReadsTheWhatsAppShape(t *testing.T) {
	card := parseVCard(whatsAppVCard)

	if card.DisplayName != "Aditi Rao" {
		t.Fatalf("DisplayName = %q", card.DisplayName)
	}
	if card.Org != "Kanteerava Studios" {
		t.Fatalf("Org = %q, want the trailing ';' trimmed", card.Org)
	}
	if card.Title != "Sound Engineer" {
		t.Fatalf("Title = %q", card.Title)
	}

	if len(card.Phones) != 2 {
		t.Fatalf("%d phones, want 2", len(card.Phones))
	}
	// The waid parameter is the whole point: it means the Message action needs
	// no lookup and no usync round trip.
	if card.Phones[0].JID != "917060029183@s.whatsapp.net" {
		t.Fatalf("phone jid = %q", card.Phones[0].JID)
	}
	if card.Phones[0].Value != "+91 70600 29183" {
		t.Fatalf("phone value = %q", card.Phones[0].Value)
	}
	// A grouped X-ABLabel beats the generic TYPE.
	if card.Phones[0].Label != "Mobile" {
		t.Fatalf("phone label = %q", card.Phones[0].Label)
	}
	if card.Phones[1].Label != "Work" || card.Phones[1].JID != "" {
		t.Fatalf("second phone = %+v, want a Work label and no jid", card.Phones[1])
	}

	if len(card.Emails) != 1 || card.Emails[0].Value != "aditi@example.com" || card.Emails[0].Label != "Work" {
		t.Fatalf("emails = %+v", card.Emails)
	}
	// The original text is kept so exporting hands on exactly what arrived.
	if card.VCard != whatsAppVCard {
		t.Error("the raw vCard was not preserved")
	}
}

// A sender who never named the contact leaves only the structured N property.
func TestParseVCardFallsBackToTheStructuredName(t *testing.T) {
	card := parseVCard("BEGIN:VCARD\nVERSION:3.0\nN:Rao;Aditi;Kumari;Dr;\nTEL:+911234567890\nEND:VCARD")
	if card.DisplayName != "Dr Aditi Kumari Rao" {
		t.Fatalf("DisplayName = %q", card.DisplayName)
	}
}

// Long values are wrapped by continuation lines, and a parser that treats them
// as separate properties silently truncates the value.
func TestParseVCardUnfoldsContinuationLines(t *testing.T) {
	card := parseVCard("BEGIN:VCARD\nVERSION:3.0\nFN:Somebody With A Very\n  Long Name Indeed\nEND:VCARD")
	if card.DisplayName != "Somebody With A Very Long Name Indeed" {
		t.Fatalf("DisplayName = %q", card.DisplayName)
	}
}

func TestParseVCardUnescapesValues(t *testing.T) {
	card := parseVCard(`BEGIN:VCARD
VERSION:3.0
FN:Smith\, John
ADR:;;12 MG Road\;Suite 4;Bengaluru;;560001;India
END:VCARD`)
	if card.DisplayName != "Smith, John" {
		t.Fatalf("DisplayName = %q", card.DisplayName)
	}
	if len(card.Addresses) != 1 {
		t.Fatalf("addresses = %+v", card.Addresses)
	}
	if card.Addresses[0].Value != "12 MG Road;Suite 4, Bengaluru, 560001, India" {
		t.Fatalf("address = %q", card.Addresses[0].Value)
	}
}

func TestParseVCardSurvivesRubbish(t *testing.T) {
	for _, raw := range []string{"", "not a vcard at all", "BEGIN:VCARD\nTEL\nEND:VCARD"} {
		card := parseVCard(raw)
		if len(card.Phones) != 0 || card.DisplayName != "" {
			t.Fatalf("parseVCard(%q) = %+v, want nothing", raw, card)
		}
	}
}

func TestContactsSummaryNamesOneAndCountsMany(t *testing.T) {
	one := parseVCard(whatsAppVCard)
	if got := contactsSummary(contactsPayloadOf(one)); got != "Aditi Rao" {
		t.Fatalf("single-card summary = %q", got)
	}
	if got := contactsSummary(contactsPayloadOf(one, one, one)); got != "3 contacts" {
		t.Fatalf("multi-card summary = %q", got)
	}
	// A nameless card still says something useful: its number.
	nameless := parseVCard("BEGIN:VCARD\nVERSION:3.0\nTEL:+911234567890\nEND:VCARD")
	if got := contactsSummary(contactsPayloadOf(nameless)); got != "+911234567890" {
		t.Fatalf("nameless summary = %q", got)
	}
}

func contactsPayloadOf(cards ...appstore.ContactCard) appstore.ContactsPayload {
	return appstore.ContactsPayload{Cards: cards}
}
