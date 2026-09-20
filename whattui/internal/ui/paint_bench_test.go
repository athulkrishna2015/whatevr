package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/textrun"
	"whattui/internal/theme"
	"whattui/internal/view"
)

// benchApp is a whole frontend drawing into a cell buffer: no terminal, no
// socket, the same paint path the real one runs.
func benchApp(cols, rows, chats, msgs int) *App {
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

func BenchmarkPaintFullFrame(b *testing.B) {
	a := benchApp(120, 40, 200, 400)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.paint()
	}
}

// The pointer moving across the chat list changes one row's background and
// nothing else, and is the frame that has to be quick: motion arrives for
// every pixel the pointer crosses.
func BenchmarkPaintHoverMove(b *testing.B) {
	a := benchApp(120, 40, 200, 400)
	a.paint()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.hovered = i % 18
		a.paint()
	}
}

// The same frame with the scripts that have to be shaped and rasterised. This
// is the expensive case and the one a pointer drag has to keep up with.
func BenchmarkPaintComplexScript(b *testing.B) {
	a := benchApp(120, 40, 200, 400)
	a.caps = term.Caps{Tier: term.TierShm, RGB: true}
	a.shaper = textrun.New(textrun.Options{})
	a.shaper.SetCellSize(10, 21)
	a.conversation.msgs.Reset()
	for i := 0; i < 400; i++ {
		a.conversation.msgs.Upsert(fmt.Sprintf("%020d", i), mustJSON(proto.MessageRow{
			ID: fmt.Sprintf("m%d", i), Kind: "text", Direction: "incoming", Status: "read",
			Timestamp: 1758000000 + int64(i)*60,
			Sender:    proto.Sender{ID: "910000000@s.whatsapp.net", Name: "Khatabook"},
			Text: "नमस्ते सर, Khatabook के इंस्टेंट लोन के साथ अपने बिजनेस के " +
				"सपनों को हकीकत बनाएँ – ₹5,00,000 तक!",
		}))
	}
	a.conversation.msgs.Ready(true, true)

	deadline := time.Now().Add(30 * time.Second)
	for !a.shaper.Begin() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.shaper.Begin() {
		b.Skip("no usable font index on this machine")
	}

	a.paint()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.hovered = i % 18
		a.paint()
	}
}

func BenchmarkPaintNarrow(b *testing.B) {
	a := benchApp(60, 24, 200, 400)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.paint()
	}
}
