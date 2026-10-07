package ui

import (
	"fmt"
	"image"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/theme"
	"whattui/internal/view"
)

// stubApp is a whole frontend drawing into a cell buffer with rows nobody sent:
// no terminal, no socket, the same paint path the real one runs.
//
// It is for the tests that probe one thing about drawing and want to say
// exactly what is on screen while they do it. Anything that asks what a real
// account looks like uses mockApp instead, because the answer to that is the
// daemon's to give.
func stubApp(cols, rows, chats, msgs int) *App {
	// A cell of ten by twenty pixels, so anything that draws in pixels has
	// something to measure off. Whether it does is the tier's business.
	win := vaxis.NewOffscreenWindowPixels(cols, rows, 10, 20)
	a := &App{
		vx:       win.Vx,
		caps:     term.Caps{Tier: term.TierColor, RGB: true},
		theme:    theme.Derive(vaxis.RGBColor(0x12, 0x14, 0x18), vaxis.RGBColor(0xe4, 0xe4, 0xe6)),
		chats:    view.NewCollection(view.Chat),
		conn:     view.NewObject(view.Connection),
		syncs:    view.NewObject(view.Sync),
		problems: view.NewCollection(view.Problem),
		login:    view.NewObject(view.Login),
		focus:    FocusComposer,
		focused:  true,
		hovered:  -1,
		images:   map[imgKey]*vaxis.KittyImage{},
		seen:     map[imgKey]bool{},
		glyphs:   map[glyphKey]*image.NRGBA{},
		drag:     drag{chat: -1},
	}
	// A client that has never dialled, which is what the frame asks about
	// when it has to tell the reader the daemon is not there.
	a.client = proto.New("/nonexistent/whattui-test.sock", "whattui-test")
	a.request = func(*v2.Request, proto.ResponseFunc) {}
	a.transport = proto.Ready

	setConn(a.conn, online())
	ready(a.conn, false)
	for i := 0; i < chats; i++ {
		id := fmt.Sprintf("%d@s.whatsapp.net", 910000000+i)
		putChat(a.chats, fmt.Sprintf("%020d", i), v2.ChatRow_builder{
			Id: id, Name: fmt.Sprintf("contact %d", i), Unread: uint32(i % 4),
			Preview: preview("the daemon owns all state and the frontend owns none of it"),
			LastMs:  (1758000000 - int64(i)*900) * 1000,
		}.Build())
	}
	ready(a.chats, true)

	c := &conversation{chatID: "910000000@s.whatsapp.net", window: &window{msgs: view.NewCollection(view.Message), size: messagePageSize, live: true, newerDone: true}}
	c.msgs.SetReverse(true)
	for i := 0; i < msgs; i++ {
		fromMe := false
		// An edit window still open, which is what the daemon puts on a
		// message you have just written. It is wall-clock rather than fixed
		// because it is a deadline, and nothing draws it, so no frame moves.
		editUntil := int64(0)
		if i%2 == 0 {
			fromMe = true
			editUntil = time.Now().Add(10 * time.Minute).UnixMilli()
		}
		putMsg(c.msgs, fmt.Sprintf("%020d", i), v2.MessageRow_builder{
			Id: fmt.Sprintf("m%d", i), TextBody: &v2.Text{}, FromMe: fromMe, Status: v2.MessageStatus_MESSAGE_STATUS_READ,
			TMs: (1758000000 + int64(i)*60) * 1000, EditUntilMs: editUntil,
			Sender: person("910000000@s.whatsapp.net", "contact 0"),
			Text: "PROTOCOL.md is the source of truth, the daemon implements the " +
				"document and not the other way around, see https://example.com/spec",
		}.Build())
	}
	ready(c.msgs, true)
	a.conversation = c
	a.activeChat = c.chatID
	return a
}

// vaxisResize is a resize the way a terminal reports one: cells and the pixels
// they are made of.
func vaxisResize(cols, rows int) vaxis.Resize {
	return vaxis.Resize{Cols: cols, Rows: rows, XPixel: cols * 10, YPixel: rows * 20}
}

// fontResize is a font size change: the window stands still and the grid under
// it is made of bigger cells.
func fontResize(cols, rows, cellW, cellH int) vaxis.Resize {
	return vaxis.Resize{Cols: cols, Rows: rows, XPixel: cols * cellW, YPixel: rows * cellH}
}

// tierApp is stubApp dressed as a terminal of a given ability. It is for the
// tests that are about what a tier draws rather than about what an account
// contains, which is why the rows are still hand built.
func tierApp(cols, rows int, tier term.Tier) *App {
	return atTier(stubApp(cols, rows, 8, 5), tier)
}
