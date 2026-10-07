package ui

import (
	"strings"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// A reaction is the one thing in a chat that is content without being a
// message: it belongs under the words it is about, it is never what somebody
// said, and a screenful of them has to stay quieter than the conversation.
//
// The daemon counts them per emoji, most first, and names at most a few of
// the people behind them on the row. The strip draws the counts as they came;
// the names are for the picker.

// reactionGroup is one emoji, how many put it there, and the ones we can name.
type reactionGroup struct {
	emoji string
	count int
	names []string
	mine  bool
}

// reactionGroups is a message's reactions by emoji in the daemon's order,
// named from reactors where they say who.
func reactionGroups(counts []*v2.ReactionCount, reactors []*v2.Reaction) []reactionGroup {
	out := make([]reactionGroup, 0, len(counts))
	at := make(map[string]int, len(counts))
	for _, c := range counts {
		emoji := strings.TrimSpace(c.GetEmoji())
		if emoji == "" || c.GetCount() == 0 {
			continue
		}
		at[emoji] = len(out)
		out = append(out, reactionGroup{emoji: emoji, count: int(c.GetCount()), mine: c.GetMine()})
	}
	for _, r := range reactors {
		if i, ok := at[strings.TrimSpace(r.GetEmoji())]; ok {
			out[i].names = append(out[i].names, reactorName(r))
		}
	}
	return out
}

// reactorName is who a reaction is from, in the words a sentence about it would
// use. Yours is "you" because the row it lands on is a row you press to take it
// back.
func reactorName(r *v2.Reaction) string {
	p := r.GetSender()
	switch {
	case p.GetSelf():
		return "you"
	case strings.TrimSpace(p.GetName()) != "":
		return p.GetName()
	case p.GetPhone() != "":
		return p.GetPhone()
	case p.GetId() != "":
		return p.GetId()
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

const (
	// pillPad is the air inside a pill, per side. One column, because a shape
	// with round ends pads its sides to its own height rather than to a flat
	// unit, and a cell is half as wide as it is tall: one column is the least
	// that keeps the curve off the glyph.
	pillPad = 1
	// pillGap is the air between two pills. One column, now that each of them
	// carries its own edge: two would read as a gap in the row rather than
	// between the things in it.
	pillGap = 1
)

// cells is how wide one pill is drawn, its own air included.
func (a *App) cells(p pill) int { return a.width(p.text) + 2*pillPad }

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
		if g.count > 1 {
			text += " " + itoa(g.count)
		}
		next := pill{text: text, mine: g.mine}
		width := a.cells(next)
		if used > 0 {
			width += pillGap
		}
		// The last of the room goes to saying how many are missing, which is
		// only worth the columns when there is something after this one.
		rest := pill{text: "+" + itoa(len(groups)-i)}
		if used+width > room || (i < len(groups)-1 && used+width+pillGap+a.cells(rest) > room) {
			if used == 0 {
				return nil, 0
			}
			pills = append(pills, rest)
			used += pillGap + a.cells(rest)
			break
		}
		pills = append(pills, next)
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
	req := &v2.Request{}
	req.SetMessageReact(v2.MessageReact_builder{MessageId: messageID, Emoji: emoji}.Build())
	a.do(req, func() {
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
func (a *App) reactChoices(m *v2.MessageRow, query string) []modalChoice {
	reactors := m.GetReactions()
	if a.reactionsFor == m.GetId() && a.reactors != nil {
		reactors = a.reactors
	}
	groups := reactionGroups(m.GetReactionCounts(), reactors)
	out := make([]modalChoice, 0, len(groups)+len(reactionPalette))
	on := map[string]bool{}
	for _, g := range groups {
		on[g.emoji] = true
		// The same shape as a palette row: the emoji, then words about it. Who
		// put it there rather than how many, because the names are what the
		// strip under the message had no room for, and counting them is
		// something the reader can do by looking.
		detail := ""
		if g.mine {
			detail = "enter takes it back"
		}
		names := strings.Join(g.names, ", ")
		if more := g.count - len(g.names); more > 0 {
			if names != "" {
				names += ", "
			}
			names += "+" + itoa(more)
		}
		out = append(out, modalChoice{
			Emoji: g.emoji, Label: g.emoji + "  " + names,
			Detail: detail, Mine: g.mine,
		})
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

// reactorsPage is how many reactors the picker asks for. the row's counts stay
// whole past it, as "+n"
const reactorsPage = 50

// followReactions keeps a reactions subscription on the message the picker is
// open on and none otherwise, and refills the picker when it changes. outside
// App.mu, on every paint
func (a *App) followReactions() {
	a.mu.Lock()
	want := ""
	if a.modal.kind == modalReact {
		want = a.modal.message
	}
	c, sub, had, seen := a.reactions, a.reactionsSub, a.reactionsFor, a.reactionsSeen
	a.mu.Unlock()

	if want != had {
		if sub != nil {
			sub.Close()
		}
		c, sub = nil, nil
		if want != "" {
			c = view.NewCollection(view.Reaction)
			sub = a.client.Subscribe(v2.Subscribe_builder{
				Limit:     reactorsPage,
				Reactions: v2.ReactionsView_builder{MessageId: want}.Build(),
			}.Build(), c, proto.Hooks{})
		}
		a.mu.Lock()
		a.reactions, a.reactionsSub, a.reactionsFor, a.reactionsSeen, a.reactors = c, sub, want, 0, nil
		a.mu.Unlock()
		return
	}
	if c == nil || !c.IsReady() || c.Version() == seen {
		return
	}
	version := c.Version()
	var reactors []*v2.Reaction
	c.Read(func(items []view.Item[*v2.Reaction], _ view.State) {
		reactors = make([]*v2.Reaction, 0, len(items))
		for _, it := range items {
			reactors = append(reactors, it.Value)
		}
	})
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reactionsFor != want || a.modal.kind != modalReact {
		return
	}
	a.reactors, a.reactionsSeen = reactors, version
	if m, ok := a.selectedMessageLocked(); ok && m.GetId() == want {
		a.modal.selector.Refresh(a.reactChoices(m, string(a.modal.query)), maxInt(a.modal.visible, 1))
	}
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
