//go:build whatevr_mock

package wamock

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// The scenarios that exist to hurt. Everything above is an account somebody
// might have; these are accounts nobody has, built out of the cases that break
// a renderer: text a terminal might execute, graphemes a wrap might split,
// volumes a list might not page, and ties an order might not settle.

func init() {
	Register(Scenario{
		Name:        "frames",
		Description: "a fixed, quiet account for reference frames and screenshots",
		Build:       buildFrames,
	})
	Register(Scenario{
		Name:        "torture",
		Description: "every text and layout case that breaks a renderer, labelled",
		Build:       buildTorture,
	})
	Register(Scenario{
		Name:        "flood",
		Description: "far more chats and messages than anybody has, arriving fast",
		Build:       buildFlood,
	})
	Register(Scenario{
		Name:        "fuzz",
		Description: "a different account at every --mock-seed, the same one at each",
		Build:       buildFuzz,
	})
}

// framesChat is the name the whattui frame harness opens. It is load bearing
// the same way READY-HARNESS is: the test finds the chat by this string.
const framesChat = "Reference"

// buildFrames is the account the reference frames are taken against. Everything
// in it is fixed: no timeline, no live traffic, no attachment that has to
// finish downloading, and every timestamp hangs off --mock-now.
//
// Everything arrives through history sync, deliberately. A backlog delivered at
// login and a history chunk describing the same chat are two pipelines racing,
// and the unread badge is whichever of them lands last. That race is worth
// having in a scenario somebody is watching; it is not worth having in the one
// the frames are diffed against.
func buildFrames(w *World) {
	people := []*Contact{
		w.Contact("917770000001", "Asha"),
		w.Contact("917770000002", "Ravi"),
		w.Contact("917770000003", "Meera"),
		w.Contact("917770000004", "Dev"),
		w.Contact("917770000005", "Nikhil"),
		w.Contact("917770000006", "Priya"),
		w.Contact("917770000007", "Sana"),
	}
	stranger := w.Contact("917770000009", "Unknown Caller").Unsaved()

	main := w.Group(framesChat, people[0], people[1], people[2], stranger)
	// Enough to fill a tall transcript and to put a day divider in a short one,
	// spaced so no two ever tie on the sort key.
	for i := 0; i < 24; i++ {
		at := Ago(time.Duration(40-i) * time.Hour)
		switch i % 4 {
		case 0:
			main.HistoryFromMe(fmt.Sprintf("older message %d, from this account", i), at)
		case 3:
			main.History(stranger, fmt.Sprintf("older message %d, from somebody unsaved", i), at)
		default:
			main.History(people[i%3], fmt.Sprintf("older message %d", i), at)
		}
	}
	main.History(people[0], "READY-HARNESS: stable synthetic conversation", Ago(3*time.Hour))
	main.History(people[1], "pick a chat to see it render", Ago(3*time.Hour-90*time.Second))
	main.HistoryFromMe("this side is the account itself", Ago(3*time.Hour-3*time.Minute))
	// The hyperlink is required: a frame with no link cells means the link
	// detector stopped working, and the golden test fails on exactly that.
	main.History(people[2], "the spec is at https://example.com/spec and it wraps far enough to need a second line of text in the transcript", Ago(3*time.Hour-5*time.Minute))
	main.History(stranger, "and a group message from somebody unsaved", Ago(100*time.Minute))
	main.History(people[0], "short", Ago(2*time.Hour))
	main.Unread(3)

	direct := w.DM(people[0])
	direct.History(people[0], "a one to one chat, for the header without a member count", Ago(90*time.Minute))
	direct.HistoryFromMe("replied from the phone", Ago(85*time.Minute))
	direct.Pin()

	quiet := w.DM(people[1])
	quiet.History(people[1], "yesterday, so the day divider has something to divide", Ago(26*time.Hour))

	w.DM(people[3]).History(people[3], "muted, so the row carries the badge", Ago(5*time.Hour)).Chat.Mute()
	w.DM(people[4]).History(people[4], "unread, four of them", Ago(6*time.Hour)).Chat.Unread(4)
	w.DM(people[5]).History(people[5], "archived, so it is not in the list at all", Ago(7*time.Hour)).Chat.Archive()
	w.DM(people[6]).History(people[6], "and one more, to make the list scroll on a short terminal", Ago(8*time.Hour))
	w.DM(stranger).History(stranger, "a contact who is not in the address book", Ago(50*time.Hour))
}

// buildTorture is the sheet. Every entry in the corpus arrives as its own
// message, preceded by a label from the account so a screenshot can be read
// like a test sheet, and the label never changes how the sample beside it wraps
// because it is a separate message.
func buildTorture(w *World) {
	victim := w.Contact("917770000001", "Asha")
	witness := w.Contact("917770000002", "Ravi")

	sheet := w.Group("Text torture", victim, witness)
	at := Ago(48 * time.Hour)
	step := 90 * time.Second
	for _, entry := range nastyTexts() {
		sheet.HistoryFromMe("--- "+entry.Label+" ---", at)
		at = at.Add(step)
		sheet.History(victim, entry.Text, at)
		at = at.Add(step)
	}
	sheet.HistoryFromMe("--- forty thousand characters ---", at)
	at = at.Add(step)
	sheet.History(witness, hugeMessage(), at)

	// Names are measured in a fixed column with a badge after them, so a name
	// that measures wrong misaligns a whole list rather than one row.
	for i, name := range nastyNames() {
		person := w.Contact(fmt.Sprintf("91777100%04d", i), name)
		chat := w.DM(person)
		chat.Say(person, "my name is the test", Ago(time.Duration(40-i)*time.Hour))
		if i%3 == 0 {
			chat.Unread(uint32(i*7 + 1))
		}
	}
	named := w.Group(nastyNames()[4], victim, witness)
	named.Say(witness, "a group whose subject does not fit anywhere", Ago(30*time.Hour))

	// Everything about ordering that a tiebreak has to settle. A message
	// timestamp is seconds on the wire, so a burst sent inside one second is
	// twenty four messages with nothing to order them by but their ids, which
	// is the case the sort key exists for.
	ties := w.Group("Same second", victim, witness)
	tie := Ago(12 * time.Hour).Truncate(time.Second)
	for i := 0; i < 24; i++ {
		ties.History(victim, fmt.Sprintf("tied message %02d, all in the same second", i), tie)
	}
	ties.History(witness, "and one a second later", tie.Add(time.Second))

	edges := w.Group("Clock edges", victim)
	edges.History(victim, "sent at the unix epoch", time.Unix(0, 0))
	edges.History(victim, "sent in 1999", time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC))
	edges.History(victim, "sent two hours from now, because a phone had the wrong clock", Ago(-2*time.Hour))
	edges.Say(victim, "and one a year out", Ago(-365*24*time.Hour))
	edges.Unread(99999)

	// A group nobody should have, to find out what the member count and the
	// participant list do when there are more of them than rows on screen.
	crowd := make([]*Contact, 0, 300)
	for i := 0; i < 300; i++ {
		crowd = append(crowd, w.Contact(fmt.Sprintf("91777200%04d", i), fmt.Sprintf("Member %03d", i)))
	}
	huge := w.Group("Three hundred people", crowd...)
	for i := 0; i < 30; i++ {
		huge.History(crowd[i*7%len(crowd)], fmt.Sprintf("message %d from a crowd", i), Ago(time.Duration(20-i/2)*time.Hour))
	}
	huge.Pin()
	huge.Mute()

	// Attachments at sizes no layout was designed around.
	shapes := w.Group("Impossible media", victim)
	shapes.AttachHistory(victim, Image("one pixel tall").Size(4000, 1), Ago(9*time.Hour))
	shapes.AttachHistory(victim, Image("one pixel wide").Size(1, 4000), Ago(8*time.Hour))
	shapes.AttachHistory(victim, Image("square and enormous").Size(4096, 4096), Ago(7*time.Hour))
	shapes.Attach(victim, Image(nastyTexts()[4].Text), Ago(6*time.Hour))
	shapes.Attach(victim, Document(nastyNames()[4]+".pdf"), Ago(5*time.Hour))
	shapes.Attach(victim, Voice(0), Ago(4*time.Hour))
	shapes.Attach(victim, Video("zero length", 0), Ago(3*time.Hour))

	// What arrives while somebody is looking. A burst is the case a frontend
	// that redraws per message cannot keep up with.
	w.After(2*time.Second, func() {
		for i := 0; i < 120; i++ {
			sheet.Say(victim, fmt.Sprintf("burst %03d", i), Now())
		}
	})
	w.After(4*time.Second, func() {
		// Typing that never stops, from several people at once, which is the
		// state a composing indicator has to survive rather than accumulate.
		sheet.Typing(victim, 0)
		sheet.Typing(witness, 0)
		named.Typing(victim, 0)
	})
	w.After(6*time.Second, func() {
		// App state churn: the same chat pinned and unpinned faster than a list
		// animation can settle.
		for i := 0; i < 12; i++ {
			huge.SetPinned(i%2 == 0)
			edges.SetMuted(i%2 == 1)
		}
	})
	w.After(8*time.Second, func() {
		for _, entry := range nastyTexts() {
			ties.Say(witness, entry.Text, Now())
		}
	})
}

// buildFlood is about volume and nothing else: more chats than a page, more
// messages than a window, and a live burst that reorders the list under the
// reader's hands.
func buildFlood(w *World) {
	const (
		people = 400
		groups = 24
	)
	crowd := make([]*Contact, 0, people)
	for i := 0; i < people; i++ {
		crowd = append(crowd, w.Contact(fmt.Sprintf("91777300%04d", i), fmt.Sprintf("Person %04d", i)))
	}

	// One chat deeper than any window, so paging older has somewhere to go.
	deep := w.DM(crowd[0])
	for i := 0; i < 3000; i++ {
		at := Ago(time.Duration(3000-i) * time.Minute)
		if i%3 == 0 {
			deep.HistoryFromMe(fmt.Sprintf("deep %04d, from this account", i), at)
			continue
		}
		deep.History(crowd[0], fmt.Sprintf("deep %04d", i), at)
	}

	for i := 1; i < people; i++ {
		chat := w.DM(crowd[i])
		chat.History(crowd[i], fmt.Sprintf("only message from %s", crowd[i].Name), Ago(time.Duration(i)*7*time.Minute))
		switch i % 11 {
		case 0:
			chat.Pin()
		case 1:
			chat.Mute()
		case 2:
			chat.Archive()
		case 3:
			chat.Unread(uint32(i % 97))
		}
	}
	for g := 0; g < groups; g++ {
		members := crowd[g*10 : g*10+10]
		group := w.Group(fmt.Sprintf("Group %02d", g), members...)
		for i := 0; i < 60; i++ {
			group.History(members[i%len(members)], fmt.Sprintf("group %02d message %02d", g, i), Ago(time.Duration(200-i)*time.Minute))
		}
	}

	// The reorder storm: messages in random chats, faster than a human reads,
	// which is what turns a list into a flicker if anything sorts in the wrong
	// place.
	w.After(2*time.Second, func() {
		for i := 0; i < 400; i++ {
			person := crowd[(i*37)%people]
			w.DM(person).Say(person, fmt.Sprintf("flood %03d", i), Now())
		}
	})
	w.After(6*time.Second, func() {
		for i := 0; i < 200; i++ {
			deep.Say(crowd[0], fmt.Sprintf("and %03d more in the chat you are reading", i), Now())
		}
	})
}

// buildFuzz is the one that finds what nobody thought of. Everything it does is
// drawn from --mock-seed, so a crash is reproducible from the one number in the
// log line, and a run at the same seed is the same account twice.
func buildFuzz(w *World) {
	r := w.Rand()
	people := make([]*Contact, 0, 60)
	for i := 0; i < 60; i++ {
		person := w.Contact(fmt.Sprintf("91777400%04d", i), fuzzName(r))
		if r.Intn(4) == 0 {
			person.Unsaved()
		}
		people = append(people, person)
	}

	chats := make([]*Chat, 0, 40)
	for i := 0; i < 40; i++ {
		if r.Intn(3) == 0 {
			members := make([]*Contact, 0, 8)
			for j := 0; j < 1+r.Intn(8); j++ {
				members = append(members, people[r.Intn(len(people))])
			}
			chats = append(chats, w.Group(fuzzName(r), members...))
			continue
		}
		chats = append(chats, w.DM(people[r.Intn(len(people))]))
	}

	for _, chat := range chats {
		for i := 0; i < r.Intn(40); i++ {
			at := Ago(time.Duration(r.Intn(60*24*7)) * time.Minute)
			from := chat.Other()
			if chat.IsGroup {
				from = chat.Members[r.Intn(len(chat.Members))]
			}
			switch {
			case from == nil || from.JID == w.Self().JID:
				chat.HistoryFromMe(fuzzText(r), at)
			case r.Intn(12) == 0:
				chat.AttachHistory(from, fuzzAttachment(r), at)
			default:
				chat.History(from, fuzzText(r), at)
			}
		}
		switch r.Intn(8) {
		case 0:
			chat.Pin()
		case 1:
			chat.Mute()
		case 2:
			chat.Archive()
		case 3:
			chat.Unread(uint32(r.Intn(500)))
		}
	}

	// Live mutation, at a rate nothing is designed for, for as long as anybody
	// leaves it running.
	for tick := 1; tick <= 20; tick++ {
		w.After(time.Duration(tick)*time.Second, func() {
			for i := 0; i < 5+r.Intn(40); i++ {
				chat := chats[r.Intn(len(chats))]
				from := chat.Other()
				if chat.IsGroup {
					from = chat.Members[r.Intn(len(chat.Members))]
				}
				if from == nil || from.JID == w.Self().JID {
					continue
				}
				switch r.Intn(10) {
				case 0:
					chat.SetPinned(r.Intn(2) == 0)
				case 1:
					chat.SetMuted(r.Intn(2) == 0)
				case 2:
					chat.Typing(from, time.Duration(r.Intn(3000))*time.Millisecond)
				case 3:
					chat.Attach(from, fuzzAttachment(r), Now())
				default:
					chat.Say(from, fuzzText(r), Now())
				}
			}
		})
	}
}

// fuzzText glues corpus entries, real words and junk together, so most messages
// look plausible and a few are anything but.
func fuzzText(r *rand.Rand) string {
	corpus := nastyTexts()
	words := strings.Fields("the daemon owns all state and the frontend owns none of it " +
		"pixel perfect is the bar for anything anybody looks at twice " +
		"चलो कल मिलते हैं ok sounds good 明日の会議は何時ですか")
	switch r.Intn(10) {
	case 0:
		return corpus[r.Intn(len(corpus))].Text
	case 1:
		return strings.Repeat(corpus[r.Intn(len(corpus))].Text, 1+r.Intn(6))
	case 2:
		return hugeMessage()[:1+r.Intn(20000)]
	case 3:
		return zalgo(words[r.Intn(len(words))], 1+r.Intn(20))
	}
	var b strings.Builder
	for i := 0; i < 1+r.Intn(60); i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(words[r.Intn(len(words))])
		if r.Intn(25) == 0 {
			b.WriteString(" " + corpus[r.Intn(len(corpus))].Text)
		}
		if r.Intn(30) == 0 {
			b.WriteString(" https://example.com/" + fmt.Sprint(r.Int63()))
		}
	}
	return b.String()
}

func fuzzName(r *rand.Rand) string {
	names := nastyNames()
	plain := strings.Fields("Asha Ravi Meera Dev Nikhil Priya Sana Vikram Farah Omar Lin Yuki")
	switch r.Intn(4) {
	case 0:
		return names[r.Intn(len(names))]
	case 1:
		return plain[r.Intn(len(plain))] + " " + names[r.Intn(len(names))]
	}
	return plain[r.Intn(len(plain))]
}

func fuzzAttachment(r *rand.Rand) *Attachment {
	switch r.Intn(8) {
	case 0:
		return Video(fuzzText(r), time.Duration(r.Intn(30))*time.Second)
	case 1:
		return GIF(fuzzText(r))
	case 2:
		return Voice(time.Duration(r.Intn(120)) * time.Second)
	case 3:
		return Audio(fuzzName(r), time.Duration(r.Intn(600))*time.Second)
	case 4:
		return Sticker()
	case 5:
		return Document(fuzzName(r) + ".pdf")
	case 6:
		return VideoNote(time.Duration(r.Intn(60)) * time.Second)
	}
	return Image(fuzzText(r)).Size(1+r.Intn(2000), 1+r.Intn(2000))
}
