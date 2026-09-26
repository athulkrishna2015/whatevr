package ui

import (
	"encoding/json"
	"fmt"
	"image"

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
		vx:      win.Vx,
		caps:    term.Caps{Tier: term.TierColor, RGB: true},
		theme:   theme.Derive(vaxis.RGBColor(0x12, 0x14, 0x18), vaxis.RGBColor(0xe4, 0xe4, 0xe6)),
		chats:   view.NewCollection[proto.ChatRow](),
		conn:    view.NewObject[proto.Connection](),
		focus:   FocusComposer,
		hovered: -1,
		images:  map[imgKey]*vaxis.KittyImage{},
		seen:    map[imgKey]bool{},
		glyphs:  map[glyphKey]*image.NRGBA{},
		drag:    drag{chat: -1},
	}
	// A client that has never dialled, which is what the frame asks about
	// when it has to tell the reader the daemon is not there.
	a.client = proto.New("/nonexistent/whattui-test.sock", "whattui-test")
	a.request = func(string, proto.Params, proto.ResponseFunc) {}
	a.transport = proto.Ready

	a.conn.Upsert("", mustJSON(proto.Connection{State: "online"}))
	a.conn.Ready(false, false)
	for i := 0; i < chats; i++ {
		id := fmt.Sprintf("%d@s.whatsapp.net", 910000000+i)
		a.chats.Upsert(fmt.Sprintf("%020d", i), mustJSON(proto.ChatRow{
			ID: id, Name: fmt.Sprintf("contact %d", i), Unread: int32(i % 4),
			Preview:         "the daemon owns all state and the frontend owns none of it",
			LastMessageTime: 1758000000 - int64(i)*900,
		}))
	}
	a.chats.Ready(true, true)

	c := &conversation{chatID: "910000000@s.whatsapp.net", msgs: view.NewCollection[proto.MessageRow]()}
	c.msgs.SetReverse(true)
	for i := 0; i < msgs; i++ {
		dir := "incoming"
		if i%2 == 0 {
			dir = "outgoing"
		}
		c.msgs.Upsert(fmt.Sprintf("%020d", i), mustJSON(proto.MessageRow{
			ID: fmt.Sprintf("m%d", i), Kind: "text", Direction: dir, Status: "read",
			Timestamp: 1758000000 + int64(i)*60,
			Sender:    proto.Sender{ID: "910000000@s.whatsapp.net", Name: "contact 0"},
			Text: "PROTOCOL.md is the source of truth, the daemon implements the " +
				"document and not the other way around, see https://example.com/spec",
		}))
	}
	c.msgs.Ready(true, true)
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

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// tierApp is stubApp dressed as a terminal of a given ability. It is for the
// tests that are about what a tier draws rather than about what an account
// contains, which is why the rows are still hand built.
func tierApp(cols, rows int, tier term.Tier) *App {
	return atTier(stubApp(cols, rows, 8, 5), tier)
}
