package proto

import "testing"

// A caption is the message's own text, and the fallback says what the message
// is. A photo with words shows both, in that order, and a frontend that drew
// the fallback for a kind whose caption had already stood in for it would draw
// the caption twice and never say the thing was a video.
func TestACaptionIsDrawnUnderWhatTheMessageIsAndNotTwice(t *testing.T) {
	body := MessageRow{
		Kind: "video", Fallback: "\U0001F3A5 Video (0:04)", Text: "3 min Plank",
	}.Body()
	if body != "\U0001F3A5 Video (0:04)\n3 min Plank" {
		t.Errorf("a captioned video reads %q", body)
	}
	// And a kind with nothing to say about itself is its caption, with no
	// blank line standing in for the line it never had.
	if body := (MessageRow{Kind: "interactive", Text: "tap to continue"}).Body(); body != "tap to continue" {
		t.Errorf("a kind with no line of its own reads %q", body)
	}
}
