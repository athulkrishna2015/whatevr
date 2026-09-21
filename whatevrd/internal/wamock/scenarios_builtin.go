//go:build whatevr_mock

package wamock

import (
	"fmt"
	"time"
)

// The built-in scenarios. A scenario is a description of an account, not a
// script of frames: it says who is in the world and what they said, and the
// real daemon decides what any of that looks like.

func init() {
	Register(Scenario{
		Name:        "empty",
		Description: "a freshly paired account with no chats",
	})
	Register(Scenario{
		Name:        "visual",
		Description: "the fixed conversation the whattui screenshot harness expects",
		Build:       buildVisual,
	})
	Register(Scenario{
		Name:        "echo",
		Description: "somebody types back whatever you send, for the send lifecycle",
		Build:       buildEcho,
	})
	Register(Scenario{
		Name:        "sync",
		Description: "a long, paced initial history sync, for the sync view",
		Build:       buildSync,
	})
	Register(Scenario{
		Name:        "busy",
		Description: "several chats with recent traffic, for scrolling and ordering",
		Build:       buildBusy,
	})
}

// buildVisual is the account the screenshot harness photographs. The literal
// strings READY-HARNESS and Visual Test Group are load-bearing: scripts/
// whattui-screenshot waits for them on screen before it takes a picture, so
// renaming either one breaks the harness rather than the scenario.
func buildVisual(w *World) {
	asha := w.Contact("917770000001", "Asha")
	ravi := w.Contact("917770000002", "Ravi")
	meera := w.Contact("917770000003", "Meera")
	// One contact nobody saved, so both name fallbacks have a case: "~Unknown
	// Caller" in the group transcript, the bare number in the chat list.
	stranger := w.Contact("917770000009", "Unknown Caller").Unsaved()

	group := w.Group("Visual Test Group", asha, ravi, meera, stranger)
	// History is what the account already had. It arrives through a real
	// history sync, so the transcript has something to scroll back into and
	// the sync view has progress to report.
	for i := 0; i < 12; i++ {
		at := Ago(time.Duration(30-i) * time.Hour)
		if i%3 == 0 {
			group.HistoryFromMe(fmt.Sprintf("older message %d, from this account", i), at)
			continue
		}
		group.History([]*Contact{asha, ravi, meera}[i%3], fmt.Sprintf("older message %d", i), at)
	}
	group.Say(asha, "READY-HARNESS: stable synthetic conversation", Ago(3*time.Hour))
	group.Say(ravi, "pick a chat to see it render", Ago(3*time.Hour-90*time.Second))
	group.SayFromMe("this side is the account itself", Ago(3*time.Hour-3*time.Minute))
	group.Say(meera, "and this one wraps far enough to exercise a second line of text in the transcript", Ago(3*time.Hour-5*time.Minute))
	group.Say(asha, "short", Ago(2*time.Hour))

	direct := w.DM(asha)
	direct.Say(asha, "a one to one chat, for the header without a member count", Ago(90*time.Minute))
	direct.SayFromMe("replied from the phone", Ago(85*time.Minute))

	quiet := w.DM(ravi)
	quiet.Say(ravi, "yesterday, so the day divider has something to divide", Ago(26*time.Hour))

	w.DM(stranger).Say(stranger, "a contact who is not in the address book", Ago(50*time.Hour))
	group.Say(stranger, "and a group message from somebody unsaved", Ago(100*time.Minute))
}

// buildBusy is the scenario for anything about ordering: enough chats that the
// list scrolls, spread over enough time that the sort is visible.
func buildBusy(w *World) {
	names := []struct{ phone, name string }{
		{"917770000001", "Asha"},
		{"917770000002", "Ravi"},
		{"917770000003", "Meera"},
		{"917770000004", "Dev"},
		{"917770000005", "Nikhil"},
		{"917770000006", "Priya"},
		{"917770000007", "Sana"},
		{"917770000008", "Vikram"},
	}
	people := make([]*Contact, 0, len(names))
	for _, entry := range names {
		people = append(people, w.Contact(entry.phone, entry.name))
	}

	team := w.Group("Team standup", people[0], people[1], people[2], people[3])
	team.Say(people[0], "standing up", Ago(20*time.Minute))
	team.Say(people[1], "same", Ago(19*time.Minute))
	team.SayFromMe("on my way", Ago(18*time.Minute))

	family := w.Group("Family", people[4], people[5])
	family.Say(people[5], "dinner at eight", Ago(4*time.Hour))
	family.Pin()
	for i := 0; i < 40; i++ {
		family.History(people[4+i%2], fmt.Sprintf("backfilled %d", i), Ago(time.Duration(48-i)*time.Hour))
	}

	for i, person := range people {
		chat := w.DM(person)
		chat.Say(person, "message from "+person.Name, Ago(time.Duration(i+1)*37*time.Minute))
		switch i {
		case 5:
			chat.Mute()
		case 6:
			chat.Archive()
		case 7:
			chat.Unread(4)
		}
	}

	// Something arriving while a frontend watches is the only way to see an
	// upsert reorder the list, which no static fixture can show.
	w.After(3*time.Second, func() {
		team.Say(people[2], "one more, live", time.Now())
	})
	w.After(8*time.Second, func() {
		w.DM(people[7]).Say(people[7], "and another, in a different chat", time.Now())
	})
}

// buildEcho is the scenario for anything about sending: every message the
// account sends is answered by somebody in the same chat, with a composing
// indicator in between.
func buildEcho(w *World) {
	asha := w.Contact("917770000001", "Asha")
	ravi := w.Contact("917770000002", "Ravi")

	dm := w.DM(asha)
	dm.Say(asha, "say anything and it comes back", Ago(2*time.Minute))
	w.Group("Echo chamber", asha, ravi).Say(ravi, "same in here", Ago(time.Minute))
	w.SetOnline(asha, true)

	w.OnSend(func(m *Msg) {
		replier := m.Chat.Other()
		if replier == nil {
			return
		}
		m.Chat.Typing(replier, 900*time.Millisecond)
		time.Sleep(time.Second)
		m.Chat.Reply(replier, "you said: "+m.Text)
	})
}

// buildSync is the scenario for the one UI state that cannot be produced on
// demand against a real account: an initial history sync in progress. It is all
// history and no backlog, spread over enough chunks and enough seconds to watch.
func buildSync(w *World) {
	names := []string{"Asha", "Ravi", "Meera", "Dev", "Nikhil", "Priya", "Sana", "Vikram"}
	people := make([]*Contact, 0, len(names))
	for i, name := range names {
		people = append(people, w.Contact(fmt.Sprintf("91777000%04d", i+1), name))
	}
	w.HistoryPace(1500 * time.Millisecond)

	group := w.Group("Syncing group", people...)
	for i := 0; i < 60; i++ {
		group.History(people[i%len(people)], fmt.Sprintf("group history %d", i), Ago(time.Duration(120-i)*time.Hour))
	}
	for i, person := range people {
		chat := w.DM(person)
		for j := 0; j < 20; j++ {
			chat.History(person, fmt.Sprintf("%s history %d", person.Name, j), Ago(time.Duration(100-j)*time.Hour-time.Duration(i)*time.Minute))
		}
	}
}
