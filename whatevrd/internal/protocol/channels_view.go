package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"

	"whatevrd/internal/app"
	"whatevrd/internal/store"
)

// ChannelLister supplies the `channels` view its rows. *store.DB implements
// it.
type ChannelLister interface {
	ListChannels(ctx context.Context) ([]store.Channel, error)
}

// ChannelActions fetches live channel messages. *wa.Client implements it.
type ChannelActions interface {
	GetChannelMessages(ctx context.Context, channelID string, count int, before int64) ([]app.ChannelMessage, error)
}

// channelActionsFrom narrows the daemon actions seam to the channel surface.
// Tests and the fixture pass nil when the views they exercise do not touch
// it; the view errors instead of panicking.
func channelActionsFrom(actions DaemonActions) ChannelActions {
	if actions == nil {
		return nil
	}
	if channelActions, ok := any(actions).(ChannelActions); ok {
		return channelActions
	}
	return nil
}

// channelsView is the followed-channels directory: one item per channel from
// the store cache, refreshed by `channels.refresh` (and follow/unfollow).
type channelsView struct {
	daemon *app.Daemon
	lister ChannelLister
}

func (v channelsView) Open(_ json.RawMessage, invalidate func()) (ViewSession, map[string]any, *Error) {
	events, cancel := v.daemon.SubscribeDaemonEvents()
	ctx, cancelCtx := context.WithCancel(context.Background())
	s := &channelsSession{
		lister:       v.lister,
		eventsCancel: cancel,
		ctx:          ctx,
		cancelCtx:    cancelCtx,
		done:         make(chan struct{}),
	}
	go s.run(events, invalidate)
	return s, nil, nil
}

type channelsSession struct {
	lister       ChannelLister
	eventsCancel func()
	ctx          context.Context
	cancelCtx    context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once
}

func (s *channelsSession) run(events <-chan app.DaemonEvent, invalidate func()) {
	for {
		select {
		case <-s.done:
			return
		case evt := <-events:
			switch evt.Kind {
			case app.DaemonEventChannelsChanged, app.DaemonEventResync:
				invalidate()
			}
		}
	}
}

type channelItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Followers   int    `json:"followers"`
	Verified    bool   `json:"verified,omitempty"`
	Muted       bool   `json:"muted,omitempty"`
}

// Items returns followed channels, most-followed first.
func (s *channelsSession) Items(max int) []Item {
	if s.lister == nil {
		return nil
	}
	rows, err := s.lister.ListChannels(s.ctx)
	if err != nil {
		log.Printf("protocol: list channels for view: %v", err)
		return nil
	}
	if max > 0 && len(rows) > max {
		rows = rows[:max]
	}
	items := make([]Item, 0, len(rows))
	for _, channel := range rows {
		items = append(items, Item{
			ID:   channel.ID,
			Sort: channel.ID,
			Data: channelItem{
				ID:          channel.ID,
				Name:        channel.Name,
				Description: channel.Description,
				Followers:   channel.Followers,
				Verified:    channel.Verified,
				Muted:       channel.Muted,
			},
		})
	}
	return items
}

func (s *channelsSession) Close() {
	s.closeOnce.Do(func() {
		s.cancelCtx()
		close(s.done)
		s.eventsCancel()
	})
}

// channelMessagesView is one channel's recent messages, newest first,
// fetched live on every fill (never stored). `extend older` pages back by
// server id.
type channelMessagesView struct {
	daemon  *app.Daemon
	actions ChannelActions
}

type channelMessagesParams struct {
	ChannelID string `json:"channel_id"`
}

func (v channelMessagesView) Open(params json.RawMessage, invalidate func()) (ViewSession, map[string]any, *Error) {
	var p channelMessagesParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, nil, errorf(CodeInvalidParams, "malformed channel_messages params")
		}
	}
	if p.ChannelID == "" {
		return nil, nil, errorf(CodeInvalidParams, "channel_messages params must carry a channel_id")
	}
	if v.actions == nil {
		return nil, nil, errorf(CodeInternal, "channel messages unavailable")
	}
	events, cancel := v.daemon.SubscribeDaemonEvents()
	ctx, cancelCtx := context.WithCancel(context.Background())
	s := &channelMessagesSession{
		actions:      v.actions,
		channelID:    p.ChannelID,
		eventsCancel: cancel,
		ctx:          ctx,
		cancelCtx:    cancelCtx,
		done:         make(chan struct{}),
	}
	go s.run(events, invalidate)
	return s, nil, nil
}

type channelMessagesSession struct {
	actions      ChannelActions
	channelID    string
	eventsCancel func()
	ctx          context.Context
	cancelCtx    context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once
}

func (s *channelMessagesSession) run(events <-chan app.DaemonEvent, invalidate func()) {
	for {
		select {
		case <-s.done:
			return
		case evt := <-events:
			switch evt.Kind {
			case app.DaemonEventChannelsChanged, app.DaemonEventResync:
				invalidate()
			}
		}
	}
}

type channelMessageItem struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Timestamp int64  `json:"timestamp"`
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	Fallback  string `json:"fallback"`
	Views     int    `json:"views,omitempty"`
}

// Items fetches the newest `max` messages live. Server ids order the feed;
// the sort key pads them so bytewise order matches numeric order.
func (s *channelMessagesSession) Items(max int) []Item {
	if s.actions == nil {
		return nil
	}
	limit := max
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.actions.GetChannelMessages(s.ctx, s.channelID, limit, 0)
	if err != nil {
		log.Printf("protocol: list channel messages for view: %v", err)
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ServerID > rows[j].ServerID })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	items := make([]Item, 0, len(rows))
	for _, msg := range rows {
		id := channelMessageID(s.channelID, msg.ServerID)
		items = append(items, Item{
			ID:   id,
			Sort: channelMessageSort(msg.ServerID),
			Data: channelMessageItem{
				ID:        id,
				ChannelID: s.channelID,
				Timestamp: msg.Timestamp,
				Kind:      msg.Kind,
				Text:      msg.Text,
				Fallback:  msg.Fallback,
				Views:     msg.Views,
			},
		})
	}
	return items
}

func (s *channelMessagesSession) Close() {
	s.closeOnce.Do(func() {
		s.cancelCtx()
		close(s.done)
		s.eventsCancel()
	})
}

func channelMessageID(channelID string, serverID int64) string {
	return fmt.Sprintf("%s:%d", channelID, serverID)
}

// channelMessageSort orders newest (largest server id) first under bytewise
// ascending comparison by inverting the id.
func channelMessageSort(serverID int64) string {
	return fmt.Sprintf("%020d", ^uint64(serverID))
}
