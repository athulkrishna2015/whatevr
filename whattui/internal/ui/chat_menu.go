package ui

// The chat context menu: what you can do to the chat it was opened on. The
// actions are the registry's own commands, so each one is a palette entry and
// a slash command too; this is the same set with the labels flipped to say
// what pressing them will do, which the palette cannot do because the palette
// has no chat to be about.

// chatActions is what a chat menu offers, in the order it offers it. Grouped by
// what it is for rather than alphabetised, so a menu read top to bottom reads
// as the chat, then the notifications, then the person.
var chatActions = []commandID{
	cmdMarkRead,
	cmdPin,
	cmdUnmute,
	cmdArchive,
	cmdFavorite,
	cmdBlock,
}

// chatChoicesLocked is the menu over one chat. The labels name the action and
// its inverse, asked of the row itself: a menu offering "Pin" over a chat that
// is already pinned is a menu that lies.
func (a *App) chatChoicesLocked(chatID string) []modalChoice {
	a.initCommands()
	row, ok := a.chatRow(chatID)
	if !ok {
		return nil
	}
	state := a.commandStateLocked()
	out := make([]modalChoice, 0, len(chatActions))
	for _, id := range chatActions {
		c := a.commands.byID[id]
		if c == nil {
			continue
		}
		if c.Enabled != nil {
			if ok, why := c.Enabled(state); !ok {
				out = append(out, modalChoice{Command: id, Label: c.Title, Disabled: why})
				continue
			}
		}
		label := c.Title
		switch id {
		case cmdPin:
			label = onOff(row.Pinned, "Pin chat", "Unpin chat")
		case cmdUnmute:
			label = onOff(row.Muted, "Mute chat", "Unmute chat")
		case cmdArchive:
			label = onOff(row.Archived, "Archive chat", "Unarchive chat")
		case cmdFavorite:
			label = onOff(row.Favorite, "Favourite chat", "Unfavourite chat")
		case cmdBlock:
			label = onOff(a.blocked(chatID), "Block contact", "Unblock contact")
		}
		out = append(out, modalChoice{Command: id, Target: chatID, Label: label, Detail: c.Direct})
	}
	return out
}

// onOff names a toggle after the press, not the state: a row that says what
// it will do is a row nobody has to read twice to work out.
func onOff(on bool, whenOn, whenOff string) string {
	if on {
		return whenOff
	}
	return whenOn
}
