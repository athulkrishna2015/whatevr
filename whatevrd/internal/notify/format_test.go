package notify

import (
	"testing"

	"whatevrd/internal/live"
)

func TestParseCapabilities(t *testing.T) {
	caps := ParseCapabilities([]string{"actions", "body", "body-markup", "image-path", "persistence", "sound"})
	if !caps.Actions || !caps.Body || !caps.BodyMarkup || !caps.ImagePath || !caps.Persistence || !caps.Sound {
		t.Fatalf("capabilities not parsed: %+v", caps)
	}
}

func TestFormatWithBody(t *testing.T) {
	c := Format(Capabilities{Body: true, Actions: true}, live.Notification{Title: "Alice", Body: " hello\nthere ", Count: 1})
	if c.Summary != "Alice" || c.Body != "hello there" {
		t.Fatalf("unexpected content: %+v", c)
	}
	if len(c.Actions) == 0 {
		t.Fatal("expected action")
	}
}

func TestFormatWithoutBody(t *testing.T) {
	c := Format(Capabilities{}, live.Notification{Title: "Family", Body: "Alice: hello", Count: 1})
	if c.Summary != "Family: Alice: hello" || c.Body != "" {
		t.Fatalf("unexpected content: %+v", c)
	}
}

func TestFormatCount(t *testing.T) {
	c := Format(Capabilities{Body: true}, live.Notification{Title: "Alice", Body: "hi", Count: 3})
	if c.Summary != "Alice (3)" {
		t.Fatalf("expected the count in the title, got %q", c.Summary)
	}
}

func TestFormatEscapesMarkup(t *testing.T) {
	c := Format(Capabilities{Body: true, BodyMarkup: true}, live.Notification{Title: "Alice", Body: "<hello>"})
	if c.Body != "&lt;hello&gt;" {
		t.Fatalf("expected escaped markup, got %q", c.Body)
	}
}

func TestFormatHiddenPreview(t *testing.T) {
	c := Format(Capabilities{Body: true}, live.Notification{Title: "Alice"})
	if c.Summary != "Alice" || c.Body != "New message" {
		t.Fatalf("expected hidden preview, got %+v", c)
	}
}

func TestFormatAvatar(t *testing.T) {
	c := Format(Capabilities{Body: true, ImagePath: true}, live.Notification{Title: "Alice", Avatar: "/a.jpg"})
	if c.Icon != "/a.jpg" || c.Hints["image-path"] != "/a.jpg" {
		t.Fatalf("expected the avatar, got %+v", c)
	}
}
