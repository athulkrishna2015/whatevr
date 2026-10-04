package ui

import (
	"slices"
	"strings"
	"testing"
)

// without is a daemon whose hello leaves out the features named
func without(a *App, missing ...string) {
	a.offers = func(feature string) bool { return !slices.Contains(missing, feature) }
}

func TestWhatTheDaemonDoesNotOfferIsNotOffered(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	calls := records(a)
	without(a, "send_text", "message_star")
	pointAt(a, t, false)
	a.paint()

	gone := []commandID{cmdSend, cmdReply, cmdStar}
	lists := map[string][]modalChoice{
		"palette": a.commandChoices("", false, false),
		"help":    a.commandChoices("", false, true),
		"slash":   a.commandChoices("", true, false),
	}
	a.mu.Lock()
	lists["menu"] = a.messageChoicesLocked()
	a.mu.Unlock()
	for name, list := range lists {
		for _, c := range list {
			if slices.Contains(gone, c.Command) {
				t.Errorf("the %s offers %s", name, c.Command)
			}
		}
	}
	if hints := a.hintBarText(); strings.Contains(hints, "r reply") || !strings.Contains(hints, "y copy") {
		t.Errorf("the hint line %q is wrong about what this daemon does", hints)
	}

	a.onKey(key('s'))
	if len(*calls) != 0 {
		t.Fatalf("an unoffered star went to the daemon: %+v", *calls)
	}
	if !a.toastRefused || a.toastText != "this whatevrd cannot star message" {
		t.Errorf("the star key said %q", a.toastText)
	}
}

func TestDeleteAsksOnlyAboutTheKindTheDaemonDoes(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	records(a)
	without(a, "message_revoke")
	pointAt(a, t, true)

	a.onKey(key('d'))
	if a.modal.kind != modalConfirm || len(a.modal.selector.items) != 1 || a.modal.selector.items[0].Command != cmdDeleteForMe {
		t.Fatalf("the confirmation offers %+v, want delete for me alone", a.modal.selector.items)
	}
}

func TestChatSearchIsRefusedWhenNotOffered(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	calls := records(a)
	without(a, "search_chats")

	a.searchChats("contact", 0)
	if len(*calls) != 0 {
		t.Fatalf("the search went to the daemon: %+v", *calls)
	}
	if !a.toastRefused || a.toastText != "this whatevrd cannot search chats" {
		t.Errorf("the search said %q", a.toastText)
	}
}
