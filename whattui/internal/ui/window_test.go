package ui

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// idle is a conversation with nothing out: no extend owed, no window being
// filled that has not been filled yet.
func idle(a *App, c *conversation) bool {
	a.mu.Lock()
	extending := c.window.extending
	a.mu.Unlock()
	return extending == v2.Direction_DIRECTION_UNSPECIFIED && (c.next == nil || c.next.msgs.IsReady())
}

// rowOf is the screen row a message's top is on.
func rowOf(c *conversation, id string, viewport int) (int, bool) {
	above, _, ok := c.place(id)
	return viewport + c.scroll - above, ok
}

// scrollBy scrolls by rows at a time until done says stop, painting and
// checking after every step that the message in the middle of the screen did
// not move and that the transcript never went blank to fetch.
func scrollBy(t *testing.T, a *App, by int, done func(moves int) bool) {
	t.Helper()
	c := a.conv()
	page := a.transcriptPage()
	moves := 0
	for i := 0; !done(moves); i++ {
		if i > 2000 {
			t.Fatal("never got there")
		}
		a.scrollTranscript(by)
		waitFor(t, "the window", func() bool { return idle(a, c) })
		old := c.window
		id, row := c.middle(page)
		held := c.scroll > 0 && c.scroll < c.maxScroll(page)
		a.paint()
		if c.window != old {
			moves++
			if old.sub.Active() {
				t.Fatal("the window that went off screen is still subscribed")
			}
		}
		if got := a.transcriptRowsText(); strings.Contains(got, "loading messages") {
			t.Fatalf("the transcript went blank to fetch:\n%s", got)
		}
		if !held || c.scroll == 0 {
			continue
		}
		if now, ok := rowOf(c, id, page); !ok || now != row {
			t.Fatalf("%s was on row %d and is on row %d (there: %v)", id, row, now, ok)
		}
	}
}

func TestScrollingPastTheCapMovesTheWindowAndKeepsThePlace(t *testing.T) {
	a := mockApp(t, floodScenario, floodDeepChat, 80, 24)
	a.paint()
	c := a.conv()
	page := a.transcriptPage()

	scrollBy(t, a, page/2, func(moves int) bool { return moves >= 2 })
	a.mu.Lock()
	live := c.window.live
	a.mu.Unlock()
	if live {
		t.Fatal("two windows up the chat is still the live one")
	}

	// back down, extending toward the present until a window reaches it
	scrollBy(t, a, -page/2, func(int) bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return c.window.live && c.scroll == 0
	})
	settle(t, a)
	a.paint()
	newest := ""
	c.msgs.Read(func(items []view.Item[*v2.MessageRow], _ view.State) { newest = items[0].ID })
	if row, ok := rowOf(c, newest, page); !ok || row >= page {
		t.Fatalf("the newest message is not on screen at the bottom (row %d, there: %v)", row, ok)
	}
}

func TestAReconnectComesBackWhereTheReaderWas(t *testing.T) {
	a := mockApp(t, floodScenario, floodDeepChat, 80, 24)
	a.paint()
	c := a.conv()
	page := a.transcriptPage()
	scrollBy(t, a, page/2, func(moves int) bool { return moves >= 1 })
	id, row := c.middle(page)

	a.client.Stop()
	a.client.Start()
	waitFor(t, "the window to be lost", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return c.window.lost
	})
	waitFor(t, "the socket", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.transport == proto.Ready
	})
	old := c.window
	a.paint()
	if c.next == nil {
		t.Fatal("nothing reopened the window")
	}
	if got := c.next.sub.Params().GetMessages(); !got.HasSort() {
		t.Fatalf("the window came back at %v, not where the reader was", got)
	}
	waitFor(t, "the new window", func() bool { return c.next.msgs.IsReady() })
	a.paint()
	if c.window == old {
		t.Fatal("the new window never took over")
	}
	if now, ok := rowOf(c, id, page); !ok || now != row {
		t.Fatalf("%s was on row %d and is on row %d (there: %v)", id, row, now, ok)
	}
}

// chatObject is the open chat's own row, the way the daemon's chat view sends
// it.
func chatObject(row *v2.ChatRow) *view.Object[*v2.ChatRow] {
	o := view.NewObject(view.Chat)
	upsert(o, v2.Upsert_builder{Id: row.GetId(), Chat: row})
	ready(o, false)
	return o
}

func TestAFoldedChatMovesTheConversation(t *testing.T) {
	for _, how := range []string{"chat row", "chat list"} {
		t.Run(how, func(t *testing.T) {
			a := stubApp(80, 24, 4, 10)
			sent := records(a)
			c := a.conv()
			const lid = "1234@lid"
			if how == "chat row" {
				c.chat = chatObject(v2.ChatRow_builder{Id: lid, Name: "contact 0"}.Build())
			} else {
				a.mu.Lock()
				c.replacedBy = lid
				a.mu.Unlock()
			}
			a.paint()

			a.mu.Lock()
			active := a.activeChat
			a.mu.Unlock()
			if c.chatID != lid || active != lid {
				t.Fatalf("the conversation is on %q and the active chat %q, not %q", c.chatID, active, lid)
			}
			if c.nextChatSub == nil || c.nextChatSub.Params().GetChat().GetChatId() != lid {
				t.Fatal("the header is not watching the chat it folded into")
			}
			if c.next == nil || c.next.sub.Params().GetMessages().GetChatId() != lid {
				t.Fatal("the transcript did not reopen on the chat it folded into")
			}
			told := false
			for _, r := range *sent {
				if r.req.HasSessionUpdate() && r.req.GetSessionUpdate().GetActiveChatId() == lid {
					told = true
				}
			}
			if !told {
				t.Fatal("the daemon was not told which chat is open now")
			}
		})
	}
}

func TestAReadOnlyChatHasNoComposer(t *testing.T) {
	a := stubApp(80, 24, 4, 10)
	a.initCommands()
	c := a.conv()
	c.chat = chatObject(v2.ChatRow_builder{Id: c.chatID, Name: "contact 0", ReadOnly: true}.Build())
	a.paint()

	if got := a.composerRowText(0); !strings.Contains(got, readOnlyNote) {
		t.Fatalf("the composer row says %q", got)
	}
	a.onKey(key('h'))
	if !a.composer.empty() {
		t.Fatalf("typing went into the composer: %q", a.composer.String())
	}
	a.mu.Lock()
	toast, refused := a.toastText, a.toastRefused
	a.mu.Unlock()
	if toast != readOnlyNote || !refused {
		t.Fatalf("typing said %q (refused: %v)", toast, refused)
	}
	// m0 is ours and still editable, so read-only is the only thing in the way
	a.setCursor("m0")
	a.composer.insert("draft")
	for _, id := range []commandID{cmdSend, cmdReply, cmdEditMessage} {
		if ok, why := a.commands.byID[id].Enabled(a.commandState()); ok || why != readOnlyNote {
			t.Errorf("%s in a read-only chat: %v, %q", id, ok, why)
		}
	}
	if ok, _ := a.commands.byID[cmdReact].Enabled(a.commandState()); !ok {
		t.Error("a reaction is refused in a read-only chat")
	}
}

func TestTheTopOfTheWindowSaysWhatIsAboveIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  v2.ChatRow_builder
		want string
	}{
		{"phone sending", v2.ChatRow_builder{LoadingOlder: true}, "loading older messages"},
		{"nothing older", v2.ChatRow_builder{HistoryExhausted: true}, "start of chat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := stubApp(80, 40, 4, 3)
			c := a.conv()
			tc.row.Id, tc.row.Name = c.chatID, "contact 0"
			c.chat = chatObject(tc.row.Build())
			a.mu.Lock()
			c.window.olderDone, c.window.oldest = true, "m0"
			a.mu.Unlock()
			a.paint()
			if got := a.transcriptRowsText(); !strings.Contains(got, tc.want) {
				t.Fatalf("no %q above the oldest message:\n%s", tc.want, got)
			}
		})
	}
}

func TestTheTopOfTheStoreAsksThePhone(t *testing.T) {
	a := stubApp(80, 24, 4, 3)
	sent := records(a)
	c := a.conv()
	c.chat = chatObject(v2.ChatRow_builder{Id: c.chatID, Name: "contact 0"}.Build())
	a.mu.Lock()
	// room in the window for the answer, so asking is all a reach does
	c.window.olderDone, c.window.oldest, c.window.size = true, "m0", 2*messagePageSize
	a.mu.Unlock()
	a.paint()
	asked := func() int {
		n := 0
		for _, r := range *sent {
			if r.req.HasChatRequestOlder() {
				n++
				if got := r.req.GetChatRequestOlder().GetChatId(); got != c.chatID {
					t.Fatalf("asked the phone about %q", got)
				}
			}
		}
		return n
	}

	a.loadOlder()
	if n := asked(); n != 1 {
		t.Fatalf("asked the phone %d times", n)
	}
	// one out is one too many to ask again
	c.chat = chatObject(v2.ChatRow_builder{Id: c.chatID, Name: "contact 0", LoadingOlder: true}.Build())
	a.paint()
	a.loadOlder()
	if n := asked(); n != 1 {
		t.Fatalf("asked the phone %d times with a request out", n)
	}

	// the answer lands above the oldest message, whether or not a frame saw
	// loading_older at all
	putMsg(c.msgs, " ", v2.MessageRow_builder{Id: "older", TextBody: &v2.Text{}, Text: "from the phone",
		TMs: 1757000000000, Sender: person(c.chatID, "contact 0")}.Build())
	c.chat = chatObject(v2.ChatRow_builder{Id: c.chatID, Name: "contact 0"}.Build())
	a.paint()
	a.mu.Lock()
	done := c.window.olderDone
	a.mu.Unlock()
	if done {
		t.Fatal("the older end is still done with the phone's answer in the window")
	}
}

// asks counts the requests for older history that reach the daemon.
func asks(a *App) *atomic.Int32 {
	var n atomic.Int32
	a.mu.Lock()
	request := a.request
	a.request = func(req *v2.Request, cb proto.ResponseFunc) {
		if req.HasChatRequestOlder() {
			n.Add(1)
		}
		request(req, cb)
	}
	a.mu.Unlock()
	return &n
}

func TestTheTopOfTheStoreFetchesFromThePhone(t *testing.T) {
	a := mockApp(t, historyScenario, historyLongChat, 80, 24)
	// the daemon is shared, so the phone may have been asked by a test before
	asked := asks(a)
	a.paint()
	c := a.conv()
	page := a.transcriptPage()

	oldest := func() string {
		id := ""
		c.msgs.Read(func(items []view.Item[*v2.MessageRow], _ view.State) {
			if len(items) > 0 {
				id = items[len(items)-1].Value.GetText()
			}
		})
		return id
	}
	deadline := time.Now().Add(60 * time.Second)
	for oldest() != "line 1" {
		if time.Now().After(deadline) {
			a.mu.Lock()
			w := *c.window
			a.mu.Unlock()
			t.Fatalf("never got past %q, asked the phone %d times; window %d long, size %d, older done %v, extending %v, failed %v, lost %v, next %v, scroll %d of %d",
				oldest(), asked.Load(), c.msgs.Len(), w.size, w.olderDone, w.extending, w.failed, w.lost, c.next != nil, c.scroll, c.maxScroll(page))
		}
		a.scrollTranscript(page / 2)
		waitFor(t, "the window", func() bool { return idle(a, c) })
		a.paint()
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAChatAtItsStartSaysSo(t *testing.T) {
	a := mockApp(t, historyScenario, "Kabir", 80, 24)
	a.paint()
	a.scrollTranscript(1000)
	waitFor(t, "the window", func() bool { return idle(a, a.conv()) })
	a.paint()
	got := a.transcriptRowsText()
	start := strings.Index(got, "start of chat")
	first := strings.Index(got, "hey, this is my new number")
	if start < 0 || first < start {
		t.Fatalf("no start of chat above the first message:\n%s", got)
	}
	// the first day is named under it, like every day after
	days := 0
	for _, line := range strings.Split(got[start:first], "\n")[1:] {
		if strings.Contains(line, "\u2500") {
			days++
		}
	}
	if days != 1 {
		t.Fatalf("%d day lines between the start and the first message:\n%s", days, got)
	}
}

func TestAnAnnouncementGroupHasNoComposer(t *testing.T) {
	a := mockApp(t, historyScenario, "Announcements", 80, 24)
	waitFor(t, "the chat row", func() bool {
		a.paint()
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.conversation.readOnly
	})
	if got := a.composerRowText(0); !strings.Contains(got, readOnlyNote) {
		t.Fatalf("the composer row says %q", got)
	}
}
