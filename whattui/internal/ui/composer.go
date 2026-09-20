package ui

import (
	"encoding/json"
	"strings"
	"unicode"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
)

// maxComposerRows is how far the composer grows before it scrolls instead.
// Past this it is eating the transcript, which is the thing the reader is
// actually here for.
const maxComposerRows = 5

// composer is the text being written and where the cursor is in it.
//
// Runes rather than bytes throughout: every movement is by grapheme or by
// word, and byte offsets in a message full of emoji and devanagari are a
// source of corruption, not of speed.
type composer struct {
	text   []rune
	cursor int
	// kill is the last thing cut, for ctrl+y, because a kill with nothing to
	// put it back into is a trap rather than an editor.
	kill []rune
	// sendErr is what the daemon said about the last send, cleared the moment
	// the reader types again.
	sendErr string
}

func (c *composer) empty() bool { return len(c.text) == 0 }

func (c *composer) String() string { return string(c.text) }

func (c *composer) clear() {
	c.text = c.text[:0]
	c.cursor = 0
}

func (c *composer) insert(s string) {
	rs := []rune(s)
	c.text = append(c.text[:c.cursor], append(rs, c.text[c.cursor:]...)...)
	c.cursor += len(rs)
}

func (c *composer) deleteBack() {
	if c.cursor == 0 {
		return
	}
	c.text = append(c.text[:c.cursor-1], c.text[c.cursor:]...)
	c.cursor--
}

func (c *composer) deleteForward() {
	if c.cursor >= len(c.text) {
		return
	}
	c.text = append(c.text[:c.cursor], c.text[c.cursor+1:]...)
}

// cut removes [from,to) and remembers it.
func (c *composer) cut(from, to int) {
	if from < 0 {
		from = 0
	}
	if to > len(c.text) {
		to = len(c.text)
	}
	if from >= to {
		return
	}
	c.kill = append(c.kill[:0], c.text[from:to]...)
	c.text = append(c.text[:from], c.text[to:]...)
	c.cursor = from
}

// wordLeft and wordRight are readline's words: skip the whitespace, then the
// run of non-whitespace. Not unicode words, because that is not what the
// fingers doing this expect.
func (c *composer) wordLeft() int {
	i := c.cursor
	for i > 0 && unicode.IsSpace(c.text[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(c.text[i-1]) {
		i--
	}
	return i
}

func (c *composer) wordRight() int {
	i := c.cursor
	for i < len(c.text) && unicode.IsSpace(c.text[i]) {
		i++
	}
	for i < len(c.text) && !unicode.IsSpace(c.text[i]) {
		i++
	}
	return i
}

// lineStart and lineEnd are the bounds of the line the cursor is on, which is
// the whole text until somebody uses shift+enter.
func (c *composer) lineStart() int {
	for i := c.cursor - 1; i >= 0; i-- {
		if c.text[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

func (c *composer) lineEnd() int {
	for i := c.cursor; i < len(c.text); i++ {
		if c.text[i] == '\n' {
			return i
		}
	}
	return len(c.text)
}

func (c *composer) multiline() bool {
	for _, r := range c.text {
		if r == '\n' {
			return true
		}
	}
	return false
}

// onComposerKey is where the composer-first rule lives. Typing types, and the
// navigation keys only take the arrows while there is nothing typed.
//
// Everything in here is borrowed: the readline bindings from every shell, the
// enter and shift+enter split from every chat application. Nothing is a
// whattui invention, which is the point.
func (a *App) onComposerKey(k vaxis.Key) {
	defer a.syncSlashModal()
	a.mu.Lock()
	c := &a.composer
	a.mu.Unlock()

	switch {
	// Enter sends, shift+enter and ctrl+j break the line. Telling the two
	// apart needs the kitty keyboard protocol, which is why ctrl+j is there
	// at all and why the hint bar names whichever one is live.
	case k.Matches(vaxis.KeyEnter, vaxis.ModShift), k.Matches('j', vaxis.ModCtrl),
		k.Matches(vaxis.KeyEnter, vaxis.ModAlt):
		c.insert("\n")
	case k.Matches(vaxis.KeyEnter):
		a.execute(cmdSend)
	case k.Matches(vaxis.KeyEsc):
		if !c.empty() {
			c.clear()
			return
		}
		a.setFocus(FocusList)
	case k.Matches(vaxis.KeyBackspace):
		c.deleteBack()
	case k.Matches(vaxis.KeyDelete), k.Matches('d', vaxis.ModCtrl):
		c.deleteForward()
	case k.Matches('w', vaxis.ModCtrl), k.Matches(vaxis.KeyBackspace, vaxis.ModAlt):
		c.cut(c.wordLeft(), c.cursor)
	case k.Matches('d', vaxis.ModAlt):
		c.cut(c.cursor, c.wordRight())
	case k.Matches('u', vaxis.ModCtrl):
		c.cut(c.lineStart(), c.cursor)
	case k.Matches('k', vaxis.ModCtrl):
		at := c.cursor
		c.cut(at, c.lineEnd())
		c.cursor = at
	case k.Matches('y', vaxis.ModCtrl):
		c.insert(string(c.kill))

	case k.Matches(vaxis.KeyLeft), k.Matches('b', vaxis.ModCtrl):
		if c.cursor > 0 {
			c.cursor--
		}
	case k.Matches(vaxis.KeyRight), k.Matches('f', vaxis.ModCtrl):
		if c.cursor < len(c.text) {
			c.cursor++
		}
	case k.Matches(vaxis.KeyLeft, vaxis.ModCtrl), k.Matches('b', vaxis.ModAlt):
		c.cursor = c.wordLeft()
	case k.Matches(vaxis.KeyRight, vaxis.ModCtrl), k.Matches('f', vaxis.ModAlt):
		c.cursor = c.wordRight()
	case k.Matches(vaxis.KeyHome), k.Matches('a', vaxis.ModCtrl):
		c.cursor = c.lineStart()
	case k.Matches(vaxis.KeyEnd), k.Matches('e', vaxis.ModCtrl):
		c.cursor = c.lineEnd()

	// The arrows belong to the transcript while there is nothing typed, and
	// to the text the moment there is. One rule, no mode.
	case k.Matches(vaxis.KeyUp):
		if c.empty() || !c.multiline() {
			a.setFocus(FocusTranscript)
			a.scrollTranscript(1)
			return
		}
		c.moveLine(-1)
	case k.Matches(vaxis.KeyDown):
		if c.empty() || !c.multiline() {
			a.scrollTranscript(-1)
			return
		}
		c.moveLine(1)
	case k.Matches(vaxis.KeyPgUp):
		a.setFocus(FocusTranscript)
		a.scrollTranscript(a.transcriptPage())
	case k.Matches(vaxis.KeyPgDown):
		a.scrollTranscript(-a.transcriptPage())

	default:
		// Anything that generated text is text. A control sequence generates
		// none, so this needs no list of keys to exclude.
		if k.Text != "" {
			c.sendErr = ""
			c.insert(k.Text)
		}
	}
}

// moveLine walks the cursor a line up or down, keeping the column it was in
// as far as the new line allows.
func (c *composer) moveLine(by int) {
	col := c.cursor - c.lineStart()
	if by < 0 {
		start := c.lineStart()
		if start == 0 {
			c.cursor = 0
			return
		}
		c.cursor = start - 1
		c.cursor = c.lineStart() + minInt(col, c.lineEnd()-c.lineStart())
		return
	}
	end := c.lineEnd()
	if end >= len(c.text) {
		c.cursor = len(c.text)
		return
	}
	c.cursor = end + 1
	c.cursor = c.lineStart() + minInt(col, c.lineEnd()-c.lineStart())
}

// send hands the text to the daemon and empties the composer. The message
// comes back through the view we are already subscribed to, so nothing is
// inserted locally: a frontend that invents a row is a frontend that has to
// reconcile it later.
func (a *App) send() {
	a.mu.Lock()
	chat := a.activeChat
	text := strings.TrimRight(a.composer.String(), "\n")
	a.mu.Unlock()

	if chat == "" || strings.TrimSpace(text) == "" {
		return
	}

	a.mu.Lock()
	a.composer.clear()
	a.composer.sendErr = ""
	a.mu.Unlock()

	a.client.Do("send.text", proto.Params{"chat_id": chat, "text": text}, func(_ json.RawMessage, err *proto.Error) {
		if err == nil {
			return
		}
		a.mu.Lock()
		// Put the words back. Losing what somebody typed because a socket
		// blinked is the one unforgivable bug in a chat client.
		if a.composer.empty() {
			a.composer.text = []rune(text)
			a.composer.cursor = len(a.composer.text)
		}
		a.composer.sendErr = err.Message
		a.mu.Unlock()
		a.vx.PostEvent(redraw{})
	})
}

// composerRows is how many rows the text needs, clamped to what the screen can
// spare. Asked before the layout is computed, so it takes the width itself.
func (a *App) composerRows(width int) int {
	a.mu.Lock()
	text := a.composer.String()
	a.mu.Unlock()
	if width < 4 || text == "" {
		return 1
	}
	n := len(a.wrap(text, width-composerGutter))
	if n < 1 {
		n = 1
	}
	return minInt(n, maxComposerRows)
}

// composerGutter is the prompt marker plus the space each side of the text.
const composerGutter = 4
