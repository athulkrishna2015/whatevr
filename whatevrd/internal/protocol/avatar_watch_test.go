package protocol

import (
	"context"
	"testing"

	"whatevrd/internal/app"
	"whatevrd/internal/store"
)

type avatarRows struct {
	rows []store.Message
	// during runs while the read is in flight
	during func()
}

func (p *avatarRows) read() ([]store.Message, error) {
	if p.during != nil {
		p.during()
	}
	return p.rows, nil
}

func (p *avatarRows) ListStarredMessages(context.Context, string, int, string) ([]store.StarredMessage, error) {
	return nil, nil
}

func (p *avatarRows) ListPinnedMessages(context.Context, string) ([]store.Message, error) {
	return p.read()
}

func (p *avatarRows) ListChatMediaMessages(context.Context, string, int, string) ([]store.Message, error) {
	return p.read()
}

// TestRowViewsWakeForTheAvatarsTheyShow: the pinned and media views wake for
// an avatar of a sender or the chat in their rows, for any avatar before
// their first read or during one, and not otherwise.
func TestRowViewsWakeForTheAvatarsTheyShow(t *testing.T) {
	avatar := func(id string) *app.DaemonEvent {
		return &app.DaemonEvent{Kind: app.DaemonEventAvatarUpdated, Avatar: app.Avatar{ID: id}}
	}
	type session struct {
		affects func(*app.DaemonEvent) bool
		items   func()
	}
	for name, open := range map[string]func(*avatarRows) session{
		"pinned": func(l *avatarRows) session {
			s := &pinnedSession{lister: l, chatID: "c@g.us", ctx: context.Background()}
			return session{s.eventAffects, func() { s.Items(0) }}
		},
		"chat_media": func(l *avatarRows) session {
			s := &chatMediaSession{lister: l, chatID: "c@g.us", ctx: context.Background()}
			return session{s.eventAffects, func() { s.Items(0) }}
		},
	} {
		lister := &avatarRows{rows: []store.Message{{ID: "c@g.us:1", ChatID: "c@g.us", SenderID: "asha@s.whatsapp.net"}}}
		s := open(lister)
		if !s.affects(avatar("someone@s.whatsapp.net")) {
			t.Fatalf("%s: before the first read every avatar has to wake it", name)
		}
		var midRead bool
		lister.during = func() { midRead = s.affects(avatar("someone@s.whatsapp.net")) }
		s.items()
		if !midRead {
			t.Fatalf("%s: an avatar landing during a read has to wake it", name)
		}
		for id, want := range map[string]bool{"asha@s.whatsapp.net": true, "c@g.us": true, "someone@s.whatsapp.net": false} {
			if got := s.affects(avatar(id)); got != want {
				t.Errorf("%s: avatar %s wakes it: %v, want %v", name, id, got, want)
			}
		}
	}
}
