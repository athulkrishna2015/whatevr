package ui

import (
	"strings"

	"whattui/internal/proto"
)

// A reaction is the one thing in a chat that is content without being a
// message: it belongs under the words it is about, it is never what somebody
// said, and a screenful of them has to stay quieter than the conversation.
//
// The daemon sends one row per reactor, because who reacted is a fact it has
// and a count is not. Three thumbs are three rows and one thing to read, so
// they are gathered by emoji here, in the order they first turned up, which is
// what every other client shows and what makes the strip stable while people
// pile on.

// reactionGroup is one emoji and everybody who put it there.
type reactionGroup struct {
	emoji string
	names []string
	mine  bool
}

// groupReactions gathers a message's reactions by emoji, first seen first.
func groupReactions(list []proto.Reaction) []reactionGroup {
	var out []reactionGroup
	at := map[string]int{}
	for _, r := range list {
		emoji := strings.TrimSpace(r.Emoji)
		if emoji == "" {
			continue
		}
		name := reactorName(r)
		i, seen := at[emoji]
		if !seen {
			at[emoji] = len(out)
			out = append(out, reactionGroup{emoji: emoji, names: []string{name}, mine: r.FromMe})
			continue
		}
		out[i].names = append(out[i].names, name)
		out[i].mine = out[i].mine || r.FromMe
	}
	return out
}

// reactorName is who a reaction is from, in the words a sentence about it would
// use. Yours is "you" because the row it lands on is a row you press to take it
// back.
func reactorName(r proto.Reaction) string {
	switch {
	case r.FromMe:
		return "you"
	case strings.TrimSpace(r.SenderName) != "":
		return r.SenderName
	case r.SenderID != "":
		return r.SenderID
	default:
		return "somebody"
	}
}

// mine is the emoji this account put on the message, or empty. WhatsApp allows
// one per person, so there is only ever the one.
func mineIn(groups []reactionGroup) string {
	for _, g := range groups {
		if g.mine {
			return g.emoji
		}
	}
	return ""
}

// pill is one group as it is drawn: the emoji, how many, and whether one of
// them was us.
type pill struct {
	text string
	mine bool
}

// pillGap is the air between two pills. The same two columns the hint line puts
// between its words, so a strip of reactions reads as a row of separate things
// rather than one run of glyphs.
const pillGap = 2

// pillsFor lays a message's reactions out as a strip no wider than room, and
// answers how wide it came out.
//
// What does not fit is counted rather than cut: half a pill is a lie about who
// reacted, and the number of people left out is the part worth keeping.
func (a *App) pillsFor(groups []reactionGroup, room int) ([]pill, int) {
	if len(groups) == 0 || room < 2 {
		return nil, 0
	}
	pills := make([]pill, 0, len(groups))
	used := 0
	for i, g := range groups {
		text := g.emoji
		if len(g.names) > 1 {
			text += " " + itoa(len(g.names))
		}
		width := a.width(text)
		if used > 0 {
			width += pillGap
		}
		// The last of the room goes to saying how many are missing, which is
		// only worth a column when there is something after this one.
		rest := "+" + itoa(len(groups)-i)
		if used+width > room || (i < len(groups)-1 && used+width+pillGap+a.width(rest) > room) {
			if used == 0 {
				return nil, 0
			}
			pills = append(pills, pill{text: rest})
			used += pillGap + a.width(rest)
			break
		}
		pills = append(pills, pill{text: text, mine: g.mine})
		used += width
	}
	return pills, used
}

// react puts an emoji on a message, or takes ours off when the emoji is empty.
// WhatsApp keeps one reaction per person, so sending another replaces it and
// nothing here has to take the old one off first.
func (a *App) react(messageID, emoji string) {
	if messageID == "" {
		return
	}
	a.do("message.react", proto.Params{"message_id": messageID, "emoji": emoji}, func() {
		if emoji == "" {
			a.toast("reaction removed")
			return
		}
		a.toast("reacted " + emoji)
	})
}

// reactChoices is the picker: what is already on the message first, so joining
// one or taking yours back is the shortest thing to do, and the palette after
// it. Searching runs over the names, which is the only part of an emoji a
// keyboard can type.
func (a *App) reactChoices(m proto.MessageRow, query string) []modalChoice {
	groups := groupReactions(m.Reactions)
	out := make([]modalChoice, 0, len(groups)+len(reactionPalette))
	on := map[string]bool{}
	for _, g := range groups {
		on[g.emoji] = true
		detail := strings.Join(g.names, ", ")
		if g.mine {
			detail = "yours, enter takes it back"
		}
		// The emoji, and how many only when there is more than one of them: a
		// row that says "1 reaction" beside the name of the one person who put
		// it there is a row saying the same thing twice.
		label := g.emoji
		if len(g.names) > 1 {
			label += "  " + itoa(len(g.names))
		}
		out = append(out, modalChoice{Emoji: g.emoji, Label: label, Detail: detail, Mine: g.mine})
	}
	for _, e := range reactionPalette {
		if on[e.emoji] {
			continue
		}
		if _, ok := fuzzyScore(query, e.name); !ok {
			continue
		}
		out = append(out, modalChoice{Emoji: e.emoji, Label: e.emoji + "  " + e.name})
	}
	return out
}

// The emoji a reaction is, in practice. The first six are the ones WhatsApp
// itself offers on a long press, in its order, because that is the row the
// reader's thumb already knows; the rest are the ones people reach past it for.
// A picker with every emoji in Unicode in it is a picker nobody finds anything
// in, and the composer is where an emoji goes when it is something you meant to
// say.
var reactionPalette = []struct{ emoji, name string }{
	{"👍", "thumbs up, yes, agreed"},
	{"❤️", "heart, love"},
	{"😂", "laughing, funny"},
	{"😮", "surprised, wow"},
	{"😢", "sad, crying"},
	{"🙏", "thanks, please, pray"},
	{"👎", "thumbs down, no"},
	{"🔥", "fire, great"},
	{"🎉", "party, congratulations"},
	{"👏", "clap, well done"},
	{"😍", "adore, lovely"},
	{"🤔", "thinking, hmm"},
	{"😅", "awkward, phew"},
	{"😭", "sobbing, too much"},
	{"😡", "angry, cross"},
	{"💯", "hundred, exactly"},
	{"✅", "done, check, tick"},
	{"❌", "no, wrong, cross"},
	{"👀", "eyes, looking"},
	{"🫡", "salute, on it"},
	{"🤝", "handshake, deal"},
	{"💀", "dead, too funny"},
	{"🥳", "celebrate, birthday"},
	{"😴", "sleep, boring"},
}
