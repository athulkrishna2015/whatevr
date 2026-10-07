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
		Name:        "media",
		Description: "every attachment kind, for bubbles, downloads and playback",
		Build:       buildMedia,
	})
	Register(Scenario{
		Name:        "outbox",
		Description: "messages the phone left pending and failed in history, which must never go out from here",
		Build:       buildOutbox,
	})
	Register(Scenario{
		Name:        "history",
		Description: "a read-only chat, a chat at its start, and one whose older history waits on the phone",
		Build:       buildHistory,
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
	// Nothing but emoji, which is the one message in a chat a frontend may
	// draw larger than the words around it. Two of them, because three or
	// fewer is one size and more than that is another.
	group.Say(ravi, "\U0001F389", Ago(110*time.Minute))
	group.SayFromMe("\U0001F602\U0001F525\U0001F44D\U0001F389", Ago(105*time.Minute))
	// An emoji-only reply, which is the one message that is drawn larger and
	// hangs under a line of somebody else's words at the same time.
	dinner := group.Say(meera, "dinner at eight \U0001F642, bring whatever you like", Ago(100*time.Minute))
	group.Quote(asha, dinner, "\U0001F44D", Ago(99*time.Minute))
	group.Quote(w.Self(), dinner, "\U0001F602", Ago(98*time.Minute))

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
	starred := team.Say(people[0], "standing up", Ago(20*time.Minute))
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
	// App state made somewhere else: this is what a frontend sees when the
	// phone pins a chat or marks one unread while it is connected.
	w.After(12*time.Second, func() {
		w.DM(people[0]).Pin()
		w.DM(people[7]).MarkRead()
		starred.Star()
	})
	w.After(16*time.Second, func() {
		family.Unpin()
		w.DM(people[1]).MarkUnread()
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

// buildMedia is the scenario for anything about attachments. Every file it
// produces is real: a jpeg that decodes, a webp sticker, a quicktime clip
// ffmpeg pulls a poster out of, a wav with a shape to it, and a pdf that opens.
// Half of it arrives through history sync and half live, because the two paths
// build the message from different protobufs.
func buildMedia(w *World) {
	asha := w.Contact("917770000001", "Asha")
	ravi := w.Contact("917770000002", "Ravi")

	dm := w.DM(asha)
	dm.AttachHistory(asha, Image("from before this device existed"), Ago(26*time.Hour))
	dm.AttachHistory(asha, Document("quarterly-report.pdf"), Ago(25*time.Hour))
	dm.Attach(asha, Image("a photo with a caption"), Ago(3*time.Hour))
	dm.Attach(asha, Image(""), Ago(2*time.Hour+50*time.Minute))
	dm.Attach(asha, Voice(6*time.Second), Ago(2*time.Hour+40*time.Minute))
	dm.AttachFromMe(Image("and one from this account"), Ago(2*time.Hour+30*time.Minute))
	dm.Say(asha, "that is the still picture set", Ago(2*time.Hour))

	group := w.Group("Media test group", asha, ravi)
	group.Attach(ravi, Video("a clip that plays", 4*time.Second), Ago(90*time.Minute))
	group.Attach(asha, GIF("looping and muted"), Ago(80*time.Minute))
	group.Attach(ravi, VideoNote(5*time.Second), Ago(70*time.Minute))
	group.Attach(asha, Audio("a shared track", 8*time.Second), Ago(60*time.Minute))
	group.Attach(ravi, Sticker(), Ago(50*time.Minute))
	group.Attach(asha, Document("notes.txt"), Ago(40*time.Minute))
	group.Attach(ravi, Image("a wide one, for layout").Size(1280, 360), Ago(30*time.Minute))
	group.Attach(asha, Image("and a tall one").Size(360, 1280), Ago(20*time.Minute))
	group.Say(ravi, "and that is everything else", Ago(10*time.Minute)).Star()

	// Something arriving live, so a download can be watched rather than found
	// already finished.
	w.After(4*time.Second, func() {
		group.Attach(ravi, Video("this one arrived while you were looking", 6*time.Second), time.Now())
	})
}

// buildOutbox is the history a linked device must not act on: two messages
// of ours the phone never got out. sending either from here would be this
// device speaking for the phone, days late. the mock logs loudly if one of
// them ever arrives.
func buildOutbox(w *World) {
	asha := w.Contact("917770000001", "Asha")
	ravi := w.Contact("917770000002", "Ravi")
	dm := w.DM(asha)
	dm.History(asha, "are you coming tonight", Ago(30*time.Hour))
	pending := dm.HistoryFromMe("typed on the phone with no signal", Ago(29*time.Hour)).Pending()
	group := w.Group("Outbox test group", asha, ravi)
	group.History(ravi, "photos from the trip?", Ago(28*time.Hour))
	failed := group.HistoryFromMe("this one failed on the phone", Ago(27*time.Hour)).Failed()
	dm.Say(asha, "say anything, it should go out; the two old ones should not", Ago(time.Minute))
	stale := map[string]bool{pending.ID: true, failed.ID: true}
	w.OnSend(func(m *Msg) {
		if stale[m.ID] {
			w.srv.log.Error().Str("id", m.ID).Str("text", m.Text).Msg("OUTBOX VIOLATION: a message history left pending or failed was sent")
		}
	})
}

// buildHistory is the account for everything above the oldest message: Ira's
// chat keeps 200 of its 260 messages on the phone until the client asks,
// Kabir's has nothing older anywhere, and the announcements nobody but the
// admins may write to.
func buildHistory(w *World) {
	ira := w.Contact("917770000011", "Ira")
	kabir := w.Contact("917770000012", "Kabir")
	admin := w.Contact("917770000013", "Neha")

	long := w.DM(ira).HoldBack(200)
	for i := 0; i < 260; i++ {
		at := Ago(time.Duration(260-i) * 20 * time.Minute)
		text := fmt.Sprintf("line %d", i+1)
		if i%3 == 0 {
			long.HistoryFromMe(text, at)
		} else {
			long.History(ira, text, at)
		}
	}

	news := w.Group("Announcements", admin).SetReadOnly(true)
	news.History(admin, "office closed on friday", Ago(26*time.Hour))
	news.History(admin, "the new badges are at the front desk", Ago(25*time.Hour))

	old := w.DM(kabir).SetHistoryEnd(true)
	old.History(kabir, "hey, this is my new number", Ago(30*24*time.Hour))
	old.HistoryFromMe("saved it", Ago(30*24*time.Hour-time.Minute))
	old.History(kabir, "see you at the reunion", Ago(29*24*time.Hour))
}
