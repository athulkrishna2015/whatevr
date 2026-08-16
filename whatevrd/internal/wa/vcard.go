package wa

import (
	"strings"

	appstore "whatevrd/internal/store"
)

// A minimal vCard 3.0 reader, sized for the cards WhatsApp actually sends.
//
// It is deliberately not a general vCard library: WhatsApp emits a narrow,
// predictable subset, and the one thing worth being careful about is the
// `waid=` parameter it hangs off TEL. That parameter is WhatsApp telling us
// this number is on WhatsApp and what its jid is, which means the "Message"
// action on a shared contact needs no lookup, no usync round trip and no
// telling the server which contacts somebody forwarded us.

// parseVCard turns one vCard into a card. The raw text is kept so the card can
// be exported unchanged; re-serializing a parse would lose whatever this
// reader does not model.
func parseVCard(raw string) appstore.ContactCard {
	card := appstore.ContactCard{VCard: raw}
	// Grouped properties (item1.TEL / item1.X-ABLabel) carry their human label
	// on a separate line, so labels are collected first and applied after.
	labels := map[string]string{}
	type pending struct {
		group string
		field appstore.ContactField
	}
	var phones, emails, urls, addresses []pending

	var structuredName string
	for _, line := range unfoldVCardLines(raw) {
		group, name, params, value := splitVCardLine(line)
		if value == "" {
			continue
		}
		switch strings.ToUpper(name) {
		case "FN":
			card.DisplayName = unescapeVCardValue(value)
		case "N":
			// "family;given;middle;prefix;suffix", and WhatsApp often sends only
			// this when the sender never named the contact.
			structuredName = structuredNameToDisplay(value)
		case "ORG":
			// ORG is "organization;unit;subunit"; WhatsApp sends just the first
			// with a trailing separator, but the units are worth keeping.
			card.Org = joinVCardComponents(value)
		case "TITLE":
			card.Title = unescapeVCardValue(value)
		case "BDAY":
			card.Birthday = unescapeVCardValue(value)
		case "TEL":
			phones = append(phones, pending{group, appstore.ContactField{
				Value: unescapeVCardValue(value),
				Label: vCardTypeLabel(params),
				JID:   waidJID(params),
			}})
		case "EMAIL":
			emails = append(emails, pending{group, appstore.ContactField{
				Value: unescapeVCardValue(value),
				Label: vCardTypeLabel(params),
			}})
		case "URL":
			urls = append(urls, pending{group, appstore.ContactField{
				Value: unescapeVCardValue(value),
				Label: vCardTypeLabel(params),
			}})
		case "ADR":
			if address := joinVCardComponents(value); address != "" {
				addresses = append(addresses, pending{group, appstore.ContactField{
					Value: address,
					Label: vCardTypeLabel(params),
				}})
			}
		case "X-ABLABEL":
			if group != "" {
				labels[group] = unescapeVCardValue(value)
			}
		}
	}

	if card.DisplayName == "" {
		card.DisplayName = structuredName
	}

	apply := func(items []pending) []appstore.ContactField {
		if len(items) == 0 {
			return nil
		}
		out := make([]appstore.ContactField, 0, len(items))
		for _, item := range items {
			// A custom label beats the generic TYPE: "Work mobile" is what the
			// sender wrote, "CELL" is what the format needed.
			if label, ok := labels[item.group]; ok && label != "" {
				item.field.Label = label
			}
			out = append(out, item.field)
		}
		return out
	}
	card.Phones = apply(phones)
	card.Emails = apply(emails)
	card.URLs = apply(urls)
	card.Addresses = apply(addresses)
	return card
}

// unfoldVCardLines joins continuation lines (a line beginning with a space or
// tab continues the one before it), which is how vCard wraps long values.
func unfoldVCardLines(raw string) []string {
	rawLines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(lines) > 0 {
			// The fold is a line break plus exactly one whitespace character.
			// Stripping all of the leading whitespace instead would eat spaces
			// that are part of the value, silently joining "A Very" and "Long"
			// into "A VeryLong".
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// splitVCardLine breaks "item1.TEL;type=CELL;waid=91...:+91 ..." into its
// group, property name, parameters and value.
func splitVCardLine(line string) (group, name string, params []string, value string) {
	head, value, found := strings.Cut(line, ":")
	if !found {
		return "", "", nil, ""
	}
	parts := strings.Split(head, ";")
	name = parts[0]
	params = parts[1:]
	if dot := strings.IndexByte(name, '.'); dot >= 0 {
		group, name = name[:dot], name[dot+1:]
	}
	return group, name, params, strings.TrimSpace(value)
}

// waidJID pulls the WhatsApp id out of a TEL's parameters. WhatsApp writes it
// as `waid=<phone>` with no server part, so this completes it into a real jid.
func waidJID(params []string) string {
	for _, param := range params {
		key, value, found := strings.Cut(param, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "waid") {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "@") {
			return value
		}
		return value + "@s.whatsapp.net"
	}
	return ""
}

// vCardTypeLabel turns the TYPE parameter into something worth showing. The
// format allows several spellings, and only the useful ones are named here;
// anything unrecognised is passed through capitalised rather than dropped.
func vCardTypeLabel(params []string) string {
	for _, param := range params {
		key, value, found := strings.Cut(param, "=")
		if !found {
			// vCard 2.1 style: a bare "CELL" with no "TYPE=".
			key, value = "type", param
		}
		if !strings.EqualFold(strings.TrimSpace(key), "type") {
			continue
		}
		for _, token := range strings.Split(value, ",") {
			token = strings.Trim(strings.TrimSpace(token), `"`)
			switch strings.ToUpper(token) {
			case "CELL", "MOBILE":
				return "Mobile"
			case "HOME":
				return "Home"
			case "WORK":
				return "Work"
			case "MAIN":
				return "Main"
			case "FAX":
				return "Fax"
			case "PAGER":
				return "Pager"
			case "IPHONE", "VOICE", "PREF", "INTERNET":
				// Carry no information a reader wants; keep looking.
				continue
			default:
				if token != "" {
					return strings.ToUpper(token[:1]) + strings.ToLower(token[1:])
				}
			}
		}
	}
	return ""
}

// splitVCardComponents splits a compound value on its component separator,
// which an escaped `\;` inside a component is not. Splitting on every `;` puts
// half an address in one field and half in the next.
func splitVCardComponents(value string) []string {
	var (
		fields  []string
		current strings.Builder
		escaped bool
	)
	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune('\\')
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == ';':
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	return append(fields, current.String())
}

// structuredNameToDisplay renders an N property as a readable name.
func structuredNameToDisplay(value string) string {
	fields := splitVCardComponents(value)
	for i := range fields {
		fields[i] = unescapeVCardValue(strings.TrimSpace(fields[i]))
	}
	// N is family;given;middle;prefix;suffix, and reads given-first.
	order := []int{3, 1, 2, 0, 4}
	parts := make([]string, 0, len(order))
	for _, index := range order {
		if index < len(fields) && fields[index] != "" {
			parts = append(parts, fields[index])
		}
	}
	return strings.Join(parts, " ")
}

// joinVCardComponents flattens a compound value (an ADR, an ORG) onto one
// line, dropping the empty components the format is full of.
func joinVCardComponents(value string) string {
	fields := splitVCardComponents(value)
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = unescapeVCardValue(strings.TrimSpace(field)); field != "" {
			parts = append(parts, field)
		}
	}
	return strings.Join(parts, ", ")
}

// unescapeVCardValue undoes the escaping the format requires inside values.
func unescapeVCardValue(value string) string {
	if !strings.ContainsRune(value, '\\') {
		return strings.TrimSpace(value)
	}
	replacer := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.TrimSpace(replacer.Replace(value))
}
