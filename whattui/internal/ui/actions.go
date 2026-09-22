package ui

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"whattui/internal/proto"
)

// What you can do to the message the cursor is on. Every one of them is a
// registry command, so every one of them is a letter, a palette entry and a
// slash command at once, and the hint line names the ones that apply while the
// cursor is where it is.
//
// None of them change a row. The daemon owns every message, so an action is a
// request and the answer arrives as an upsert through the view we are already
// subscribed to: a frontend that edited its own copy would have to reconcile it
// against the real one a moment later.

// replySelected points the draft at the message the cursor is on. The cursor
// lets go, because the strip above the composer is now the thing saying which
// message this is about.
func (a *App) replySelected() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	// The colour a person is known by, the same one their rule and their disc
	// carry in the transcript. It is what says who this is about before the
	// name is read.
	colour := a.theme.Accent
	if !m.Outgoing() {
		colour = a.theme.IdentityFor(m.Sender.ID)
	}

	a.mu.Lock()
	a.composer.replyTo = m.ID
	a.composer.editing = ""
	a.composer.targetName = replyName(m)
	a.composer.targetText = oneLine(a.body(m))
	a.composer.targetColour = colour
	a.mu.Unlock()
	a.clearCursor()
	a.setFocus(FocusComposer)
}

// editSelected loads a message you sent back into the composer. Sending then
// replaces it rather than saying it again.
func (a *App) editSelected() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	a.mu.Lock()
	a.composer.replyTo = ""
	a.composer.editing = m.ID
	a.composer.targetName = "editing"
	a.composer.targetText = oneLine(m.Text)
	a.composer.targetColour = a.theme.Warning
	a.composer.text = []rune(m.Text)
	a.composer.cursor = len(a.composer.text)
	a.mu.Unlock()
	a.clearCursor()
	a.setFocus(FocusComposer)
}

// copySelected puts the message on the clipboard through OSC 52, which is the
// same path the mouse selection takes and the reason copying works over ssh.
func (a *App) copySelected() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	text := m.Body()
	if strings.TrimSpace(text) == "" {
		return
	}
	a.vx.ClipboardPush(text)
	a.toast("copied " + plural(utf8.RuneCountInString(text), "character") + " to the clipboard")
}

// reactSelected opens the emoji picker on the message the cursor is on. The
// picker leads with what is already on it, because joining a reaction or taking
// yours back is what most of them are.
func (a *App) reactSelected() { a.openMessageModal(modalReact, point{}) }

// forwardSelected opens the chat picker. The daemon does the forwarding: a
// forward is not the same words sent again, it is WhatsApp's own forward, and
// the row that lands in the other chat says where it came from.
func (a *App) forwardSelected() { a.openMessageModal(modalForward, point{}) }

func (a *App) forward(messageID string, chatIDs []string) {
	if messageID == "" || len(chatIDs) == 0 {
		return
	}
	a.do("message.forward", proto.Params{"message_id": messageID, "chat_ids": chatIDs}, func() {
		a.toast("forwarded to " + plural(len(chatIDs), "chat"))
	})
}

// menuSelected opens the context menu on the message the cursor is on. From the
// keyboard there is no pointer to put it under, so it goes under the message
// itself, which is the thing it is about.
func (a *App) menuSelected() {
	id := a.cursor()
	at := point{}
	a.mu.Lock()
	for _, m := range a.messages {
		if m.id == id {
			at = point{col: m.at.Col + 2, row: m.at.Row}
			break
		}
	}
	a.mu.Unlock()
	a.openMessageModal(modalMenu, at)
}

func (a *App) starSelected() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	starred := !m.Starred
	a.do("message.star", proto.Params{"message_id": m.ID, "starred": starred}, func() {
		if starred {
			a.toast("starred")
			return
		}
		a.toast("unstarred")
	})
}

// deleteSelected asks which kind of delete this is. Two different things wear
// the word: one takes the message off this device, the other takes it away from
// everybody, and the second cannot be undone.
func (a *App) deleteSelected() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	choices := make([]modalChoice, 0, 2)
	if m.Outgoing() && !m.Revoked {
		choices = append(choices, modalChoice{
			Command: cmdRevoke, Label: "Delete for everyone", Detail: "nobody in the chat keeps it",
		})
	}
	choices = append(choices, modalChoice{
		Command: cmdDeleteForMe, Label: "Delete for me", Detail: "this device only",
	})

	a.mu.Lock()
	a.modal = modalState{kind: modalConfirm, prompt: a.clip(oneLine(a.body(m)), 60)}
	a.modal.selector.Set(choices)
	a.mu.Unlock()
}

func (a *App) deleteSelectedForMe() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	a.do("message.delete", proto.Params{"message_id": m.ID}, func() { a.toast("deleted here") })
}

func (a *App) revokeSelected() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	a.do("message.revoke", proto.Params{"message_id": m.ID}, func() { a.toast("deleted for everyone") })
}

// do sends a command and says what the daemon said if it refused. Nothing
// waits for the answer: what the action actually did arrives as an upsert.
func (a *App) do(method string, params proto.Params, done func()) {
	request := a.request
	if request == nil {
		return
	}
	request(method, params, func(_ json.RawMessage, err *proto.Error) {
		if err != nil {
			a.refuse(err.Message)
			return
		}
		if done != nil {
			done()
		}
	})
}

// replyName is who the strip says you are answering. Your own message says so
// in the first person, the way it reads in the sentence somebody is about to
// write.
func replyName(m proto.MessageRow) string {
	if m.Outgoing() {
		return "you"
	}
	if m.Sender.Name != "" {
		return m.Sender.Name
	}
	return m.Sender.ID
}

// oneLine squeezes a message onto a single row, for the places that name a
// message rather than show it.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
