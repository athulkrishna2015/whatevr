//go:build whatevr_mock

package wamock

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	Register(Scenario{
		Name:        "test:send",
		Description: "fixture for the outbound, receipt and history tests",
		Build: func(w *World) {
			asha := w.Contact("917770000001", "Asha").SavedAs("Asha Kapoor")
			ravi := w.Contact("917770000002", "Ravi")

			direct := w.DM(asha)
			direct.History(asha, "older, from a history sync", Ago(48*time.Hour))
			direct.Say(asha, "say something", Ago(time.Minute))

			group := w.Group("Send Group", asha, ravi)
			group.Say(ravi, "and in here too", Ago(time.Minute))
			group.Pin()

			w.Receipts(50*time.Millisecond, 150*time.Millisecond)
			w.SetOnline(asha, true)
			w.OnSend(func(m *Msg) {
				replier := m.Chat.Other()
				m.Chat.Typing(replier, 20*time.Millisecond)
				m.Chat.Reply(replier, "echo: "+m.Text)
			})
		},
	})
}

// awaitReceipt waits for a receipt of the given type for one message id.
func awaitReceipt(ctx context.Context, t *testing.T, ch <-chan *events.Receipt, id string, kind types.ReceiptType) *events.Receipt {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case evt := <-ch:
			for _, got := range evt.MessageIDs {
				if got == id && evt.Type == kind {
					return evt
				}
			}
		case <-deadline:
			t.Fatalf("no %q receipt for %s", kind, id)
		case <-ctx.Done():
			t.Fatalf("context ended waiting for a %q receipt", kind)
		}
	}
}

// TestSendLifecycle is stage 2 end to end: the client encrypts a message the
// mock has to decrypt with a Signal session it built from its own published
// prekeys, and the delivery and read receipts come back for it.
func TestSendLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	srv, cli, msgs := dialMock(ctx, t, Options{Seed: 21, Scenario: "test:send"})
	receipts := make(chan *events.Receipt, 64)
	cli.AddEventHandler(func(raw any) {
		if evt, ok := raw.(*events.Receipt); ok {
			select {
			case receipts <- evt:
			default:
			}
		}
	})
	// Drain the backlog so the reply is the next thing that shows up.
	collect(ctx, t, msgs, 2)

	asha := types.JID{User: "917770000001", Server: types.DefaultUserServer}
	resp, err := cli.SendMessage(ctx, asha, &waE2E.Message{Conversation: proto.String("hello from the test")})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp.Timestamp.IsZero() {
		t.Error("the server ack carried no timestamp, so the message would be stored in 1970")
	}

	awaitReceipt(ctx, t, receipts, resp.ID, types.ReceiptTypeDelivered)
	awaitReceipt(ctx, t, receipts, resp.ID, types.ReceiptTypeRead)

	reply := collect(ctx, t, msgs, 1)[0]
	if got := reply.Message.GetConversation(); got != "echo: hello from the test" {
		t.Errorf("reply says %q, want the echo of what was sent", got)
	}
	if reply.Info.Chat.ToNonAD() != asha {
		t.Errorf("reply landed in %s, want the chat with %s", reply.Info.Chat, asha)
	}
	if _, ok := srv.peerFor(asha, srv.encryptionJID(asha)); ok != nil {
		t.Errorf("peer for %s: %v", asha, ok)
	}
}

// TestGroupSendDecrypts covers the other half of stage 2: a group message is
// one sender-key ciphertext for everybody, with the key itself handed out in
// the per-device copies, so opening it takes both steps.
func TestGroupSendDecrypts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, cli, msgs := dialMock(ctx, t, Options{Seed: 22, Scenario: "test:send"})
	backlog := collect(ctx, t, msgs, 2)
	group := find(t, backlog, "and in here too").Info.Chat

	if _, err := cli.SendMessage(ctx, group, &waE2E.Message{Conversation: proto.String("into the group")}); err != nil {
		t.Fatalf("send: %v", err)
	}
	reply := collect(ctx, t, msgs, 1)[0]
	if got := reply.Message.GetConversation(); got != "echo: into the group" {
		t.Errorf("group reply says %q, want the echo of what was sent", got)
	}
	if reply.Info.Chat != group {
		t.Errorf("group reply landed in %s, want %s", reply.Info.Chat, group)
	}
}

// TestHistorySyncArrives is stage 3: the notification points at a real
// encrypted blob on the mock's media host, and the client does the whole
// download, decrypt and decompress on it.
func TestHistorySyncArrives(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	history := make(chan *waE2E.HistorySyncNotification, 8)
	opts := Options{Seed: 23, Scenario: "test:send"}
	srv, cli, _ := dialMock(ctx, t, opts)
	cli.AddEventHandler(func(raw any) {
		evt, ok := raw.(*events.Message)
		if !ok {
			return
		}
		notif := evt.Message.GetProtocolMessage().GetHistorySyncNotification()
		if notif == nil {
			notif = evt.RawMessage.GetProtocolMessage().GetHistorySyncNotification()
		}
		if notif != nil {
			select {
			case history <- notif:
			default:
			}
		}
	})

	var blob *waE2E.HistorySyncNotification
	select {
	case blob = <-history:
	case <-time.After(30 * time.Second):
		t.Fatal("no history sync notification arrived")
	case <-ctx.Done():
		t.Fatal("context ended waiting for a history sync")
	}

	data, err := cli.DownloadHistorySync(ctx, blob, true)
	if err != nil {
		t.Fatalf("download history sync: %v", err)
	}
	var found bool
	for _, conv := range data.GetConversations() {
		for _, msg := range conv.GetMessages() {
			if msg.GetMessage().GetMessage().GetConversation() == "older, from a history sync" {
				found = true
			}
		}
	}
	if !found && len(data.GetInlineContacts()) == 0 {
		t.Error("the first chunk carried neither the history message nor the contacts")
	}
	if srv.world == nil {
		t.Fatal("server has no world")
	}
}

// TestAppStateRoundTrip covers the part of app state stage 2 depends on: the
// push name, without which whatsmeow refuses to send presence at all, and a pin
// the client makes surviving the re-fetch it does straight afterwards.
func TestAppStateRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	synced := make(chan appstate.WAPatchName, 16)
	srv, cli, _ := dialMock(ctx, t, Options{Seed: 24, Scenario: "test:send"})
	cli.AddEventHandler(func(raw any) {
		if evt, ok := raw.(*events.AppStateSyncComplete); ok {
			select {
			case synced <- evt.Name:
			default:
			}
		}
	})
	// Both collections matter: critical_block carries the push name, and a pin
	// sent before regular_low has caught up is encoded from the wrong version.
	waiting := map[appstate.WAPatchName]bool{
		appstate.WAPatchCriticalBlock: true,
		appstate.WAPatchRegularLow:    true,
	}
	for len(waiting) > 0 {
		select {
		case name := <-synced:
			delete(waiting, name)
		case <-time.After(30 * time.Second):
			t.Fatalf("app state never synced %v", waiting)
		}
	}

	// Presence is the thing that actually depends on it: whatsmeow refuses to
	// send any without a push name, and the daemon sends one before every
	// presence subscription.
	if err := cli.SendPresence(ctx, types.PresenceAvailable); err != nil {
		t.Fatalf("send presence: %v", err)
	}

	asha := types.JID{User: "917770000001", Server: types.DefaultUserServer}
	if err := cli.SendAppState(ctx, appstate.BuildPin(asha, true)); err != nil {
		t.Fatalf("send app state: %v", err)
	}
	// The server has to have kept it: whatsmeow re-fetches immediately after a
	// send, and a server that forgot would answer with the old state and undo
	// what the user just did.
	srv.appState.mu.Lock()
	patches := len(srv.appState.patches[appstate.WAPatchRegularLow])
	srv.appState.mu.Unlock()
	if patches < 2 {
		t.Errorf("regular_low holds %d patches, want the scenario's pin plus the one just sent", patches)
	}
}

// TestProfilePicture is the avatar half of stage 3: a real jpeg, hosted on the
// mock's own media host, fetched over the redirected transport.
func TestProfilePicture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, cli, _ := dialMock(ctx, t, Options{Seed: 25, Scenario: "test:send"})
	asha := types.JID{User: "917770000001", Server: types.DefaultUserServer}

	info, err := cli.GetProfilePictureInfo(ctx, asha, nil)
	if err != nil {
		t.Fatalf("profile picture: %v", err)
	}
	if info.ID == "" || info.URL == "" {
		t.Fatalf("profile picture info is empty: %+v", info)
	}
	// Asking again with the id we already have must say nothing changed, which
	// is the path the daemon's avatar cache takes on every refresh.
	again, err := cli.GetProfilePictureInfo(ctx, asha, &whatsmeow.GetProfilePictureParams{ExistingID: info.ID})
	if err != nil {
		t.Fatalf("profile picture refetch: %v", err)
	}
	if again != nil {
		t.Errorf("refetch with a known id returned %+v, want nil for unchanged", again)
	}

	stranger := types.JID{User: "919999999999", Server: types.DefaultUserServer}
	if _, err := cli.GetProfilePictureInfo(ctx, stranger, nil); err == nil {
		t.Error("somebody who is not in the world has a profile picture")
	}
}
