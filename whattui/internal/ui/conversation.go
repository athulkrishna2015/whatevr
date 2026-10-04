package ui

import (
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// conversation is one open transcript: its subscription, its window state and
// where the reader is in it.
//
// All of it is presentation state. The messages themselves live in the
// collection, which the daemon keeps correct.
//
// Everything on it is touched only from the event loop, which is why the
// drawing code reads it without a lock. The exceptions say so: they are
// written from the client's goroutine, and App.mu covers them.
type conversation struct {
	chatID string
	// window is the messages subscription on screen, and next the one being
	// filled to take over from it. The screen never shows a window half
	// filled: next is drawn from the first frame after its ready.
	*window
	next *window

	// chat is the chat's own row, outside any list window: the header,
	// whether it is a group, whether we may send, whether its history is
	// coming. nextChat is the same for the id it folded into.
	chat        *view.Object[*v2.ChatRow]
	chatSub     *proto.Subscription
	nextChat    *view.Object[*v2.ChatRow]
	nextChatSub *proto.Subscription
	// replacedBy is the id the chat folded into, from the chat list. Under
	// App.mu.
	replacedBy string
	// readOnly is the chat row's read_only as of the last frame, kept here
	// so the commands can ask it under App.mu.
	readOnly bool
	// asked is a chat_request_older the daemon has not answered yet. Under
	// App.mu.
	asked bool
	// hold is a message to keep on the same screen row across a window swap.
	hold *hold

	// scroll counts rows up from the live edge. Zero is pinned to the bottom,
	// which is where a chat opens and where it stays while messages arrive.
	//
	// Rows, not messages. Scrolling a message at a time means a long one
	// leaves the screen in a single notch, which reads as the transcript
	// jumping rather than moving.
	scroll int
	// contentRows is how tall everything in the window is, gaps included, so
	// the scroll knows where its end is.
	contentRows int

	// The laid-out form of every message in the window, which is the
	// expensive half of drawing one. Thrown away when the pane changes size,
	// and refreshed per message when the daemon changes one.
	cache      map[string]entry
	cacheWidth int
	cacheRows  int
	cacheVer   uint64
	// cacheGroup is whether the chat is a group, because whether a message
	// says who sent it depends on that and on nothing in the message.
	cacheGroup bool
	// cacheBoxed is which shape the transcript is drawn with, because that
	// changes how wide a message is allowed to be.
	cacheBoxed bool
	// cacheTop is what sits above the oldest message
	cacheTop topMark
	// runs is the window gathered into runs, newest first, which is the order
	// the transcript draws them in.
	runs []run
	// selected is the message the actions act on, by id, or empty for none,
	// and selectedRow is that message as of the last time anything about it
	// changed. The copy is what lets an action ask whether it applies without
	// asking the collection: see setCursor.
	selected    string
	selectedRow *v2.MessageRow
}

// window is one messages subscription and how far it reaches.
type window struct {
	sub  *proto.Subscription
	msgs *view.Collection[*v2.MessageRow]
	// anchor is the message it was opened around, for a swap that cannot
	// find the one the reader was looking at
	anchor string

	// The rest is under App.mu: the hooks write it.
	//
	// size is what the window has asked for, so no extend asks past the cap.
	size int
	// live is a window whose newer end is the present.
	live bool
	// olderDone and newerDone are the ends the local store has nothing past.
	olderDone, newerDone bool
	// oldest is the oldest message when olderDone was last said: one older
	// than it is the phone's answer landing in the window
	oldest string
	// extending is the way an extend is out, and extendBy how many it asked
	// for. UNSPECIFIED for none.
	extending v2.Direction
	extendBy  int
	// failed is a subscribe the daemon refused, lost one the connection took
	// with it.
	failed, lost bool
}

// hold is a message and the screen row its top was on.
type hold struct {
	id, anchor string
	top        int
}

// topMark is what the transcript shows above the oldest message in the window.
type topMark int

const (
	topNone topMark = iota
	// topLoading is the phone sending older history
	topLoading
	// topStart is the start of the chat: nothing older anywhere
	topStart
)

const (
	messagePageSize = 60
	// messagesCap is the most one messages window holds, the daemon's cap.
	messagesCap = 256
)

func (a *App) openChat(chatID string) {
	a.clearSelection()
	a.mu.Lock()
	if a.conversation != nil && a.conversation.chatID == chatID {
		a.activeChat = chatID
		a.focus = FocusComposer
		a.mu.Unlock()
		return
	}
	old := a.conversation
	a.mu.Unlock()

	if old != nil {
		old.close()
	}

	c := &conversation{chatID: chatID}
	c.chat, c.chatSub = a.watchChat(chatID)
	c.window = a.openWindow(chatID, nil, "")

	a.mu.Lock()
	a.conversation = c
	a.activeChat = chatID
	a.focus = FocusComposer
	a.mu.Unlock()

	a.updateSession()
}

// chatRow is a chat's row: the open chat's own subscription's, or the chat
// list's until that one has answered.
func (a *App) chatRow(id string) (*v2.ChatRow, bool) {
	if c := a.conv(); c != nil && c.chat != nil && c.chatID == id {
		if row, ok := c.chat.Value(); ok {
			return row, true
		}
	}
	if it, ok := a.chats.Get(id); ok {
		return it.Value, true
	}
	return nil, false
}

// close lets go of every subscription the conversation holds.
func (c *conversation) close() {
	for _, w := range []*window{c.window, c.next} {
		if w != nil && w.sub != nil {
			w.sub.Close()
		}
	}
	for _, s := range []*proto.Subscription{c.chatSub, c.nextChatSub} {
		if s != nil {
			s.Close()
		}
	}
}

// watchChat subscribes to one chat's own row.
func (a *App) watchChat(chatID string) (*view.Object[*v2.ChatRow], *proto.Subscription) {
	o := view.NewObject(view.Chat)
	sub := a.client.Subscribe(v2.Subscribe_builder{
		Chat: v2.ChatView_builder{ChatId: chatID}.Build(),
	}.Build(), o, proto.Hooks{})
	return o, sub
}

// openWindow subscribes to a chat's messages: at the live edge for a nil
// sort, around the message with that sort otherwise.
func (a *App) openWindow(chatID string, sort []byte, anchor string) *window {
	w := &window{msgs: view.NewCollection(view.Message), size: messagePageSize, anchor: anchor}
	// The live edge is row 0, so the transcript reads bottom to top and a new
	// message lands where the reader already is.
	w.msgs.SetReverse(true)
	params := v2.MessagesView_builder{ChatId: chatID}
	if sort == nil {
		params.Latest = &v2.Latest{}
		w.live, w.newerDone = true, true
	} else {
		params.Sort = sort
	}
	w.sub = a.client.Subscribe(v2.Subscribe_builder{
		Limit:    messagePageSize,
		Messages: params.Build(),
	}.Build(), w.msgs, proto.Hooks{
		OnReady: func(exhausted bool) { a.windowReady(w, exhausted) },
		OnFailed: func(err *proto.Error) {
			a.mu.Lock()
			w.failed = true
			a.mu.Unlock()
			a.refuse(err.Error())
		},
		OnExtendFailed: func(_ v2.Direction, err *proto.Error) {
			a.mu.Lock()
			w.extending = v2.Direction_DIRECTION_UNSPECIFIED
			if err.HasCode(v2.ErrorCode_ERROR_CODE_INVALID_PARAMS) {
				// the daemon counts the window its own way once it has
				// gone live; its no is the cap, and the next reach moves
				w.size = messagesCap
			} else {
				w.size -= w.extendBy
			}
			a.mu.Unlock()
			a.vx.PostEvent(redraw{})
		},
		// a window comes back from where the reader is, not from where it
		// was opened: see followChat
		OnLost: func() {
			a.mu.Lock()
			w.lost = true
			a.mu.Unlock()
			a.vx.PostEvent(redraw{})
		},
	})
	return w
}

// windowReady notes what a ready says about the end just filled. The first
// fill of an anchored window speaks for both ends, and only when both are done.
func (a *App) windowReady(w *window, exhausted bool) {
	// read before App.mu, never under it: see reach
	oldest := oldestIn(w.msgs)
	a.mu.Lock()
	defer a.mu.Unlock()
	switch w.extending {
	case v2.Direction_DIRECTION_OLDER:
		w.olderDone = exhausted
	case v2.Direction_DIRECTION_NEWER:
		w.newerDone = exhausted
		// the daemon keeps a window that reached the present there
		w.live = w.live || exhausted
	default:
		if exhausted {
			w.olderDone, w.newerDone, w.live = true, true, true
		}
	}
	w.oldest = oldest
	w.extending = v2.Direction_DIRECTION_UNSPECIFIED
}

// oldestIn is the id of the oldest message in a window.
func oldestIn(msgs *view.Collection[*v2.MessageRow]) string {
	id := ""
	msgs.Read(func(items []view.Item[*v2.MessageRow], _ view.State) {
		if len(items) > 0 {
			id = items[len(items)-1].ID
		}
	})
	return id
}

// loadOlder reaches back up the transcript, loadNewer down toward the
// present. Guarded so a scroll that outruns the daemon does not queue a
// hundred extends.
func (a *App) loadOlder() { a.reach(v2.Direction_DIRECTION_OLDER) }
func (a *App) loadNewer() { a.reach(v2.Direction_DIRECTION_NEWER) }

// reach grows the window toward d, moves to a window around what is on
// screen when this one is full, and asks the phone for older history when
// the local store has none.
func (a *App) reach(d v2.Direction) {
	c := a.conv()
	if c == nil || c.next != nil {
		return
	}
	w := c.window
	// Asked before App.mu is taken, never under it: a collection has a lock
	// of its own and the daemon's goroutine writes through it.
	if !w.msgs.IsReady() {
		return
	}
	have := w.msgs.Len()
	phone := d == v2.Direction_DIRECTION_OLDER && a.phoneHasMore(c)

	a.mu.Lock()
	if a.conversation != c || w.extending != v2.Direction_DIRECTION_UNSPECIFIED || w.lost || w.failed {
		a.mu.Unlock()
		return
	}
	done := w.olderDone
	if d == v2.Direction_DIRECTION_NEWER {
		done = w.newerDone || w.live
	}
	// what the phone sends lands in the window only within its reach, so a
	// window asking for it keeps a page of room
	ask := done && phone
	grow := !done || ask && w.size-have < messagePageSize
	n := min(messagePageSize, messagesCap-w.size)
	if grow && n > 0 {
		w.extending, w.extendBy = d, n
		w.size += n
	}
	a.mu.Unlock()

	switch {
	case !grow:
	case n <= 0:
		a.shift(c)
	case w.sub != nil:
		w.sub.Extend(n, d)
	}
	if ask {
		a.askPhone(c)
	}
}

// phoneHasMore is a chat whose phone may have history older than the store.
func (a *App) phoneHasMore(c *conversation) bool {
	if c.chat == nil {
		return false
	}
	row, ok := c.chat.Value()
	return ok && !row.GetHistoryExhausted()
}

// askPhone asks for history older than anything the daemon has. The rows land
// in the store like any other; loading_older on the chat row is what says
// it is on its way.
func (a *App) askPhone(c *conversation) {
	if row, _ := c.chat.Value(); row.GetLoadingOlder() {
		return
	}
	a.mu.Lock()
	if c.asked || a.request == nil {
		a.mu.Unlock()
		return
	}
	c.asked = true
	request := a.request
	a.mu.Unlock()

	req := &v2.Request{}
	req.SetChatRequestOlder(v2.ChatRequestOlder_builder{ChatId: c.chatID}.Build())
	request(req, func(_ *v2.Response, err *proto.Error) {
		// from here loading_older on the row says whether one is out
		a.mu.Lock()
		c.asked = false
		a.mu.Unlock()
		if err != nil {
			a.refuse(err.Error())
		}
	})
}

// shift opens a window around the message in the middle of the screen. The
// one on screen stays until the new one is filled.
func (a *App) shift(c *conversation) {
	id, _ := c.middle(a.transcriptPage())
	it, ok := c.msgs.Get(id)
	if !ok {
		return
	}
	c.next = a.openWindow(c.chatID, []byte(it.Sort), id)
}

// reopen replaces the window with one at the same place: the live edge for a
// reader at the bottom of a live window, around the middle of the screen for
// anyone else.
func (a *App) reopen(c *conversation) {
	if c.next != nil && c.next.sub != nil {
		c.next.sub.Close()
	}
	c.next = nil
	a.mu.Lock()
	live := c.window.live
	a.mu.Unlock()
	if live && c.scroll == 0 {
		c.next = a.openWindow(c.chatID, nil, "")
		return
	}
	id, _ := c.middle(a.transcriptPage())
	it, ok := c.msgs.Get(id)
	if !ok {
		c.next = a.openWindow(c.chatID, nil, "")
		return
	}
	c.next = a.openWindow(c.chatID, []byte(it.Sort), id)
}

// followChat brings the open conversation up to date with what the daemon
// said since the last frame: a chat folded into another, a connection back
// after a drop, a window filled and ready to take over, the phone done
// sending history. Run at the start of every frame, on the event loop.
func (a *App) followChat(viewport int) {
	c := a.conv()
	if c == nil {
		return
	}
	var row *v2.ChatRow
	if c.chat != nil {
		row, _ = c.chat.Value()
	}

	a.mu.Lock()
	by := c.replacedBy
	c.replacedBy = ""
	back := a.transport == proto.Ready
	lost := c.window.lost
	var nextFailed bool
	if c.next != nil {
		nextFailed = c.next.failed || c.next.lost
	}
	a.mu.Unlock()

	// The chat row says so for a chat outside the list window: the daemon
	// still answers the old id, with the row of the chat it is now.
	if id := row.GetId(); id != "" && id != c.chatID {
		by = id
	}
	if by != "" && by != c.chatID {
		a.moveChat(c, by)
	} else if lost && back && c.next == nil {
		a.reopen(c)
	}

	if nextFailed {
		c.next.sub.Close()
		c.next = nil
	}
	if c.next != nil && c.next.msgs.IsReady() {
		a.swap(c, viewport)
	}
	if c.nextChat != nil && c.nextChat.IsReady() {
		c.chatSub.Close()
		c.chat, c.chatSub = c.nextChat, c.nextChatSub
		c.nextChat, c.nextChatSub = nil, nil
		row, _ = c.chat.Value()
	}

	// after any swap, so it is the window on screen; and before App.mu,
	// never under it: see reach
	oldest := oldestIn(c.msgs)
	a.mu.Lock()
	c.readOnly = row.GetReadOnly()
	// what the phone sent landed past where the store ran out, so there may
	// be more of it
	grew := c.window.olderDone && oldest != "" && oldest != c.window.oldest
	if grew {
		c.window.olderDone = false
	}
	a.mu.Unlock()
	if grew {
		a.clampScroll(c, viewport)
	}
}

// moveChat follows a chat that folded into another: the transcript, the
// header and the session all go to the new id, from where the reader is.
func (a *App) moveChat(c *conversation, by string) {
	if c.nextChatSub != nil {
		c.nextChatSub.Close()
	}
	c.nextChat, c.nextChatSub = a.watchChat(by)

	a.mu.Lock()
	c.chatID = by
	if a.conversation == c {
		a.activeChat = by
	}
	a.mu.Unlock()
	a.reopen(c)
	a.updateSession()
}

// swap puts the filled window on screen in one frame and lets go of the old
// one. The message in the middle of the screen stays on its row.
func (a *App) swap(c *conversation, viewport int) {
	old := c.window
	id, row := c.middle(viewport)

	a.mu.Lock()
	next := c.next
	live := next.live && next.anchor == ""
	a.mu.Unlock()

	c.window, c.next = next, nil
	c.cache, c.runs = nil, nil
	c.hold = nil
	switch {
	case live && c.scroll == 0:
		// a reader at the bottom of a live window stays at the bottom
	case id != "":
		c.hold = &hold{id: id, anchor: next.anchor, top: row}
	case next.anchor != "":
		c.hold = &hold{id: next.anchor, top: viewport / 2}
	}
	if old.sub != nil {
		old.sub.Close()
	}
}

// keepHeld puts the held message back on the row it was on, now the new
// window is laid out.
func (c *conversation) keepHeld(viewport int) {
	h := c.hold
	if h == nil {
		return
	}
	c.hold = nil
	if above, _, ok := c.place(h.id); ok {
		c.scroll = h.top - viewport + above
	} else if above, _, ok := c.place(h.anchor); ok {
		c.scroll = viewport/2 - viewport + above
	}
	c.scroll = max(min(c.scroll, c.maxScroll(viewport)), 0)
}

// spin redraws the frame once a spinner frame has gone by, and only once
// however many frames asked.
func (a *App) spin() {
	if a.spinning.Swap(true) {
		return
	}
	time.AfterFunc(spinnerTick, func() {
		a.spinning.Store(false)
		a.vx.PostEvent(redraw{})
	})
}

const spinnerTick = 80 * time.Millisecond
