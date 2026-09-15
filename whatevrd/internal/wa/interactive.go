package wa

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// This file implements the interactive message kinds: polls (create, vote,
// live tally), contact cards and locations. Polls send immediately (like
// reactions) rather than through the media queue: they carry no bytes.

// pollDefinition is the static part of a poll row (poll_data column).
type pollDefinition struct {
	Question   string   `json:"question"`
	Options    []string `json:"options"`
	Selectable int      `json:"selectable"`
}

// SendPoll creates a single- or multi-select poll. Options are trimmed,
// deduplicated, and capped the way official clients cap them (2–12).
func (c *Client) SendPoll(ctx context.Context, chatID, question string, options []string, multi bool) (appstore.SavedTextMessage, error) {
	client, err := c.requireConnectedClient()
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "poll question is required")
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || seen[option] {
			continue
		}
		seen[option] = true
		clean = append(clean, option)
	}
	if len(clean) < 2 {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "a poll needs at least two options")
	}
	if len(clean) > 12 {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "a poll holds at most 12 options")
	}
	targetJID, err := types.ParseJID(chatID)
	if err != nil {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}
	targetJID = c.normalizeJIDForChat(ctx, targetJID)
	chatID = targetJID.String()

	selectable := 1
	if multi {
		selectable = len(clean)
	}
	built := client.BuildPollCreation(question, clean, selectable)
	messageID := client.GenerateMessageID()
	if _, err := client.SendMessage(ctx, targetJID, built, whatsmeow.SendRequestExtra{ID: messageID}); err != nil {
		return appstore.SavedTextMessage{}, err
	}
	definition, _ := json.Marshal(pollDefinition{Question: question, Options: clean, Selectable: selectable})
	saved, err := c.store.SaveMediaMessage(ctx, appstore.MediaMessageInput{
		TextMessageInput: appstore.TextMessageInput{
			ID:          internalMessageIDForChat(chatID, messageID),
			ChatID:      chatID,
			SenderID:    "me",
			Text:        question,
			Timestamp:   time.Now(),
			Direction:   appstore.DirectionOutgoing,
			Status:      appstore.StatusSent,
			IsGroup:     targetJID.Server == types.GroupServer || targetJID.Server == types.BroadcastServer,
			CountUnread: false,
		},
		MediaKind: appstore.MediaKindPoll,
		PollData:  string(definition),
		PollTally: "{}",
	})
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	if saved.Inserted {
		c.daemon.PublishNewMessage(toDaemonMessage(saved.Message), toDaemonChat(saved.Chat))
	}
	return saved, nil
}

// VotePoll votes for option names on a poll message. A re-vote replaces the
// voter's old ballot, and the voter's own tally updates immediately.
func (c *Client) VotePoll(ctx context.Context, messageID string, optionNames []string) (appstore.Message, error) {
	client, err := c.requireConnectedClient()
	if err != nil {
		return appstore.Message{}, err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorInvalidArgument, "message_id is required")
	}
	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return appstore.Message{}, err
	}
	if message.MediaKind != appstore.MediaKindPoll {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorRejected, "message is not a poll")
	}
	clean := make([]string, 0, len(optionNames))
	for _, option := range optionNames {
		if option = strings.TrimSpace(option); option != "" {
			clean = append(clean, option)
		}
	}
	if len(clean) == 0 {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorInvalidArgument, "at least one option is required")
	}
	chatJID, err := types.ParseJID(message.ChatID)
	if err != nil {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}
	author := message.SenderID
	if author == "me" {
		if client.Store.ID == nil {
			return appstore.Message{}, app.NewCommandError(app.CommandErrorNotLoggedIn, "WhatsApp session is not logged in")
		}
		author = client.Store.ID.ToNonAD().String()
	}
	authorJID, err := types.ParseJID(author)
	if err != nil {
		return appstore.Message{}, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid poll author: %v", err)
	}
	vote, err := client.BuildPollVote(ctx, &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chatJID,
			Sender:   authorJID,
			IsGroup:  chatJID.Server == types.GroupServer,
			IsFromMe: message.SenderID == "me",
		},
		ID: types.MessageID(appstore.ExternalMessageID(message.ChatID, message.ID)),
	}, clean)
	if err != nil {
		return appstore.Message{}, err
	}
	if _, err := client.SendMessage(ctx, chatJID, vote); err != nil {
		return appstore.Message{}, err
	}
	tally, err := c.store.RecordPollVotes(ctx, message.ID, "me", clean)
	if err != nil {
		return appstore.Message{}, err
	}
	updated, err := c.refreshPollTally(ctx, message.ID, tally)
	if err != nil {
		return appstore.Message{}, err
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	return updated, nil
}

// refreshPollTally persists a tally map onto its poll row.
func (c *Client) refreshPollTally(ctx context.Context, messageID string, tally map[string]int) (appstore.Message, error) {
	raw, _ := json.Marshal(tally)
	return c.store.SetMessagePollTally(ctx, messageID, string(raw))
}

// pollCreationInput stores an inbound poll as a poll-kind row.
func (c *Client) pollCreationInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	creation := evt.Message.GetPollCreationMessage()
	if creation == nil {
		if v2 := evt.Message.GetPollCreationMessageV2(); v2 != nil {
			creation = v2
		} else if v3 := evt.Message.GetPollCreationMessageV3(); v3 != nil {
			creation = v3
		}
	}
	if creation == nil {
		return appstore.MediaMessageInput{}, false
	}
	question := strings.TrimSpace(creation.GetName())
	if question == "" {
		return appstore.MediaMessageInput{}, false
	}
	options := make([]string, 0, len(creation.GetOptions()))
	for _, option := range creation.GetOptions() {
		if name := strings.TrimSpace(option.GetOptionName()); name != "" {
			options = append(options, name)
		}
	}
	if len(options) < 2 {
		return appstore.MediaMessageInput{}, false
	}
	base, _, ok := c.mediaInputBase(ctx, evt, opts, question, creation.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}
	definition, _ := json.Marshal(pollDefinition{
		Question:   question,
		Options:    options,
		Selectable: int(creation.GetSelectableOptionsCount()),
	})
	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindPoll,
		PollData:         string(definition),
		PollTally:        "{}",
	}, true
}

// handlePollVote records an inbound ballot. It returns true when the event
// was a vote (handled or best-effort skipped), so the caller never files a
// vote as a chat message.
func (c *Client) handlePollVote(ctx context.Context, evt *events.Message) bool {
	if evt == nil || evt.Message == nil {
		return false
	}
	update := evt.Message.GetPollUpdateMessage()
	if update == nil {
		return false
	}
	client := c.currentClient()
	if client == nil {
		return true
	}
	pollKey := update.GetPollCreationMessageKey()
	if pollKey == nil {
		return true
	}
	decrypted, err := client.DecryptPollVote(ctx, evt)
	if err != nil {
		c.log.Warnf("Failed to decrypt poll vote %s: %v", evt.Info.ID, err)
		return true
	}
	chatJID := c.normalizeJIDForChat(ctx, evt.Info.Chat)
	internalID := internalMessageIDForChat(chatJID.String(), pollKey.GetID())
	poll, err := c.store.GetMessage(ctx, internalID)
	if err != nil {
		return true
	}
	var definition pollDefinition
	if err := json.Unmarshal([]byte(poll.PollData), &definition); err != nil {
		return true
	}
	hashes := map[string]bool{}
	for _, hash := range decrypted.GetSelectedOptions() {
		hashes[string(hash)] = true
	}
	var names []string
	for _, option := range definition.Options {
		digest := sha256.Sum256([]byte(option))
		if hashes[string(digest[:])] {
			names = append(names, option)
		}
	}
	if len(names) == 0 {
		return true
	}
	voter := c.canonicalParticipantJID(ctx, evt.Info.Sender)
	if voter == "" {
		voter = senderID(evt.Info)
	}
	tally, err := c.store.RecordPollVotes(ctx, poll.ID, voter, names)
	if err != nil {
		c.log.Warnf("Failed to record poll vote for %s: %v", poll.ID, err)
		return true
	}
	updated, err := c.refreshPollTally(ctx, poll.ID, tally)
	if err != nil {
		c.log.Warnf("Failed to refresh poll tally for %s: %v", poll.ID, err)
		return true
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(updated))
	return true
}

// SendContact shares a contact card (name + phone) as a vCard message.
func (c *Client) SendContact(ctx context.Context, chatID, name, phone string) (appstore.SavedTextMessage, error) {
	client, err := c.requireConnectedClient()
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)
	if name == "" {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "contact name is required")
	}
	if phone == "" {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "contact phone is required")
	}
	targetJID, err := types.ParseJID(chatID)
	if err != nil {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}
	targetJID = c.normalizeJIDForChat(ctx, targetJID)
	chatID = targetJID.String()

	vcard := fmt.Sprintf("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:%s\r\nTEL;TYPE=CELL:%s\r\nEND:VCARD", vcardEscape(name), vcardEscape(phone))
	contactMsg := &waE2E.ContactMessage{
		DisplayName: proto.String(name),
		Vcard:       proto.String(vcard),
	}
	messageID := client.GenerateMessageID()
	if _, err := client.SendMessage(ctx, targetJID, &waE2E.Message{ContactMessage: contactMsg}, whatsmeow.SendRequestExtra{ID: messageID}); err != nil {
		return appstore.SavedTextMessage{}, err
	}
	payload, _ := proto.Marshal(contactMsg)
	saved, err := c.store.SaveMediaMessage(ctx, appstore.MediaMessageInput{
		TextMessageInput: appstore.TextMessageInput{
			ID:          internalMessageIDForChat(chatID, messageID),
			ChatID:      chatID,
			SenderID:    "me",
			Text:        name,
			Timestamp:   time.Now(),
			Direction:   appstore.DirectionOutgoing,
			Status:      appstore.StatusSent,
			IsGroup:     targetJID.Server == types.GroupServer || targetJID.Server == types.BroadcastServer,
			CountUnread: false,
		},
		MediaKind:     appstore.MediaKindContact,
		MediaMimeType: "text/vcard",
		MediaPayload:  payload,
	})
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	if saved.Inserted {
		c.daemon.PublishNewMessage(toDaemonMessage(saved.Message), toDaemonChat(saved.Chat))
	}
	return saved, nil
}

func vcardEscape(s string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\n", "\\n", ",", "\\,", ";", "\\;")
	return replacer.Replace(s)
}

// contactInput stores an inbound contact card.
func (c *Client) contactInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	contact := evt.Message.GetContactMessage()
	if contact == nil {
		if array := evt.Message.GetContactsArrayMessage(); array != nil && len(array.GetContacts()) > 0 {
			contact = array.GetContacts()[0]
		}
	}
	if contact == nil {
		return appstore.MediaMessageInput{}, false
	}
	name := strings.TrimSpace(contact.GetDisplayName())
	phone := contactPhoneFromVCard(contact.GetVcard())
	if name == "" {
		name = phone
	}
	if name == "" {
		name = "Contact"
	}
	base, _, ok := c.mediaInputBase(ctx, evt, opts, name, contact.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}
	payload, err := proto.Marshal(contact)
	if err != nil {
		return appstore.MediaMessageInput{}, false
	}
	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindContact,
		MediaMimeType:    "text/vcard",
		MediaPayload:     payload,
	}, true
}

// contactPhoneFromVCard pulls the first TEL value out of a vCard blob.
func contactPhoneFromVCard(vcard string) string {
	for _, line := range strings.Split(vcard, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "TEL") {
			if _, value, ok := strings.Cut(line, ":"); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

// SendLocation shares a location pin (coordinates plus an optional place
// name/address).
func (c *Client) SendLocation(ctx context.Context, chatID string, lat, long float64, name, address string) (appstore.SavedTextMessage, error) {
	client, err := c.requireConnectedClient()
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	if lat < -90 || lat > 90 || long < -180 || long > 180 {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "coordinates out of range")
	}
	targetJID, err := types.ParseJID(chatID)
	if err != nil {
		return appstore.SavedTextMessage{}, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}
	targetJID = c.normalizeJIDForChat(ctx, targetJID)
	chatID = targetJID.String()

	label := strings.TrimSpace(name)
	if label == "" {
		label = fmt.Sprintf("%.5f, %.5f", lat, long)
	}
	locationMsg := &waE2E.LocationMessage{
		DegreesLatitude:  proto.Float64(lat),
		DegreesLongitude: proto.Float64(long),
	}
	if name = strings.TrimSpace(name); name != "" {
		locationMsg.Name = proto.String(name)
	}
	if address = strings.TrimSpace(address); address != "" {
		locationMsg.Address = proto.String(address)
	}
	messageID := client.GenerateMessageID()
	if _, err := client.SendMessage(ctx, targetJID, &waE2E.Message{LocationMessage: locationMsg}, whatsmeow.SendRequestExtra{ID: messageID}); err != nil {
		return appstore.SavedTextMessage{}, err
	}
	payload, _ := proto.Marshal(locationMsg)
	saved, err := c.store.SaveMediaMessage(ctx, appstore.MediaMessageInput{
		TextMessageInput: appstore.TextMessageInput{
			ID:          internalMessageIDForChat(chatID, messageID),
			ChatID:      chatID,
			SenderID:    "me",
			Text:        label,
			Timestamp:   time.Now(),
			Direction:   appstore.DirectionOutgoing,
			Status:      appstore.StatusSent,
			IsGroup:     targetJID.Server == types.GroupServer || targetJID.Server == types.BroadcastServer,
			CountUnread: false,
		},
		MediaKind:     appstore.MediaKindLocation,
		MediaMimeType: "text/x-location",
		MediaPayload:  payload,
		GeoLat:        lat,
		GeoLong:       long,
	})
	if err != nil {
		return appstore.SavedTextMessage{}, err
	}
	if saved.Inserted {
		c.daemon.PublishNewMessage(toDaemonMessage(saved.Message), toDaemonChat(saved.Chat))
	}
	return saved, nil
}

// locationInput stores an inbound location pin.
func (c *Client) locationInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	location := evt.Message.GetLocationMessage()
	if location == nil {
		if live := evt.Message.GetLiveLocationMessage(); live != nil {
			location = &waE2E.LocationMessage{
				DegreesLatitude:  live.DegreesLatitude,
				DegreesLongitude: live.DegreesLongitude,
				JPEGThumbnail:    live.JPEGThumbnail,
				ContextInfo:      live.ContextInfo,
			}
		}
	}
	if location == nil {
		return appstore.MediaMessageInput{}, false
	}
	label := strings.TrimSpace(location.GetName())
	if label == "" {
		label = strings.TrimSpace(location.GetAddress())
	}
	if label == "" {
		label = fmt.Sprintf("%.5f, %.5f", location.GetDegreesLatitude(), location.GetDegreesLongitude())
	}
	base, chatID, ok := c.mediaInputBase(ctx, evt, opts, label, location.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}
	payload, err := proto.Marshal(location)
	if err != nil {
		return appstore.MediaMessageInput{}, false
	}
	_ = chatID
	return appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindLocation,
		MediaMimeType:    "text/x-location",
		MediaPayload:     payload,
		GeoLat:           location.GetDegreesLatitude(),
		GeoLong:          location.GetDegreesLongitude(),
	}, true
}
