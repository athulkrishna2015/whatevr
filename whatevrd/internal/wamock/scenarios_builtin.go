//go:build whatevr_mock

package wamock

import "time"

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

	group := w.Group("Visual Test Group", asha, ravi, meera)
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

	for i, person := range people {
		chat := w.DM(person)
		chat.Say(person, "message from "+person.Name, Ago(time.Duration(i+1)*37*time.Minute))
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
