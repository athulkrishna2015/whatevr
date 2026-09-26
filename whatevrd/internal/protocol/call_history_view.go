package protocol

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"whatevrd/internal/app"
	"whatevrd/internal/store"
)

// CallHistoryLister supplies the `call_history` view its rows. *store.DB
// implements it.
type CallHistoryLister interface {
	ListCallLogMessages(ctx context.Context, limit int) ([]store.CallLogMessage, error)
}

// callHistoryView is the Calls page's recent section: a live-edge prefix window
// over every chat's call-log tombstones, newest first, each row carrying its
// chat's display name so the cross-chat list can label it and open the chat. It
// reuses the `messages` item shape plus `chat_name`, exactly as `starred`
// does; a row's `direction` and `call_log` object carry everything a calls list
// needs to render it.
type callHistoryView struct {
	daemon *app.Daemon
	lister CallHistoryLister
}

// callHistoryItem is a call-history row: the whole `messages` item plus the
// chat's display name. The embedded messageItem's fields, including `id`,
// promote to the top-level object.
type callHistoryItem struct {
	messageItem
	ChatName string `json:"chat_name,omitempty"`
}

func (v callHistoryView) Open(_ json.RawMessage, invalidate func()) (ViewSession, map[string]any, *Error) {
	events, cancel := v.daemon.SubscribeDaemonEvents()
	ctx, cancelCtx := context.WithCancel(context.Background())
	s := &callHistorySession{lister: v.lister, eventsCancel: cancel, ctx: ctx, cancelCtx: cancelCtx, done: make(chan struct{})}
	go s.run(events, invalidate)
	return s, nil, nil
}

type callHistorySession struct {
	lister       CallHistoryLister
	eventsCancel func()
	ctx          context.Context
	cancelCtx    context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once
}

func (s *callHistorySession) run(events <-chan app.DaemonEvent, invalidate func()) {
	for {
		select {
		case <-s.done:
			return
		case evt := <-events:
			if s.eventAffects(evt) {
				invalidate()
			}
		}
	}
}

// eventAffects reports whether an event may have changed this window's rows.
// A finished call lands as a brand-new call-log message, so unlike `starred`
// this also listens for NewMessage. Deletes/clears and a resync can move rows;
// an avatar refresh only re-colors one (a cheap re-read that diffs to nothing).
// The view spans every chat, so no event is ever out of scope.
func (s *callHistorySession) eventAffects(evt app.DaemonEvent) bool {
	switch evt.Kind {
	case app.DaemonEventResync,
		app.DaemonEventNewMessage,
		app.DaemonEventMessageUpdated,
		app.DaemonEventMessageDeleted,
		app.DaemonEventChatDeleted,
		app.DaemonEventChatCleared,
		app.DaemonEventAvatarUpdated:
		return true
	default:
		return false
	}
}

// Items returns the newest `max` call-log rows slice-ordered newest-first (the
// engine keeps the prefix = the newest), each carrying a newest-first sort key
// so generic clients render the recent list in the same order.
func (s *callHistorySession) Items(max int) []Item {
	items, _ := s.ItemsErr(max)
	return items
}

func (s *callHistorySession) ItemsErr(max int) ([]Item, error) {
	if s.lister == nil {
		return nil, nil
	}
	limit := max
	if limit <= 0 {
		limit = messagesUnboundedLimit
	}
	rows, err := s.lister.ListCallLogMessages(s.ctx, limit)
	if err != nil {
		log.Printf("protocol: list call log messages for view: %v", err)
		return nil, err
	}
	items := make([]Item, 0, len(rows))
	for _, cm := range rows {
		items = append(items, Item{
			ID:   cm.ID,
			Sort: newestFirstSort(cm.Message),
			Data: callHistoryItem{messageItem: messageItemFromStore(cm.Message), ChatName: cm.ChatName},
		})
	}
	return items, nil
}

func (s *callHistorySession) Close() {
	s.closeOnce.Do(func() {
		s.cancelCtx()
		close(s.done)
		s.eventsCancel()
	})
}
