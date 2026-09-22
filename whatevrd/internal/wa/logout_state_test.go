package wa

import (
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// A logout empties what belonged to the account. It must not take the maps
// away with it: work started before the logout lands after it, and writing to
// a nil map is a panic that takes the daemon down and every frontend with it.
// A media download arriving after somebody unpaired their phone did exactly
// that.
func TestALogoutEmptiesTheAccountsMapsAndKeepsThem(t *testing.T) {
	c := &Client{
		avatarQueued:              map[appstore.AvatarSubject]avatarPriority{},
		offlineSyncChangedChats:   map[string]uint32{},
		groupParticipantsFresh:    map[string]time.Time{},
		groupParticipantsInFlight: map[string]bool{},
		sendTimings:               map[string]*sendTiming{},
		pendingAppState:           map[types.JID]pendingAppStateEntry{},
		pendingStars:              map[string]bool{},
		pendingRewrites:           map[string]pendingRewrite{},
		backfillInFlight:          map[string]*backfillRequest{},
		posterQueued:              map[string]posterPriority{},
		mediaDownloads:            map[string]*mediaDownloadState{},
		mediaRetries:              map[string]*mediaRetryState{},
		mediaStreams:              map[string]*mediaStreamEntry{},
		messageStickerLocks:       map[string]*messageStickerKeyLock{},
		stickerDownloads:          map[string]*stickerFileDownloadState{},
		stickerLibraryTimers:      map[app.StickerSource]*time.Timer{},
	}
	// Something in every one of them, so emptying is a thing the test can see.
	c.avatarQueued[appstore.AvatarSubject{ID: "a"}] = avatarPriority(1)
	c.offlineSyncChangedChats["chat"] = 1
	c.groupParticipantsFresh["group"] = time.Now()
	c.groupParticipantsInFlight["group"] = true
	c.sendTimings["msg"] = &sendTiming{}
	c.pendingAppState[types.JID{User: "1", Server: types.DefaultUserServer}] = pendingAppStateEntry{}
	c.pendingStars["msg"] = true
	c.pendingRewrites["msg"] = pendingRewrite{}
	c.backfillInFlight["chat"] = &backfillRequest{}
	c.posterQueued["msg"] = posterPriority(1)
	c.mediaDownloads["msg"] = &mediaDownloadState{done: make(chan struct{})}
	c.mediaRetries["msg"] = &mediaRetryState{}
	c.mediaStreams["msg"] = &mediaStreamEntry{}
	c.messageStickerLocks["msg"] = &messageStickerKeyLock{}
	c.stickerDownloads["sticker"] = &stickerFileDownloadState{done: make(chan struct{})}
	c.stickerLibraryTimers[app.StickerSourceRecent] = time.NewTimer(time.Hour)

	// Which maps the account had, before the logout takes what is in them. A
	// map this client never made is one this test never filled, and what the
	// reset does about it is the reset's business: the sessions a connection
	// holds are not the account's.
	value := reflect.ValueOf(c).Elem()
	fields := value.Type()
	var held []int
	for i := 0; i < value.NumField(); i++ {
		if field := value.Field(i); field.Kind() == reflect.Map && !field.IsNil() {
			held = append(held, i)
		}
	}

	c.resetInMemoryAccountState()

	for _, i := range held {
		field := value.Field(i)
		if field.IsNil() {
			t.Errorf("%s is nil after a logout, so the next write to it is a panic", fields.Field(i).Name)
			continue
		}
		if field.Len() != 0 {
			t.Errorf("%s still holds %d entries of the account that logged out",
				fields.Field(i).Name, field.Len())
		}
	}
}
