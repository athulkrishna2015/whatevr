package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"whatevrd/internal/app"
	"whatevrd/internal/store"
)

// LiveLocationLister supplies the `live_locations` view. *store.DB implements it.
type LiveLocationLister interface {
	ListLiveLocationShares(ctx context.Context, chatID string, now int64) ([]store.LiveLocationShare, error)
	GetMessage(ctx context.Context, id string) (store.Message, error)
}

// liveLocationsView is the set of live-location shares still running in one
// chat: who is sharing, since when, until when, and where they were last seen.
//
// It is a view of its own rather than a field on the message row for the reason
// PROTOCOL.md gives under Granularity: a position that changes every few seconds
// and a message row that does not have no business being the same item. A chat
// header or a banner subscribes to this and never re-renders a transcript; the
// transcript subscribes to `messages` and never churns on a moving pin.
type liveLocationsView struct {
	daemon *app.Daemon
	lister LiveLocationLister
}

type liveLocationsParams struct {
	ChatID string `json:"chat_id"`
}

func (v liveLocationsView) Open(params json.RawMessage, invalidate func()) (ViewSession, map[string]any, *Error) {
	var p liveLocationsParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, nil, errorf(CodeInvalidParams, "malformed live_locations params")
		}
	}
	if p.ChatID == "" {
		return nil, nil, errorf(CodeInvalidParams, "live_locations params must carry a chat_id")
	}

	events, cancel := v.daemon.SubscribeDaemonEvents()
	ctx, cancelCtx := context.WithCancel(context.Background())
	s := &liveLocationsSession{
		lister:       v.lister,
		chatID:       p.ChatID,
		eventsCancel: cancel,
		ctx:          ctx,
		cancelCtx:    cancelCtx,
		done:         make(chan struct{}),
	}
	go s.run(events, invalidate)
	return s, nil, nil
}

type liveLocationsSession struct {
	lister       LiveLocationLister
	chatID       string
	eventsCancel func()
	ctx          context.Context
	cancelCtx    context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once
}

// liveLocationsTick re-reads the window on a timer as well as on events,
// because a share ending is the passage of time rather than something the
// daemon was told about. Ten seconds is finer than the "N minutes left" the
// banner shows, and this view is at most a handful of rows.
const liveLocationsTick = 10 * time.Second

func (s *liveLocationsSession) run(events <-chan app.DaemonEvent, invalidate func()) {
	ticker := time.NewTicker(liveLocationsTick)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			invalidate()
		case evt := <-events:
			if s.eventAffects(evt) {
				invalidate()
			}
		}
	}
}

func (s *liveLocationsSession) eventAffects(evt app.DaemonEvent) bool {
	switch evt.Kind {
	case app.DaemonEventResync:
		return true
	case app.DaemonEventLiveLocationsChanged:
		return evt.Chat.ID == s.chatID
	case app.DaemonEventMessageDeleted, app.DaemonEventChatDeleted:
		return evt.DeletedChatID == s.chatID
	case app.DaemonEventChatCleared:
		return evt.Chat.ID == s.chatID
	default:
		return false
	}
}

// liveLocationItem is one running share.
type liveLocationItem struct {
	// ID is the opening message's id, so a frontend can scroll straight to the
	// bubble the banner is talking about.
	ID        string                 `json:"id"`
	ChatID    string                 `json:"chat_id"`
	Sender    messageSender          `json:"sender"`
	StartedAt int64                  `json:"started_at"`
	ExpiresAt int64                  `json:"expires_at,omitempty"`
	UpdatedAt int64                  `json:"updated_at,omitempty"`
	Location  *store.LocationPayload `json:"location,omitempty"`
}

func (s *liveLocationsSession) Items(max int) []Item {
	items, _ := s.ItemsErr(max)
	return items
}

func (s *liveLocationsSession) ItemsErr(max int) ([]Item, error) {
	if s.lister == nil {
		return nil, nil
	}
	shares, err := s.lister.ListLiveLocationShares(s.ctx, s.chatID, time.Now().Unix())
	if err != nil {
		log.Printf("protocol: list live locations for view: %v", err)
		return nil, err
	}
	if max > 0 && len(shares) > max {
		shares = shares[:max]
	}

	items := make([]Item, 0, len(shares))
	for _, share := range shares {
		message, err := s.lister.GetMessage(s.ctx, share.MessageID)
		if err != nil {
			// The row is gone (deleted for me, chat cleared) but the share row
			// outlived it for a moment. Skip rather than emit a nameless item.
			continue
		}
		items = append(items, Item{
			ID: share.MessageID,
			// Newest share first, which is the order a banner wants to read
			// them in when more than one person is sharing.
			Sort: fmt.Sprintf("%020d-%s", int64(1)<<62-share.StartedAt, share.MessageID),
			Data: liveLocationItem{
				ID:        share.MessageID,
				ChatID:    share.ChatID,
				Sender:    messageSender{ID: message.SenderID, Name: message.SenderName, AvatarPath: message.SenderAvatarLocalPath},
				StartedAt: share.StartedAt,
				ExpiresAt: share.ExpiresAt,
				UpdatedAt: share.LastUpdateAt,
				Location:  store.DecodePayload(message.PayloadJSON).Location,
			},
		})
	}
	return items, nil
}

func (s *liveLocationsSession) Close() {
	s.closeOnce.Do(func() {
		s.cancelCtx()
		close(s.done)
		s.eventsCancel()
	})
}
