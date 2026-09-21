package ui

import (
	"fmt"
	"strings"
	"testing"

	"whattui/internal/proto"
)

// nastyChatNames is the same corpus wamock's torture scenario serves, in the
// form this package can paint without a daemon. A name arrives from somebody
// else's phone and is drawn in a fixed column with a badge after it, so a name
// that measures wrong misaligns a whole list rather than one row.
var nastyChatNames = []string{
	"plain name",
	"\U0001F389",
	"مجموعة الاختبار",
	"日本語のグループ名前が長すぎる",
	"नाम गड़बड़",
	strings.Repeat("a very long group name ", 18),
	"name\nwith\nnewlines",
	"name\twith\ttabs",
	"\x1b[31mred name\x1b[0m",
	"   leading and trailing   ",
	"\U0001F468‍\U0001F469‍\U0001F467‍\U0001F466",
	"‮reversed name‬",
	"ＷＩＤＥ　ＮＡＭＥ",
	"before\x00after",
	"​​​zero width",
}

// Every glyph in a column shares one left edge. A chat list is the column that
// proves it: whatever a name is made of, the avatar, the name and the preview
// start where they start in every other row, and nothing spills out of the
// pane into the transcript beside it.
func TestChatNamesShareOneLeftEdge(t *testing.T) {
	a := stubApp(120, 40, 0, 0)
	a.chats.Reset()
	for i, name := range nastyChatNames {
		a.chats.Upsert(fmt.Sprintf("%020d", i), mustJSON(proto.ChatRow{
			ID: fmt.Sprintf("%d@s.whatsapp.net", 910000000+i), Name: name,
			Preview:         "preview",
			LastMessageTime: 1758000000 - int64(i)*900,
		}))
	}
	a.chats.Ready(true, true)
	a.paint()

	pane := a.layout().ChatList
	// Column zero is the bar that marks the open chat, and it is meant to be
	// the one thing that reaches further left than everything else.
	edges := map[int]int{}
	for row := 0; row < pane.Height; row++ {
		// The divider is the pane's own last column, not a row's ink.
		col := firstInkColumn(a, row, 1, pane.Width-1)
		if col < 0 {
			continue
		}
		// Two kinds of line, the name and the preview, each with its own edge.
		kind := row % 2
		if want, seen := edges[kind]; seen && col != want {
			t.Errorf("row %d starts at column %d, the others start at %d: %q",
				row, col, want, rowText(a, row, pane.Width))
			continue
		} else if !seen {
			edges[kind] = col
		}
		// The rule and the pane: a name wide enough to reach the divider must
		// have been clipped before it got there.
		if last := lastInkColumn(a, row, pane.Width-1); last >= pane.Width-2 {
			t.Errorf("row %d draws into the divider at column %d: %q",
				row, last, rowText(a, row, pane.Width))
		}
	}
	if len(edges) != 2 {
		t.Fatalf("the chat list drew %d kinds of line, want a name and a preview", len(edges))
	}
}

// No control character reaches the screen. A terminal acts on what it is
// handed, so a message or a name that carries an escape must arrive as text or
// not at all.
func TestNoControlCharactersReachTheScreen(t *testing.T) {
	a := stubApp(120, 40, 0, 0)
	a.chats.Reset()
	for i, name := range nastyChatNames {
		a.chats.Upsert(fmt.Sprintf("%020d", i), mustJSON(proto.ChatRow{
			ID: fmt.Sprintf("%d@s.whatsapp.net", 910000000+i), Name: name,
			Preview:         "\x1b[31mpreview\x07\x00\x1b]8;;https://evil.example\x1b\\",
			LastMessageTime: 1758000000 - int64(i)*900,
		}))
	}
	a.chats.Ready(true, true)
	a.paint()

	cols, rows := a.vx.Snapshot().Dimensions()
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			for _, r := range a.vx.Cell(col, row).Grapheme {
				if r == 0x1b || r == 0x00 || r == 0x07 || (r < 0x20 && r != '\t') || (r >= 0x80 && r <= 0x9f) {
					t.Fatalf("cell %d,%d carries control character %U", col, row, r)
				}
			}
		}
	}
}

func firstInkColumn(a *App, row, from, width int) int {
	for col := from; col < width; col++ {
		cell := a.vx.Cell(col, row)
		if strings.TrimSpace(cell.Grapheme) != "" {
			return col
		}
	}
	return -1
}

func lastInkColumn(a *App, row, width int) int {
	for col := width - 1; col >= 0; col-- {
		if strings.TrimSpace(a.vx.Cell(col, row).Grapheme) != "" {
			return col
		}
	}
	return -1
}

func rowText(a *App, row, width int) string {
	var b strings.Builder
	for col := 0; col < width; col++ {
		b.WriteString(a.vx.Cell(col, row).Grapheme)
	}
	return b.String()
}
