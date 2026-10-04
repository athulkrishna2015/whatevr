package ui

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"
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
	cmdReact         commandID = "message.react"
	cmdForward       commandID = "message.forward"
	cmdMenu          commandID = "message.menu"
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
	hasChat bool
	// readOnly is a chat we cannot send to
	readOnly  bool
	hasDraft  bool
	chatCount int
	// The message the selection cursor is on, and whether there is one. The
	// row is carried rather than fetched: whether an action applies is asked
	// under App.mu, and asking the collection under App.mu is a deadlock.
	hasMessage bool
	message    *v2.MessageRow
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
	canSend := func(state commandState) (bool, string) {
		if !state.hasChat {
			return false, "open a chat first"
		}
		if state.readOnly {
			return false, readOnlyNote
		}
		return true, ""
	}
	hasMessage := func(state commandState) (bool, string) {
		if !state.hasMessage {
			return false, "point at a message with the arrows first"
		}
		return true, ""
	}
	// What is left of a message somebody deleted for everybody is a sentence
	// saying one was here. There is nothing to answer, copy, star, react to or
	// pass on, and the only thing anybody can still do to it is take the note
	// off this device, which is what the delete command is for.
	hasLiveMessage := func(state commandState) (bool, string) {
		if ok, why := hasMessage(state); !ok {
			return false, why
		}
		if state.message.GetRevoked() {
			return false, "that message is already deleted"
		}
		return true, ""
	}
	a.commands = newCommandRegistry([]command{
		{ID: cmdPalette, Title: "Command palette", Description: "Find an action or /chat", Direct: "^p", Leader: "p", Slash: "palette", Run: func() { a.openModal(modalPalette) }},
		{ID: cmdHelp, Title: "Keyboard help", Description: "Show every command and binding", Direct: "?", Leader: "?", Slash: "help", Run: func() { a.openModal(modalHelp) }},
		{ID: cmdSend, Title: "Send message", Description: "Send the current draft", Direct: "enter", Enabled: func(state commandState) (bool, string) {
			if ok, why := canSend(state); !ok {
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
		{ID: cmdReply, Title: "Reply to message", Description: "Answer the message the cursor is on", Direct: "r", Scope: scopeMessage, Slash: "reply", Enabled: func(state commandState) (bool, string) {
			if ok, why := canSend(state); !ok {
				return false, why
			}
			return hasLiveMessage(state)
		}, Run: a.replySelected},
		{ID: cmdReact, Title: "React to message", Description: "Put an emoji on the message, or take yours off", Direct: "+", Scope: scopeMessage, Slash: "react", Enabled: hasLiveMessage, Run: a.reactSelected},
		{ID: cmdForward, Title: "Forward message", Description: "Send the message on to other chats", Direct: "f", Scope: scopeMessage, Slash: "forward", Enabled: hasLiveMessage, Run: a.forwardSelected},
		{ID: cmdMenu, Title: "Actions on this message", Description: "Open the menu of everything the message can do", Direct: "m", Scope: scopeMessage, Slash: "menu", Enabled: hasLiveMessage, Run: a.menuSelected},
		{ID: cmdEditMessage, Title: "Edit message", Description: "Rewrite a message you sent", Direct: "e", Scope: scopeMessage, Slash: "edit", Enabled: func(state commandState) (bool, string) {
			if ok, why := hasMessage(state); !ok {
				return false, why
			}
			if ok, why := canSend(state); !ok {
				return false, why
			}
			// Asked before the composer ever loads the message, so a window
			// that has closed is an answer in the corner rather than an edit
			// that looks like it started and then bounces off the daemon.
			switch {
			case !state.message.GetFromMe():
				return false, "you can only edit your own messages"
			case state.message.GetRevoked():
				return false, "that message is already deleted"
			case state.message.GetEditUntilMs() == 0:
				return false, "this kind of message cannot be edited"
			case !editable(state.message, time.Now().UnixMilli()):
				return false, "too late, the edit window has closed"
			}
			return true, ""
		}, Run: a.editSelected},
		{ID: cmdCopyMessage, Title: "Copy message", Description: "Put the message on the clipboard", Direct: "y", Scope: scopeMessage, Slash: "copy", Enabled: hasLiveMessage, Run: a.copySelected},
		{ID: cmdStar, Title: "Star message", Description: "Star the message, or take the star off", Direct: "s", Scope: scopeMessage, Slash: "star", Enabled: hasLiveMessage, Run: a.starSelected},
		{ID: cmdDelete, Title: "Delete message", Description: "Ask which kind of delete this is", Direct: "d", Scope: scopeMessage, Slash: "delete", Enabled: hasMessage, Run: a.deleteSelected},
		{ID: cmdDeleteForMe, Title: "Delete for me", Description: "Take the message off this device only", Slash: "delete-for-me", Enabled: hasMessage, Run: a.deleteSelectedForMe},
		{ID: cmdRevoke, Title: "Delete for everyone", Description: "Take the message away from everybody in the chat", Slash: "delete-for-everyone", Enabled: func(state commandState) (bool, string) {
			if ok, why := hasMessage(state); !ok {
				return false, why
			}
			if !state.message.GetFromMe() {
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
	// Pasted text is text, whatever letters are in it. Shift is allowed because
	// the shifted glyph is already in the text: a binding on "+" is a binding
	// on the key somebody actually pressed to get one.
	if k.Modifiers&^vaxis.ModShift != 0 || k.Text == "" || k.EventType == vaxis.EventPaste {
		return "", false
	}
	for _, c := range a.commands.ordered {
		if c.Scope == scopeMessage && c.Direct == k.Text {
			return c.ID, true
		}
	}
	return "", false
}

// matchesBinding reads a command's own spelling of its key. The registry is
// where a binding is written down, so the spelling is what has to be
// understood here: a switch over the bindings that happened to exist when it
// was written silently drops the next one somebody adds, which is a command
// the palette offers, the help lists, the hint bar names, and the key does
// nothing about.
func matchesBinding(k vaxis.Key, binding string) bool {
	switch binding {
	case "":
		return false
	case "tab":
		return k.Matches(vaxis.KeyTab)
	case "s-tab":
		return k.Matches(vaxis.KeyTab, vaxis.ModShift)
	case "enter":
		return k.Matches(vaxis.KeyEnter)
	case "esc":
		return k.Matches(vaxis.KeyEsc)
	}
	if ctrl, ok := strings.CutPrefix(binding, "^"); ok {
		if r, ok := oneRune(ctrl); ok {
			return k.Matches(r, vaxis.ModCtrl)
		}
		return false
	}
	if r, ok := oneRune(binding); ok {
		return k.Matches(r)
	}
	return false
}

func oneRune(s string) (rune, bool) {
	r, size := utf8.DecodeRuneInString(s)
	return r, size == len(s) && r != utf8.RuneError
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

// messageChoicesLocked is what a context menu holds: every action that applies
// to the message the cursor is on, with the key that does it beside it.
//
// The registry again rather than a list of its own, so an action added anywhere
// turns up here as well, and a menu can never offer something the key would
// refuse. The menu itself is left out of it: a row that opens the menu you are
// looking at is a row that does nothing.
func (a *App) messageChoicesLocked() []modalChoice {
	state := a.commandStateLocked()
	var out []modalChoice
	for _, c := range a.commands.ordered {
		if c.Scope != scopeMessage || c.ID == cmdMenu {
			continue
		}
		if c.Enabled != nil {
			if ok, _ := c.Enabled(state); !ok {
				continue
			}
		}
		label := c.Title
		if c.ID == cmdStar && state.message.GetStarred() {
			label = "Unstar message"
		}
		out = append(out, modalChoice{Command: c.ID, Label: label, Detail: c.Direct})
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
		readOnly:   a.conversation != nil && a.conversation.readOnly,
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
