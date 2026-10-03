package wa

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

func TestShouldNotifyChatNoFocusedSession(t *testing.T) {
	c := &Client{frontendSessions: make(map[string]frontendSession)}
	c.FrontendSessionStarted("s1")
	c.FrontendSessionStateChanged("s1", false, "chat-a")

	if !c.ShouldNotifyChat("chat-a") {
		t.Fatal("unfocused session should not suppress notifications")
	}
}

func TestShouldNotifyChatRespectsArchivedPreference(t *testing.T) {
	c := &Client{frontendSessions: make(map[string]frontendSession)}
	c.appPrefs.Store(&app.AppPreferences{MuteArchivedChats: true})
	if c.shouldNotifyChat(app.Chat{ID: "archived", IsArchived: true}) {
		t.Fatal("archived chat should be muted by default preference")
	}
	if !c.shouldNotifyChat(app.Chat{ID: "active", IsArchived: false}) {
		t.Fatal("unarchived chat should still notify")
	}
	c.appPrefs.Store(&app.AppPreferences{MuteArchivedChats: false})
	if !c.shouldNotifyChat(app.Chat{ID: "archived", IsArchived: true}) {
		t.Fatal("archived chat should notify when archived muting is disabled")
	}
}

func TestLoadAppPreferencesArchivedMuteMigration(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want bool
	}{
		{name: "missing field defaults on", json: `{"NotificationsEnabled":true}`, want: true},
		{name: "explicit false remains off", json: `{"NotificationsEnabled":true,"MuteArchivedChats":false}`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "prefs.db"))
			if err != nil {
				t.Fatalf("open db: %v", err)
			}
			defer db.Close()
			if err := db.SetDaemonConfig(ctx, daemonConfigAppPreferencesKey, tc.json); err != nil {
				t.Fatalf("save preferences: %v", err)
			}
			c := &Client{store: db}
			c.loadAppPreferences(ctx)
			if got := c.appPreferences().MuteArchivedChats; got != tc.want {
				t.Fatalf("MuteArchivedChats = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldNotifyChatFocusedSameChat(t *testing.T) {
	c := &Client{frontendSessions: make(map[string]frontendSession)}
	c.FrontendSessionStarted("s1")
	c.FrontendSessionStateChanged("s1", true, "chat-a")

	if c.ShouldNotifyChat("chat-a") {
		t.Fatal("focused active chat should suppress notifications")
	}
}

func TestShouldNotifyChatFocusedDifferentChat(t *testing.T) {
	c := &Client{frontendSessions: make(map[string]frontendSession)}
	c.FrontendSessionStarted("s1")
	c.FrontendSessionStateChanged("s1", true, "chat-a")

	if !c.ShouldNotifyChat("chat-b") {
		t.Fatal("focused different chat should not suppress notifications")
	}
}

func TestShouldNotifyChatSessionEndClearsSuppression(t *testing.T) {
	c := &Client{frontendSessions: make(map[string]frontendSession)}
	c.FrontendSessionStarted("s1")
	c.FrontendSessionStateChanged("s1", true, "chat-a")
	c.FrontendSessionEnded("s1")

	if !c.ShouldNotifyChat("chat-a") {
		t.Fatal("ended session should not suppress notifications")
	}
}

func TestShouldNotifyChatAnyFocusedSessionSuppresses(t *testing.T) {
	c := &Client{frontendSessions: make(map[string]frontendSession)}
	c.FrontendSessionStarted("s1")
	c.FrontendSessionStarted("s2")
	c.FrontendSessionStateChanged("s1", false, "chat-a")
	c.FrontendSessionStateChanged("s2", true, "chat-a")

	if c.ShouldNotifyChat("chat-a") {
		t.Fatal("any focused session on chat should suppress notifications")
	}
}

func TestDesiredPresenceRequiresFocusedSession(t *testing.T) {
	sessions := map[string]frontendSession{
		"s1": {focused: false, activeChatID: "chat-a"},
	}

	if got := desiredPresenceForSessions(sessions); got != types.PresenceUnavailable {
		t.Fatalf("presence = %s, want unavailable", got)
	}
}

func TestDesiredPresenceAnyFocusedSessionAvailable(t *testing.T) {
	sessions := map[string]frontendSession{
		"s1": {focused: false, activeChatID: "chat-a"},
		"s2": {focused: true, activeChatID: "chat-b"},
	}

	if got := desiredPresenceForSessions(sessions); got != types.PresenceAvailable {
		t.Fatalf("presence = %s, want available", got)
	}
}

func TestDesiredPresenceNoSessionsUnavailable(t *testing.T) {
	if got := desiredPresenceForSessions(nil); got != types.PresenceUnavailable {
		t.Fatalf("presence = %s, want unavailable", got)
	}
}

// A QR retry stays on the login screen rather than falling through to the
// offline backoff. An expired code retries immediately; a failed attempt backs
// off, which is the only difference between the two.
func TestConnectionRetryKeepsQRLoginOnLoginRetry(t *testing.T) {
	retry := connectionRetry(3, fmt.Errorf("wrapped: %w", errQRCodeExpired))

	if retry.attempt != 0 {
		t.Fatalf("attempt = %d, want 0", retry.attempt)
	}
	if retry.delay != qrRetryDelay {
		t.Fatalf("delay = %v, want %v", retry.delay, qrRetryDelay)
	}
	if retry.state != app.StateNeedLogin {
		t.Fatalf("state = %v, want %v", retry.state, app.StateNeedLogin)
	}
	if retry.detail != "" {
		t.Fatalf("detail = %q, want empty to preserve QR detail", retry.detail)
	}
	if retry.canReconnect {
		t.Fatal("canReconnect = true, want false")
	}
	if retry.nextRetryUnix {
		t.Fatal("nextRetryUnix = true, want false")
	}
}

func TestConnectionRetryMarksNonQRFailuresOffline(t *testing.T) {
	retry := connectionRetry(0, fmt.Errorf("connect failed"))

	if retry.attempt != 1 {
		t.Fatalf("attempt = %d, want 1", retry.attempt)
	}
	if retry.delay < connBackoffBase || retry.delay >= connBackoffBase+time.Second {
		t.Fatalf("delay = %v, want first backoff range", retry.delay)
	}
	if retry.state != app.StateOffline {
		t.Fatalf("state = %v, want %v", retry.state, app.StateOffline)
	}
	if retry.detail == "" {
		t.Fatal("detail is empty, want retry detail")
	}
	if !retry.canReconnect {
		t.Fatal("canReconnect = false, want true")
	}
	if !retry.nextRetryUnix {
		t.Fatal("nextRetryUnix = false, want true")
	}
}

func TestIsAppStateConflictError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "lthash sentinel",
			err:  fmt.Errorf("wrapped: %w", appstate.ErrMismatchingLTHash),
			want: true,
		},
		{
			name: "server 409",
			err:  errors.New(`server returned error updating app state (regular_low): <error code="409" text="conflict"/>`),
			want: true,
		},
		{
			name: "verify patch",
			err:  errors.New("failed to verify patch v561: mismatching LTHash"),
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("network unavailable"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAppStateConflictError(tt.err); got != tt.want {
				t.Fatalf("isAppStateConflictError() = %v, want %v", got, tt.want)
			}
		})
	}
}
