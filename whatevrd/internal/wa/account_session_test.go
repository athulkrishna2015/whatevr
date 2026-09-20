package wa

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

// Replacing the account must not leave a row of the previous one behind.
//
// Detached work used to run on context.WithoutCancel, so ending the account
// neither stopped it nor waited for it: a write already in flight landed after
// ClearSessionData had emptied the tables, and the next account opened with a
// chat that was not its own. The session has to drain before the wipe.
func TestAccountReplacementLeavesNoRowFromThePreviousAccount(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}
	client.session = newAccountSession(context.Background())

	if err := db.SetSelfJID(ctx, "111@s.whatsapp.net"); err != nil {
		t.Fatalf("SetSelfJID: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	client.spawn(func(ctx context.Context) {
		close(started)
		<-release
		_, _ = db.EnsureChat(ctx, "999@s.whatsapp.net", "previous account", false)
	})
	<-started

	drained := make(chan struct{})
	go func() {
		client.endSessionLocked()
		close(drained)
	}()

	select {
	case <-drained:
		t.Fatal("ending the account returned while its work was still in flight, so the wipe races the write")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("ending the account never drained")
	}

	if err := db.ClearSessionData(ctx); err != nil {
		t.Fatalf("ClearSessionData: %v", err)
	}
	client.beginSessionLocked(context.Background())
	defer client.endSessionLocked()

	chats, err := db.ListChats(ctx, 50, 0, "")
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}
	if len(chats) != 0 {
		t.Fatalf("the new account opened with %d chat(s) from the previous one", len(chats))
	}

	// The jid the store marks as "me" is account data too: left behind, the
	// previous account's votes stayed highlighted in every poll tally.
	if jid := db.CachedSelfJIDForTest(); jid != "" {
		t.Fatalf("self jid %q survived the wipe", jid)
	}
}
