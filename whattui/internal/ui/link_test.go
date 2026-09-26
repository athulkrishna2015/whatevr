package ui

import "testing"

func TestAWordWithNoUrlIsOneSpan(t *testing.T) {
	got := linkSpans("hello")
	if len(got) != 1 || got[0].link != "" {
		t.Fatalf("linkSpans(hello) = %#v", got)
	}
}

func TestSentencePunctuationStaysOutOfTheUrl(t *testing.T) {
	got := linkSpans("(https://kde.org/plasma-desktop/)")
	if len(got) != 3 {
		t.Fatalf("linkSpans = %#v, want three spans", got)
	}
	if got[1].text != "https://kde.org/plasma-desktop/" || got[1].link != got[1].text {
		t.Fatalf("url span = %#v", got[1])
	}
	if got[0].text != "(" || got[2].text != ")" {
		t.Fatalf("punctuation = %q %q", got[0].text, got[2].text)
	}
}

func TestABareHostGetsAScheme(t *testing.T) {
	got := linkSpans("www.archlinux.org")
	if got[0].link != "https://www.archlinux.org" {
		t.Fatalf("link = %q", got[0].link)
	}
}

func TestASchemeWithNothingAfterItIsNotALink(t *testing.T) {
	for _, s := range []string{"https://", "www.", "http://."} {
		if got := linkSpans(s); got[0].link != "" {
			t.Fatalf("linkSpans(%q) = %#v, want plain text", s, got)
		}
	}
}
