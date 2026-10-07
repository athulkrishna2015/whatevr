package notify

import (
	"html"
	"strconv"
	"strings"
	"unicode/utf8"

	"whatevrd/internal/live"
)

const previewLimit = 200

type Capabilities struct {
	Actions     bool
	Body        bool
	BodyMarkup  bool
	IconStatic  bool
	ImagePath   bool
	Persistence bool
	Sound       bool
	InlineReply bool
}

type Content struct {
	Summary string
	Body    string
	Icon    string
	Actions []string
	Hints   map[string]any
}

func ParseCapabilities(values []string) Capabilities {
	var caps Capabilities
	for _, value := range values {
		switch value {
		case "actions":
			caps.Actions = true
		case "body":
			caps.Body = true
		case "body-markup":
			caps.BodyMarkup = true
		case "icon-static":
			caps.IconStatic = true
		case "image-path":
			caps.ImagePath = true
		case "persistence":
			caps.Persistence = true
		case "sound":
			caps.Sound = true
		case "inline-reply", "x-kde-reply":
			caps.InlineReply = true
		}
	}
	return caps
}

// Format lays a notification out for what the server can show. an empty
// body is a hidden preview, and says only that something came.
func Format(caps Capabilities, n live.Notification) Content {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = n.Chat
	}
	if n.Count > 1 {
		title += " (" + strconv.Itoa(n.Count) + ")"
	}
	body := strings.Join(strings.Fields(n.Body), " ")
	if body == "" {
		body = "New message"
	}
	body = truncate(body, previewLimit)
	// no sound-name hint: the worker plays the sound, and a server that
	// honours the hint would play it twice
	content := Content{Summary: title, Hints: map[string]any{"category": "im.received"}}
	if caps.Actions {
		content.Actions = []string{"default", "Open Chat", "mark-read", "Mark as read"}
		if caps.InlineReply {
			content.Actions = append(content.Actions, "reply", "Reply")
			content.Hints["x-kde-reply"] = "reply"
		}
	}
	if (caps.ImagePath || caps.IconStatic) && n.Avatar != "" {
		content.Icon = n.Avatar
		if caps.ImagePath {
			content.Hints["image-path"] = n.Avatar
		}
	}
	if caps.Body {
		content.Body = body
		if caps.BodyMarkup {
			content.Body = html.EscapeString(body)
		}
		return content
	}
	content.Summary = truncate(title+": "+body, previewLimit)
	return content
}

func truncate(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
