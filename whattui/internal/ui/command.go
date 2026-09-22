package ui

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
)

type commandID string

const (
	cmdQuit          commandID = "app.quit"
	cmdInterrupt     commandID = "app.interrupt"
	cmdPalette       commandID = "palette.open"
	cmdHelp          commandID = "help.open"
	cmdSend          commandID = "message.send"
	cmdOpenChat      commandID = "chat.open"
	cmdFocusNext     commandID = "focus.next"
	cmdFocusPrevious commandID = "focus.previous"
	cmdBoxes         commandID = "transcript.boxes"
	cmdRedraw        commandID = "app.redraw"
	cmdReply         commandID = "message.reply"
	cmdEditMessage   commandID = "message.edit"
	cmdCopyMessage   commandID = "message.copy"
	cmdStar          commandID = "message.star"
	cmdDelete        commandID = "message.delete"
	cmdDeleteForMe   commandID = "message.delete-for-me"
	cmdRevoke        commandID = "message.delete-for-everyone"
)

// scope says where a direct binding is live. A binding with no scope belongs to
// the whole application; one with a scope is resolved by whatever owns that
// context, because a bare letter is only ever a binding while nobody is typing.
type scope int

const (
	scopeGlobal scope = iota
	// scopeMessage is live only while the selection cursor is on a message,
	// which is only ever while the composer is empty.
	scopeMessage
)

type command struct {
	ID          commandID
	Title       string
	Description string
	Direct      string
	Scope       scope
	Leader      string
	Slash       string
	Enabled     func(commandState) (bool, string)
	Run         func()
}

type commandRegistry struct {
	ordered []command
	byID    map[commandID]*command
}

type commandState struct {
	hasChat   bool
	hasDraft  bool
	chatCount int
	// The message the selection cursor is on, and whether there is one. The
	// row is carried rather than fetched: whether an action applies is asked
	// under App.mu, and asking the collection under App.mu is a deadlock.
	hasMessage bool
	message    proto.MessageRow
}

func newCommandRegistry(commands []command) commandRegistry {
	r := commandRegistry{ordered: commands, byID: make(map[commandID]*command, len(commands))}
	for i := range r.ordered {
		if r.ordered[i].ID == "" || r.byID[r.ordered[i].ID] != nil {
			panic("duplicate or empty command id: " + r.ordered[i].ID)
		}
		r.byID[r.ordered[i].ID] = &r.ordered[i]
	}
	return r
}

func (a *App) initCommands() {
	if a.commands.byID != nil {
		return
	}
	hasChat := func(state commandState) (bool, string) {
		if !state.hasChat {
			return false, "open a chat first"
		}
		return true, ""
	}
	hasMessage := func(state commandState) (bool, string) {
		if !state.hasMessage {
			return false, "point at a message with the arrows first"
		}
		return true, ""
	}
	a.commands = newCommandRegistry([]command{
		{ID: cmdPalette, Title: "Command palette", Description: "Find an action or /chat", Direct: "^p", Leader: "p", Slash: "palette", Run: func() { a.openModal(modalPalette) }},
		{ID: cmdHelp, Title: "Keyboard help", Description: "Show every command and binding", Direct: "?", Leader: "?", Slash: "help", Run: func() { a.openModal(modalHelp) }},
		{ID: cmdSend, Title: "Send message", Description: "Send the current draft", Direct: "enter", Enabled: func(state commandState) (bool, string) {
			if ok, why := hasChat(state); !ok {
				return false, why
			}
			if !state.hasDraft {
				return false, "draft is empty"
			}
			return true, ""
		}, Slash: "send", Run: a.send},
		{ID: cmdOpenChat, Title: "Open selected chat", Description: "Open the highlighted chat", Direct: "enter", Leader: "o", Enabled: func(state commandState) (bool, string) {
			if state.chatCount == 0 {
				return false, "no chats available"
			}
			return true, ""
		}, Slash: "open", Run: a.openSelected},
		{ID: cmdFocusNext, Title: "Focus next pane", Description: "Move focus clockwise", Direct: "tab", Slash: "focus-next", Run: func() { a.cycleFocus(1) }},
		{ID: cmdFocusPrevious, Title: "Focus previous pane", Description: "Move focus anticlockwise", Direct: "s-tab", Slash: "focus-previous", Run: func() { a.cycleFocus(-1) }},
		{ID: cmdBoxes, Title: "Toggle message boxes", Description: "Draw a panel per message instead of a rule per run", Leader: "b", Slash: "boxes", Run: a.toggleBoxes},
		{ID: cmdRedraw, Title: "Redraw the screen", Description: "Throw away what the terminal is showing and draw it again", Direct: "^l", Slash: "redraw", Run: a.redraw},
		{ID: cmdReply, Title: "Reply to message", Description: "Answer the message the cursor is on", Direct: "r", Scope: scopeMessage, Slash: "reply", Enabled: hasMessage, Run: a.replySelected},
		{ID: cmdEditMessage, Title: "Edit message", Description: "Rewrite a message you sent", Direct: "e", Scope: scopeMessage, Slash: "edit", Enabled: func(state commandState) (bool, string) {
			if ok, why := hasMessage(state); !ok {
				return false, why
			}
			// Asked before the composer ever loads the message, so a window
			// that has closed is an answer in the corner rather than an edit
			// that looks like it started and then bounces off the daemon.
			switch {
			case !state.message.Outgoing():
				return false, "you can only edit your own messages"
			case state.message.Revoked:
				return false, "that message is already deleted"
			case state.message.EditUntil == 0:
				return false, "this kind of message cannot be edited"
			case !state.message.Editable(time.Now().Unix()):
				return false, "too late, the edit window has closed"
			}
			return true, ""
		}, Run: a.editSelected},
		{ID: cmdCopyMessage, Title: "Copy message", Description: "Put the message on the clipboard", Direct: "y", Scope: scopeMessage, Slash: "copy", Enabled: hasMessage, Run: a.copySelected},
		{ID: cmdStar, Title: "Star message", Description: "Star the message, or take the star off", Direct: "s", Scope: scopeMessage, Slash: "star", Enabled: hasMessage, Run: a.starSelected},
		{ID: cmdDelete, Title: "Delete message", Description: "Ask which kind of delete this is", Direct: "d", Scope: scopeMessage, Slash: "delete", Enabled: hasMessage, Run: a.deleteSelected},
		{ID: cmdDeleteForMe, Title: "Delete for me", Description: "Take the message off this device only", Slash: "delete-for-me", Enabled: hasMessage, Run: a.deleteSelectedForMe},
		{ID: cmdRevoke, Title: "Delete for everyone", Description: "Take the message away from everybody in the chat", Slash: "delete-for-everyone", Enabled: func(state commandState) (bool, string) {
			if ok, why := hasMessage(state); !ok {
				return false, why
			}
			if !state.message.Outgoing() {
				return false, "you can only delete your own messages for everyone"
			}
			return true, ""
		}, Run: a.revokeSelected},
		{ID: cmdInterrupt, Title: "Clear draft or quit", Description: "Clear typed text, otherwise quit", Direct: "^c", Slash: "clear", Run: a.interrupt},
		{ID: cmdQuit, Title: "Quit", Description: "Close whattui", Direct: "^q", Leader: "q", Slash: "quit", Run: a.quitApp},
	})
}

func (a *App) execute(id commandID) bool {
	a.initCommands()
	c := a.commands.byID[id]
	if c == nil {
		return false
	}
	if c.Enabled != nil {
		if ok, why := c.Enabled(a.commandState()); !ok {
			a.refuse(why)
			return true
		}
	}
	c.Run()
	return true
}

func (a *App) quitApp() {
	a.mu.Lock()
	a.quit = true
	a.mu.Unlock()
}

func (a *App) interrupt() {
	a.mu.Lock()
	if !a.composer.empty() {
		a.composer.clear()
	} else {
		a.quit = true
	}
	a.mu.Unlock()
}

func (a *App) directCommand(k vaxis.Key) (commandID, bool) {
	a.initCommands()
	for _, c := range a.commands.ordered {
		if c.Scope != scopeGlobal {
			continue
		}
		// Textual and focus-dependent bindings are resolved by their pane.
		if c.Direct != "?" && c.Direct != "enter" && matchesBinding(k, c.Direct) {
			return c.ID, true
		}
	}
	return "", false
}

// messageCommand resolves a bare letter against the actions that act on the
// message the cursor is on. Only the caller knows whether there is one, and
// there is only ever one while nothing is typed: that is what makes a letter
// safe to spend here.
func (a *App) messageCommand(k vaxis.Key) (commandID, bool) {
	a.initCommands()
	// Pasted text is text, whatever letters are in it.
	if k.Modifiers != 0 || k.Text == "" || k.EventType == vaxis.EventPaste {
		return "", false
	}
	for _, c := range a.commands.ordered {
		if c.Scope == scopeMessage && c.Direct == k.Text {
			return c.ID, true
		}
	}
	return "", false
}

func matchesBinding(k vaxis.Key, binding string) bool {
	switch binding {
	case "^q":
		return k.Matches('q', vaxis.ModCtrl)
	case "^c":
		return k.Matches('c', vaxis.ModCtrl)
	case "^p":
		return k.Matches('p', vaxis.ModCtrl)
	case "tab":
		return k.Matches(vaxis.KeyTab)
	case "s-tab":
		return k.Matches(vaxis.KeyTab, vaxis.ModShift)
	case "enter":
		return k.Matches(vaxis.KeyEnter)
	case "?":
		return k.Matches('?')
	default:
		return false
	}
}

func (a *App) commandForDirect(binding string) (commandID, bool) {
	a.initCommands()
	for _, c := range a.commands.ordered {
		if c.Direct == binding {
			return c.ID, true
		}
	}
	return "", false
}

func (a *App) leaderCommand(k vaxis.Key) (commandID, bool) {
	a.initCommands()
	key := strings.ToLower(k.Text)
	for _, c := range a.commands.ordered {
		if c.Leader == key && k.Modifiers == 0 {
			return c.ID, true
		}
	}
	return "", false
}

func (a *App) commandChoices(query string, slashOnly, help bool) []modalChoice {
	return a.commandChoicesFor(query, slashOnly, help, a.commandState())
}

func (a *App) commandChoicesFor(query string, slashOnly, help bool, state commandState) []modalChoice {
	a.initCommands()
	type ranked struct {
		choice modalChoice
		score  int
		order  int
	}
	var matches []ranked
	for i, c := range a.commands.ordered {
		if slashOnly && c.Slash == "" {
			continue
		}
		disabled := ""
		if c.Enabled != nil {
			if ok, why := c.Enabled(state); !ok {
				disabled = why
			}
		}
		label := c.Title
		if slashOnly {
			label = "/" + c.Slash
		}
		detail := c.Description
		if help {
			detail = commandBindings(c)
		}
		score, ok := fuzzyScore(query, label+" "+c.Description)
		if !ok {
			continue
		}
		// A slash menu is a menu of slash names: what you typed matching the
		// name itself beats it turning up somewhere in a description.
		if slashOnly && strings.HasPrefix(c.Slash, strings.TrimPrefix(query, "/")) {
			score += 1000
		}
		matches = append(matches, ranked{modalChoice{Command: c.ID, Label: label, Detail: detail, Disabled: disabled}, score, i})
	}
	if query != "" {
		sort.SliceStable(matches, func(i, j int) bool {
			if matches[i].score == matches[j].score {
				return matches[i].order < matches[j].order
			}
			return matches[i].score > matches[j].score
		})
	}
	out := make([]modalChoice, len(matches))
	for i := range matches {
		out[i] = matches[i].choice
	}
	return out
}

func (a *App) commandState() commandState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.commandStateLocked()
}

func (a *App) commandStateLocked() commandState {
	message, hasMessage := a.selectedMessageLocked()
	return commandState{
		hasChat:    a.activeChat != "",
		hasDraft:   strings.TrimSpace(a.composer.String()) != "",
		chatCount:  a.chats.Len(),
		hasMessage: hasMessage,
		message:    message,
	}
}

// enabled is whether a command applies right now, for the surfaces that show
// only the ones that do.
func (a *App) enabled(id commandID, state commandState) bool {
	a.initCommands()
	c := a.commands.byID[id]
	if c == nil {
		return false
	}
	if c.Enabled == nil {
		return true
	}
	ok, _ := c.Enabled(state)
	return ok
}

func commandBindings(c command) string {
	var parts []string
	if c.Direct != "" {
		parts = append(parts, c.Direct)
	}
	if c.Leader != "" {
		parts = append(parts, "^x "+c.Leader)
	}
	if c.Slash != "" {
		parts = append(parts, "/"+c.Slash)
	}
	return strings.Join(parts, "  ")
}

func fuzzyScore(query, candidate string) (int, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	candidate = strings.ToLower(candidate)
	if query == "" {
		return 0, true
	}
	score, at, run := 0, 0, 0
	for _, r := range query {
		found := false
		for at < len(candidate) {
			c := rune(candidate[at])
			at++
			if unicode.ToLower(c) == r {
				run++
				score += 10 + run*2
				found = true
				break
			}
			run = 0
		}
		if !found {
			return 0, false
		}
	}
	return score, true
}
