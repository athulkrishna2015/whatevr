package ui

import (
	"whattui/internal/proto"
	"whattui/internal/view"
)

// What you can do to a chat. Every action takes the chat it acts on rather
// than reading it off the open conversation, because the context menu is
// opened on a row that may not be the open chat; the registry commands pass
// the open one.
//
// None of them change a row. The daemon owns every chat, so an action is a
// request and the answer arrives as the ordinary upsert of the list we are
// already subscribed to — the same shape as the message actions.

// chatRow reads one row out of the chat list. Asked outside App.mu, which is
// where a collection is always asked anything: the daemon writes through it
// from its own goroutine, and taking the two locks in the other order is how
// two goroutines stop forever.
func (a *App) chatRow(id string) (proto.ChatRow, bool) {
	var row proto.ChatRow
	found := false
	a.chats.Read(func(items []view.Item[proto.ChatRow], _ view.State) {
		for _, it := range items {
			if it.ID == id {
				row, found = it.Value, true
				return
			}
		}
	})
	return row, found
}

// blocked asks the blocklist view whether this jid is on it. Asked rather than
// held, so a block made on the phone is answered by the phone's list without a
// refresh here.
func (a *App) blocked(jid string) bool {
	blocked := false
	a.blocklist.Read(func(items []view.Item[proto.BlockedContact], _ view.State) {
		for _, it := range items {
			if it.ID == jid {
				blocked = true
				return
			}
		}
	})
	return blocked
}

// onOpenChat runs an action against the open conversation, which is what the
// palette and the slash commands mean by "this chat".
func (a *App) onOpenChat(what func(chatID string)) func() {
	return func() {
		a.mu.Lock()
		id := a.activeChat
		a.mu.Unlock()
		if id == "" {
			a.refuse("open a chat first")
			return
		}
		what(id)
	}
}

func (a *App) togglePin(chatID string) {
	row, ok := a.chatRow(chatID)
	if !ok {
		return
	}
	pinned := !row.Pinned
	a.do("chat.pin", proto.Params{"chat_id": chatID, "pinned": pinned}, func() {
		a.toast(onOff(pinned, "pinned", "unpinned"))
	})
}

func (a *App) toggleMute(chatID string) {
	row, ok := a.chatRow(chatID)
	if !ok {
		return
	}
	muted := !row.Muted
	// Zero is forever, the only duration that needs no question asked: a timed
	// mute is a second command and a picker this menu has no room for.
	a.do("chat.mute", proto.Params{"chat_id": chatID, "muted": muted, "duration_secs": 0}, func() {
		a.toast(onOff(muted, "muted", "unmuted"))
	})
}

func (a *App) toggleArchive(chatID string) {
	row, ok := a.chatRow(chatID)
	if !ok {
		return
	}
	archived := !row.Archived
	a.do("chat.archive", proto.Params{"chat_id": chatID, "archived": archived}, func() {
		a.toast(onOff(archived, "archived", "unarchived"))
	})
}

func (a *App) toggleFavorite(chatID string) {
	row, ok := a.chatRow(chatID)
	if !ok {
		return
	}
	favorite := !row.Favorite
	a.do("chat.favorite", proto.Params{"chat_id": chatID, "favorite": favorite}, func() {
		a.toast(onOff(favorite, "added to favourites", "removed from favourites"))
	})
}

// toggleBlock blocks or unblocks the contact a direct chat is with. A group is
// left with group.leave, which is a different command with a different
// consequence, so the menu says so rather than offering the wrong one.
func (a *App) toggleBlock(chatID string) {
	row, ok := a.chatRow(chatID)
	if !ok {
		return
	}
	if row.IsGroup {
		a.refuse("only a direct chat can be blocked")
		return
	}
	blocked := !a.blocked(chatID)
	a.do("contact.block", proto.Params{"jid": chatID, "blocked": blocked}, func() {
		a.toast(onOff(blocked, "blocked", "unblocked"))
	})
}

// markRead tells the daemon how far this device has read, which is the newest
// message in the window. The desktop frontend sends the watermark it tracked
// while its window was open; here the window is the whole conversation, so its
// live edge is the answer.
func (a *App) markRead(chatID string) {
	a.mu.Lock()
	c := a.conversation
	a.mu.Unlock()
	if c == nil || c.chatID != chatID {
		a.refuse("open that chat to mark it read")
		return
	}
	// Asked before the lock is taken: a collection has a lock of its own.
	var newest proto.MessageRow
	c.msgs.Read(func(items []view.Item[proto.MessageRow], _ view.State) {
		for _, it := range items {
			if newest.ID == "" || it.Value.Timestamp > newest.Timestamp {
				newest = it.Value
			}
		}
	})
	if newest.ID == "" {
		return
	}
	a.do("chat.mark_read", proto.Params{"chat_id": chatID, "up_to_message_id": newest.ID}, nil)
}

// markAllRead takes every unread message in every chat as read. It has no chat
// to act on, so it is not a per-chat action.
func (a *App) markAllRead() {
	a.do("chat.mark_all_read", proto.Params{}, nil)
}
