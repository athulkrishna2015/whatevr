package ui

import "whattui/internal/proto"

// What the daemon knows about this window: whether it is in front, and which
// chat is open in it.
//
// It is one answer per connection, replaced whole by every session.update, and
// the daemon spends it on two things a reader would otherwise blame on it. The
// notifier stays quiet about the chat you are looking at, and a notification
// click is handed to the frontend that is actually in front rather than to a
// window buried three workspaces away.
//
// So a terminal that says it is focused and never takes it back is not a
// frontend with a missing feature: it is a frontend that has taken the
// notifications for one chat away from every other window on the machine, and
// the click with them. Saying so is two events the terminal already sends.

// updateSession tells the daemon where this window stands. Both halves travel
// together because the daemon keeps no history: the last one it was told is the
// whole of what it knows.
func (a *App) updateSession() {
	request := a.request
	if request == nil {
		return
	}
	a.mu.Lock()
	params := proto.Params{"focused": a.focused, "active_chat_id": a.activeChat}
	a.mu.Unlock()
	request("session.update", params, nil)
}

// windowFocus records that the terminal came to the front or left it, and tells
// the daemon when that changed. Nothing on the frame draws it, so nothing is
// redrawn for it.
func (a *App) windowFocus(focused bool) bool {
	a.mu.Lock()
	changed := a.focused != focused
	a.focused = focused
	a.mu.Unlock()
	if changed {
		a.updateSession()
	}
	return false
}
