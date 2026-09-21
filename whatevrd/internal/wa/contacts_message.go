package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	appstore "whatevrd/internal/store"
)

// Shared contact cards. WhatsApp has two shapes for the same idea, a single
// ContactMessage and a ContactsArrayMessage holding several, and both become
// one row carrying a list, so a renderer has one thing to draw rather than two.

func (c *Client) contactMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}

	var (
		payload     appstore.ContactsPayload
		contextInfo *waE2E.ContextInfo
		kind        string
	)
	switch {
	case evt.Message.GetContactMessage() != nil:
		contact := evt.Message.GetContactMessage()
		contextInfo = contact.GetContextInfo()
		kind = appstore.MediaKindContact
		payload.Cards = []appstore.ContactCard{cardFromContactMessage(contact)}
	case evt.Message.GetContactsArrayMessage() != nil:
		array := evt.Message.GetContactsArrayMessage()
		contextInfo = array.GetContextInfo()
		kind = appstore.MediaKindContacts
		payload.DisplayName = strings.TrimSpace(array.GetDisplayName())
		for _, contact := range array.GetContacts() {
			payload.Cards = append(payload.Cards, cardFromContactMessage(contact))
		}
		// An array that arrived with exactly one card is a single contact as
		// far as anybody reading it is concerned.
		if len(payload.Cards) == 1 {
			kind = appstore.MediaKindContact
		}
	default:
		return appstore.MediaMessageInput{}, false
	}
	if len(payload.Cards) == 0 {
		return appstore.MediaMessageInput{}, false
	}

	base, _, ok := c.mediaInputBase(ctx, evt, opts, "", contextInfo)
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Contacts: &payload})
	if err != nil {
		c.log.Warnf("Failed to encode contacts payload for %s: %v", base.ID, err)
		return appstore.MediaMessageInput{}, false
	}

	base.PayloadJSON = encoded

	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        kind,
		PayloadSummary:   contactsSummary(payload),
	}, true
}

// cardFromContactMessage parses a contact's vCard, falling back to the display
// name WhatsApp sends alongside it when the card itself carries no name.
func cardFromContactMessage(contact *waE2E.ContactMessage) appstore.ContactCard {
	card := parseVCard(contact.GetVcard())
	if card.DisplayName == "" {
		card.DisplayName = strings.TrimSpace(contact.GetDisplayName())
	}
	return card
}

// contactsSummary is the detail on the one-line rendering: the person's name
// for a single card, and a count for several, because listing four names in a
// chat-list row helps nobody.
func contactsSummary(payload appstore.ContactsPayload) string {
	if len(payload.Cards) == 1 {
		if name := payload.Cards[0].DisplayName; name != "" {
			return name
		}
		if phone := primaryPhone(payload.Cards[0]); phone != "" {
			return phone
		}
		return ""
	}
	if payload.DisplayName != "" {
		return payload.DisplayName
	}
	// English-only, like every other daemon-side summary: the wire's `fallback`
	// is a last resort for frontends that cannot render the kind, and the ones
	// that can render their own localized text from the payload.
	return fmt.Sprintf("%d contacts", len(payload.Cards))
}

// primaryPhone is the number a card leads with: the one WhatsApp vouched for
// if there is one, otherwise simply the first.
func primaryPhone(card appstore.ContactCard) string {
	for _, phone := range card.Phones {
		if phone.JID != "" {
			return phone.Value
		}
	}
	if len(card.Phones) > 0 {
		return card.Phones[0].Value
	}
	return ""
}
