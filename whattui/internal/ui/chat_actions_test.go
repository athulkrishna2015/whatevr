package ui

import (
	"testing"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// The whole point of a chat action is that it reaches the daemon as the right
// command carrying the opposite of the row's current state, so that is what
// these check: one call, one method, the right flag, both ways round.

// toggleCase is one action and the two commands it must be able to send: the
// one for a row in the state `set` puts it in, and the one for a row in the
// other state.
type toggleCase struct {
	id     commandID
	method string
	// set puts the row into one end of the toggle, and blocked adds to the
	// blocklist for the action whose state lives there rather than on the row.
	set     func(*App, *proto.ChatRow)
	blocked bool
	// withSet is what the command must carry for a row `set` has put in that
	// state, and withoutSet for a row in the other one.
	withSet    func(proto.Params) bool
	withoutSet func(proto.Params) bool
}

var chatToggles = []toggleCase{
	{
		id: cmdPin, method: "chat.pin",
		set:        func(_ *App, r *proto.ChatRow) { r.Pinned = true },
		withSet:    func(p proto.Params) bool { return p["pinned"] == false },
		withoutSet: func(p proto.Params) bool { return p["pinned"] == true },
	},
	{
		id: cmdUnmute, method: "chat.mute",
		set:        func(_ *App, r *proto.ChatRow) { r.Muted = true },
		withSet:    func(p proto.Params) bool { return p["muted"] == false && p["duration_secs"] == 0 },
		withoutSet: func(p proto.Params) bool { return p["muted"] == true },
	},
	{
		id: cmdArchive, method: "chat.archive",
		set:        func(_ *App, r *proto.ChatRow) { r.Archived = true },
		withSet:    func(p proto.Params) bool { return p["archived"] == false },
		withoutSet: func(p proto.Params) bool { return p["archived"] == true },
	},
	{
		id: cmdFavorite, method: "chat.favorite",
		set:        func(_ *App, r *proto.ChatRow) { r.Favorite = true },
		withSet:    func(p proto.Params) bool { return p["favorite"] == false },
		withoutSet: func(p proto.Params) bool { return p["favorite"] == true },
	},
	{
		id: cmdBlock, method: "contact.block",
		set:        func(_ *App, _ *proto.ChatRow) {},
		blocked:    true,
		withSet:    func(p proto.Params) bool { return p["blocked"] == false },
		withoutSet: func(p proto.Params) bool { return p["blocked"] == true },
	},
}

func TestEveryChatToggleSendsTheOppositeOfTheRow(t *testing.T) {
	for _, tc := range chatToggles {
		t.Run(string(tc.id), func(t *testing.T) {
			// From the untouched fixture, where nothing is set.
			a := stubApp(100, 26, 4, 6)
			calls := records(a)
			a.runChatAction(tc.id, a.activeChat)
			if len(*calls) != 1 {
				t.Fatalf("%s made %d requests, want one", tc.id, len(*calls))
			}
			if got := (*calls)[0].method; got != tc.method {
				t.Fatalf("%s used %q, want %q", tc.id, got, tc.method)
			}
			if !tc.withoutSet((*calls)[0].params) {
				t.Fatalf("%s sent %v for a row with nothing set", tc.id, (*calls)[0].params)
			}

			// And from the other end, which is the half that is easy to get
			// wrong: the same action on a row already in that state.
			a = stubApp(100, 26, 4, 6)
			row, _ := a.chatRow(a.activeChat)
			tc.set(a, &row)
			if tc.blocked {
				a.blocklist.Upsert("0000", mustJSON(proto.BlockedContact{ID: a.activeChat}))
				a.blocklist.Ready(false, false)
			}
			a.chats.Upsert(firstChatSortKey, mustJSON(row))
			calls = records(a)
			a.runChatAction(tc.id, a.activeChat)
			if len(*calls) != 1 || !tc.withSet((*calls)[0].params) {
				t.Fatalf("%s sent %v for a row already set", tc.id, *calls)
			}
		})
	}
}

// The first chat of the fixture list, under the sort key stubApp gave it.
// Re-upserting a row under a different key would add a second row rather than
// change the first.
const firstChatSortKey = "00000000000000000000"

// A context menu acts on the chat it was opened over. Opening it on one chat
// while another is the open one is the case that catches a menu wired to the
// open conversation instead of to its own row.
func TestTheChatMenuActsOnTheChatItWasOpenedOn(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	open := a.activeChat

	var second string
	a.chats.Read(func(items []view.Item[proto.ChatRow], _ view.State) {
		if len(items) < 2 {
			t.Skip("the fixture has one chat")
		}
		second = items[1].ID
	})
	if second == open {
		t.Fatalf("the fixture's second row is the open chat %q", open)
	}

	a.openChatMenu(1, point{2, 1})
	if a.modal.kind != modalMenu || a.modal.chat != second {
		t.Fatalf("the menu is %d over %q, want a menu over %q", a.modal.kind, a.modal.chat, second)
	}
	// The pin row, rather than the first: marking a chat read needs its
	// transcript, which only the open chat has.
	choice, ok := choiceFor(a.modal.selector.items, cmdPin)
	if !ok {
		t.Fatal("the chat menu offers no pin")
	}
	a.chooseLocked(choice)()
	if len(*calls) != 1 {
		t.Fatalf("the menu made %d requests, want one", len(*calls))
	}
	if got := (*calls)[0].params["chat_id"]; got != second {
		t.Fatalf("the menu acted on %v, want the row it was opened on %q", got, second)
	}
}

// A menu that offers "Pin" over a pinned chat is a menu that lies, so the
// label has to be read off the row rather than off the command's title.
func TestTheChatMenuNamesTheActionNotTheState(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	id := a.activeChat
	if row, _ := a.chatRow(id); row.Pinned {
		t.Fatal("the fixture chat is already pinned")
	}
	if got := labelOf(a.chatChoicesLocked(id), cmdPin); got != "Pin chat" {
		t.Fatalf("an unpinned chat offers %q", got)
	}
	row, _ := a.chatRow(id)
	row.Pinned = true
	a.chats.Upsert(firstChatSortKey, mustJSON(row))
	if got := labelOf(a.chatChoicesLocked(id), cmdPin); got != "Unpin chat" {
		t.Fatalf("a pinned chat offers %q", got)
	}
}

// choiceFor is the row a menu gives one command.
func choiceFor(choices []modalChoice, id commandID) (modalChoice, bool) {
	for _, c := range choices {
		if c.Command == id && c.Disabled == "" {
			return c, true
		}
	}
	return modalChoice{}, false
}

// labelOf is the label a menu gives one command.
func labelOf(choices []modalChoice, id commandID) string {
	for _, c := range choices {
		if c.Command == id {
			return c.Label
		}
	}
	return ""
}

// Marking a chat read is the one action with no flag: it is a watermark, and
// the watermark is the newest message in the window.
func TestMarkingReadSendsTheNewestMessageInTheWindow(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)

	a.runChatAction(cmdMarkRead, a.activeChat)
	if len(*calls) != 1 || (*calls)[0].method != "chat.mark_read" {
		t.Fatalf("mark read made %+v", *calls)
	}
	params := (*calls)[0].params
	if params["chat_id"] != a.activeChat {
		t.Fatalf("mark read names %v, want %q", params["chat_id"], a.activeChat)
	}
	if got := params["up_to_message_id"]; got != "m5" {
		t.Fatalf("mark read stops at %v, want the newest message in the window", got)
	}
}

// Marking everything read has no chat to act on, so it must not be one of the
// menu's rows: a row with no chat behind it is a row that does the wrong thing
// wherever it is pressed from.
func TestMarkAllReadIsNotInTheChatMenu(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	for _, c := range a.chatChoicesLocked(a.activeChat) {
		if c.Command == cmdMarkAllRead {
			t.Fatal("the chat menu offers a command that acts on every chat")
		}
	}
}

// A group is left with leaving it, which is a different command with a
// different consequence, so the menu refuses rather than offering the wrong
// one.
func TestBlockingAGroupIsRefusedNotSent(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	row, _ := a.chatRow(a.activeChat)
	row.IsGroup = true
	a.chats.Upsert(firstChatSortKey, mustJSON(row))

	a.runChatAction(cmdBlock, a.activeChat)
	if len(*calls) != 0 {
		t.Fatalf("blocking a group sent %+v", *calls)
	}
}