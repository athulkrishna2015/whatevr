package wa

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// Scheduled events, both directions.
//
// An EventMessage is a plan: a name, a time, optionally a place or a call link.
// RSVPs come back as EncEventResponseMessage, encrypted against the event's
// message secret exactly as poll votes are, except that whatsmeow exports no
// helper for this one (see msgsecret_event.go).
//
// A response is one person's whole current answer, not a delta, so applying one
// replaces whatever they said before.

func (c *Client) eventMessageInput(ctx context.Context, evt *events.Message, opts ingestOptions) (appstore.MediaMessageInput, bool) {
	if evt == nil || evt.Message == nil {
		return appstore.MediaMessageInput{}, false
	}
	event := evt.Message.GetEventMessage()
	if event == nil {
		return appstore.MediaMessageInput{}, false
	}

	base, chatID, ok := c.mediaInputBase(ctx, evt, opts, "", event.GetContextInfo())
	if !ok {
		return appstore.MediaMessageInput{}, false
	}

	payload := &appstore.EventPayload{
		Name:               strings.TrimSpace(event.GetName()),
		Description:        strings.TrimSpace(event.GetDescription()),
		StartsAt:           event.GetStartTime(),
		EndsAt:             event.GetEndTime(),
		Canceled:           event.GetIsCanceled(),
		JoinLink:           strings.TrimSpace(event.GetJoinLink()),
		ExtraGuestsAllowed: event.GetExtraGuestsAllowed(),
		ScheduleCall:       event.GetIsScheduleCall(),
		ReminderOffsetSecs: event.GetReminderOffsetSec(),
	}

	input := appstore.MediaMessageInput{
		TextMessageInput: base,
		MediaKind:        appstore.MediaKindEvent,
		PayloadSummary:   payload.Name,
	}

	// An event's venue is an ordinary shared place, so it gets the same map the
	// location bubble gets: same payload shape, same stitched PNG, same
	// download lifecycle. Writing a second, lesser map for events would be the
	// only reason events ever looked different from places.
	if location := event.GetLocation(); location != nil {
		payload.Location = locationPayloadFromMessage(location)
		input.MediaMimeType = "image/png"
		input.MediaThumbnailLocalPath = c.saveMessageThumbnail(chatID, base.ID, location.GetJPEGThumbnail())
		input.MediaWidth = mapOutputWidth
		input.MediaHeight = mapOutputHeight
	}

	encoded, err := appstore.EncodePayload(appstore.MessagePayload{Event: payload})
	if err != nil {
		c.log.Warnf("Failed to encode event payload for %s: %v", base.ID, err)
		return appstore.MediaMessageInput{}, false
	}
	input.PayloadJSON = encoded
	return input, true
}

// handleEventResponse folds somebody's RSVP into its event. Returns true when
// the event was consumed and must not become a message row of its own: an
// answer is a change to the plan, not a line in the conversation.
func (c *Client) handleEventResponse(ctx context.Context, evt *events.Message) bool {
	if evt == nil || evt.Message == nil {
		return false
	}
	enc := evt.Message.GetEncEventResponseMessage()
	if enc == nil {
		return false
	}

	chatJID := c.normalizeJIDForChat(ctx, evt.Info.Chat)
	eventID := internalMessageIDForChat(chatJID.String(), enc.GetEventCreationMessageKey().GetID())
	responder := senderID(evt.Info)

	response, err := c.decryptEventResponse(ctx, evt)
	if err != nil {
		// Almost always the event itself not having arrived yet, so its message
		// secret is not stored. Unlike a poll vote there is nothing to park it
		// against: an RSVP carries no useful state until the event exists, and
		// WhatsApp redelivers responses on reconnect anyway.
		c.log.Debugf("Failed to decrypt an event response on %s: %v", eventID, err)
		return true
	}

	respondedAt := evt.Info.Timestamp.Unix()
	if ms := response.GetTimestampMS(); ms > 0 {
		respondedAt = ms / 1000
	}
	applied, err := c.store.ApplyEventResponse(ctx, eventID, responder,
		eventResponseValue(response.GetResponse()), int(response.GetExtraGuestCount()), respondedAt)
	if err != nil {
		c.log.Warnf("Failed to apply an event response on %s: %v", eventID, err)
		return true
	}
	if applied {
		c.publishEventUpdated(ctx, eventID)
	}
	return true
}

// eventResponseValue maps the wire enum to the string the store and the wire
// both use. UNKNOWN becomes "maybe" rather than an empty answer, because a
// response we cannot name is still somebody having answered.
func eventResponseValue(response waE2E.EventResponseMessage_EventResponseType) string {
	switch response {
	case waE2E.EventResponseMessage_GOING:
		return appstore.EventResponseGoing
	case waE2E.EventResponseMessage_NOT_GOING:
		return appstore.EventResponseNotGoing
	default:
		return appstore.EventResponseMaybe
	}
}

// eventResponseType is the inverse, for our own outgoing answer.
func eventResponseType(value string) (waE2E.EventResponseMessage_EventResponseType, bool) {
	switch value {
	case appstore.EventResponseGoing:
		return waE2E.EventResponseMessage_GOING, true
	case appstore.EventResponseNotGoing:
		return waE2E.EventResponseMessage_NOT_GOING, true
	case appstore.EventResponseMaybe:
		return waE2E.EventResponseMessage_MAYBE, true
	default:
		return waE2E.EventResponseMessage_UNKNOWN, false
	}
}

func (c *Client) publishEventUpdated(ctx context.Context, eventID string) {
	message, err := c.store.GetMessage(ctx, eventID)
	if err != nil {
		return
	}
	c.daemon.PublishMessageUpdated(toDaemonMessage(message))
}

// RespondToEvent sends our own RSVP.
//
// Like a poll vote, the answer is written before the send so the chips move on
// the same frame as the tap, and put back if the send fails: showing an answer
// nobody else will ever receive is worse than showing none.
func (c *Client) RespondToEvent(ctx context.Context, messageID, response string, extraGuests int) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "message_id is required")
	}
	responseType, ok := eventResponseType(strings.TrimSpace(response))
	if !ok {
		return app.NewCommandError(app.CommandErrorInvalidArgument,
			"response must be one of %s, %s, %s",
			appstore.EventResponseGoing, appstore.EventResponseNotGoing, appstore.EventResponseMaybe)
	}
	if extraGuests < 0 {
		extraGuests = 0
	}

	client := c.currentClient()
	if client == nil || !client.IsLoggedIn() {
		return app.NewCommandError(app.CommandErrorNotLoggedIn, "not logged in")
	}

	message, err := c.store.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}
	if message.MediaKind != appstore.MediaKindEvent {
		return app.NewCommandError(app.CommandErrorInvalidArgument, "message %s is not an event", messageID)
	}
	payload := appstore.DecodePayload(message.PayloadJSON).Event
	if payload != nil && payload.Canceled {
		return app.NewCommandError(app.CommandErrorRejected, "this event was canceled")
	}
	if payload != nil && !payload.ExtraGuestsAllowed {
		extraGuests = 0
	}

	eventInfo, err := c.eventMessageInfo(message)
	if err != nil {
		return err
	}

	own := client.Store.GetJID()
	if own.IsEmpty() {
		return app.NewCommandError(app.CommandErrorNotLoggedIn, "not logged in")
	}

	now := time.Now()
	sealed, err := c.encryptEventResponse(ctx, eventInfo, own, &waE2E.EventResponseMessage{
		Response:        responseType.Enum(),
		TimestampMS:     ptrTo(now.UnixMilli()),
		ExtraGuestCount: ptrTo(int32(extraGuests)),
	})
	if err != nil {
		return app.NewCommandError(app.CommandErrorRejected, "build event response: %v", err)
	}

	selfJID := own.ToNonAD().String()
	previousResponse, previousGuests, err := c.store.EventResponseOf(ctx, messageID, selfJID)
	if err != nil {
		return err
	}
	if selfJID != "" {
		if _, err := c.store.ApplyEventResponse(ctx, messageID, selfJID,
			strings.TrimSpace(response), extraGuests, now.Unix()); err != nil {
			return err
		}
		c.publishEventUpdated(ctx, messageID)
	}

	if _, err := client.SendMessage(ctx, eventInfo.Chat, sealed); err != nil {
		if selfJID != "" {
			c.undoEventResponse(ctx, messageID, selfJID, previousResponse, previousGuests, now)
			c.publishEventUpdated(ctx, messageID)
		}
		return app.NewCommandError(app.CommandErrorRejected, "send event response: %v", err)
	}
	return nil
}

// undoEventResponse puts back what we had answered before an RSVP that failed
// to send. Answering nothing before means the row goes away entirely, rather
// than being rewritten to an empty answer that would show as a chip.
func (c *Client) undoEventResponse(ctx context.Context, messageID, selfJID, previous string, guests int, now time.Time) {
	if previous == "" {
		if err := c.store.ClearEventResponse(ctx, messageID, selfJID); err != nil {
			c.log.Warnf("Failed to undo an unsent RSVP on %s: %v", messageID, err)
		}
		return
	}
	if _, err := c.store.ApplyEventResponse(ctx, messageID, selfJID, previous, guests, now.Unix()); err != nil {
		c.log.Warnf("Failed to undo an unsent RSVP on %s: %v", messageID, err)
	}
}

// eventMessageInfo rebuilds the MessageInfo the encryption needs from the
// stored row: which chat the event is in, who created it and its external id.
func (c *Client) eventMessageInfo(message appstore.Message) (*types.MessageInfo, error) {
	chatJID, err := types.ParseJID(message.ChatID)
	if err != nil {
		return nil, app.NewCommandError(app.CommandErrorInvalidArgument, "invalid chat_id: %v", err)
	}
	_, externalID, found := strings.Cut(message.ID, ":")
	if !found {
		return nil, app.NewCommandError(app.CommandErrorInvalidArgument, "malformed message id %q", message.ID)
	}

	sender := chatJID
	if message.Direction == appstore.DirectionOutgoing {
		if client := c.currentClient(); client != nil {
			if own := client.Store.GetJID(); !own.IsEmpty() {
				sender = own.ToNonAD()
			}
		}
	} else if parsed, err := types.ParseJID(message.SenderID); err == nil {
		sender = parsed
	}

	return &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chatJID,
			Sender:   sender,
			IsFromMe: message.Direction == appstore.DirectionOutgoing,
			IsGroup:  chatJID.Server == types.GroupServer,
		},
		ID: externalID,
	}, nil
}

// eventSummary is the detail on the one-line rendering: the event's name, which
// is the whole point of it.
func eventSummary(event *waE2E.EventMessage) string {
	if event == nil {
		return ""
	}
	return strings.TrimSpace(event.GetName())
}

func ptrTo[T any](value T) *T {
	return &value
}
