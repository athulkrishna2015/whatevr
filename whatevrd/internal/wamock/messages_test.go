//go:build whatevr_mock

package wamock

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	Register(Scenario{
		Name:        "test:conversation",
		Description: "fixture for the package's own tests",
		Build: func(w *World) {
			asha := w.Contact("917770000001", "Asha")
			ravi := w.Contact("917770000002", "Ravi")

			direct := w.DM(asha)
			direct.Say(asha, "first", Ago(2*time.Hour))
			direct.SayFromMe("second", Ago(time.Hour))

			group := w.Group("Test Group", asha, ravi)
			group.Say(ravi, "in a group", Ago(30*time.Minute))

			w.After(0, func() { group.Say(asha, "live", time.Now()) })
		},
	})
}

// collect drains the client's message events until it has n of them, so a test
// can assert on a whole conversation rather than on whatever arrived first.
func collect(ctx context.Context, t *testing.T, ch <-chan *events.Message, n int) []*events.Message {
	t.Helper()
	out := make([]*events.Message, 0, n)
	deadline := time.After(30 * time.Second)
	for len(out) < n {
		select {
		case evt := <-ch:
			out = append(out, evt)
		case <-deadline:
			t.Fatalf("got %d messages, want %d", len(out), n)
		case <-ctx.Done():
			t.Fatalf("context ended with %d of %d messages", len(out), n)
		}
	}
	return out
}

func find(t *testing.T, msgs []*events.Message, text string) *events.Message {
	t.Helper()
	for _, evt := range msgs {
		if evt.Message.GetConversation() == text {
			return evt
		}
	}
	t.Fatalf("no message saying %q among %d", text, len(msgs))
	return nil
}

// TestScenarioMessagesArrive is the whole of stage 1 end to end: a scenario
// world, a real Signal session built from the client's own uploaded prekeys,
// and messages that come out the far side as decrypted events.
func TestScenarioMessagesArrive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, cli, msgs := dialMock(ctx, t, Options{Seed: 11, Scenario: "test:conversation"})
	received := collect(ctx, t, msgs, 4)

	asha := types.JID{User: "917770000001", Server: types.DefaultUserServer}
	ravi := types.JID{User: "917770000002", Server: types.DefaultUserServer}

	first := find(t, received, "first")
	if first.Info.Chat.ToNonAD() != asha {
		t.Errorf("incoming direct message landed in %s, want %s", first.Info.Chat, asha)
	}
	if first.Info.IsFromMe {
		t.Error("a message from a contact is marked as from me")
	}
	if first.Info.PushName != "Asha" {
		t.Errorf("push name is %q, want Asha", first.Info.PushName)
	}
	if time.Since(first.Info.Timestamp) < 90*time.Minute {
		t.Errorf("timestamp %s is not the two hours ago the scenario asked for", first.Info.Timestamp)
	}

	// A message the account sent elsewhere is the case that decides whether the
	// mock guessed the right Signal address, because whatsmeow always decrypts
	// its own other devices under a LID.
	second := find(t, received, "second")
	if !second.Info.IsFromMe {
		t.Error("the account's own message is not marked as from me")
	}
	if second.Info.Chat.ToNonAD() != asha {
		t.Errorf("own message landed in %s, want the chat with %s", second.Info.Chat, asha)
	}

	group := find(t, received, "in a group")
	if !group.Info.IsGroup {
		t.Error("group message is not marked as a group message")
	}
	if group.Info.Sender.ToNonAD() != ravi {
		t.Errorf("group message sender is %s, want %s", group.Info.Sender, ravi)
	}
	if group.Info.Chat.Server != types.GroupServer {
		t.Errorf("group message chat is %s, want a group", group.Info.Chat)
	}

	// The fourth one is scheduled rather than backlogged, so reaching it proves
	// the timeline runs after the offline sync rather than instead of it.
	live := find(t, received, "live")
	if live.Info.Chat != group.Info.Chat {
		t.Errorf("live message landed in %s, want %s", live.Info.Chat, group.Info.Chat)
	}

	info, err := cli.GetGroupInfo(ctx, group.Info.Chat)
	if err != nil {
		t.Fatalf("group info: %v", err)
	}
	if info.Name != "Test Group" {
		t.Errorf("group is named %q, want Test Group", info.Name)
	}
	if len(info.Participants) != 3 {
		t.Errorf("group has %d participants, want 3", len(info.Participants))
	}

	onWA, err := cli.IsOnWhatsApp(ctx, []string{"+" + asha.User, "+1555000000"})
	if err != nil {
		t.Fatalf("usync: %v", err)
	}
	if len(onWA) != 2 {
		t.Fatalf("usync answered for %d numbers, want 2", len(onWA))
	}
	if !onWA[0].IsIn {
		t.Error("a contact the scenario created is reported as not on WhatsApp")
	}
	if onWA[1].IsIn {
		t.Error("a number nobody has heard of is reported as on WhatsApp")
	}
}

// TestMessageIDsAreDeterministic is the property the golden frames will rest
// on. Message ids are the tiebreak in the daemon's message sort key, so two
// runs that numbered the same conversation differently would order
// same-second messages differently and diff against each other.
func TestMessageIDsAreDeterministic(t *testing.T) {
	ids := func(seed int64) []string {
		srv, err := New(Options{Seed: seed, Scenario: "test:conversation", Logger: discardLogger()})
		if err != nil {
			t.Fatalf("new server: %v", err)
		}
		var out []string
		for _, msg := range srv.world.backlog {
			out = append(out, msg.ID)
		}
		if len(out) == 0 {
			t.Fatal("scenario produced no messages")
		}
		return out
	}

	first, second := ids(42), ids(42)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("same seed produced %s then %s at index %d", first[i], second[i], i)
		}
	}
	if other := ids(43); other[0] == first[0] {
		t.Error("different seeds produced the same first message id")
	}
}
