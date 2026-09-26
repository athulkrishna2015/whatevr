package ui

import (
	"fmt"
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
)

// whattui never clears the screen: the panes tile it between them, so clearing
// first would be a write to every cell that every pane is about to write
// again. That is only true if they really do tile it. Any cell no pane writes
// keeps whatever the terminal had there, which after a resize is somebody
// else's frame.
func TestEveryCellBelongsToAPane(t *testing.T) {
	t.Parallel()
	sizes := [][2]int{
		{120, 40}, {113, 45}, {107, 41}, {100, 30}, {99, 30}, {93, 49},
		{80, 24}, {68, 24}, {67, 24}, {60, 24}, {40, 20}, {39, 20}, {38, 14},
	}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a := stubApp(size[0], size[1], 6, 8)
			sentinel := vaxis.Cell{
				Character: vaxis.Character{Grapheme: "·", Width: 1},
				Style:     vaxis.Style{Foreground: vaxis.RGBColor(0xff, 0x00, 0xff)},
			}
			a.vx.Window().Fill(sentinel)
			a.paint()

			for row := 0; row < size[1]; row++ {
				for col := 0; col < size[0]; col++ {
					if got := a.vx.Cell(col, row); got.Grapheme == sentinel.Grapheme {
						t.Fatalf("cell %d,%d was not drawn by any pane", col, row)
					}
				}
			}
		})
	}
}

// The same, in the states the frame can actually be in, and across the resize
// that leaves a frame drawn for another size on the screen.
func TestEveryCellBelongsToAPaneInEveryState(t *testing.T) {
	sentinel := vaxis.Cell{
		Character: vaxis.Character{Grapheme: "\u00b7", Width: 1},
		Style:     vaxis.Style{Foreground: vaxis.RGBColor(0xff, 0x00, 0xff)},
	}
	bare := func(a *App) {
		a.mu.Lock()
		a.conversation, a.activeChat = nil, ""
		a.mu.Unlock()
	}

	for name, setup := range map[string]func(*App){
		"no chat open":    bare,
		"no chats at all": func(a *App) { bare(a); a.chats.Reset() },
		"a notice":        func(a *App) { a.transport = proto.Connecting },
		"a modal":         func(a *App) { a.openModal(modalPalette) },
		"the list focused": func(a *App) {
			a.mu.Lock()
			a.focus = FocusList
			a.mu.Unlock()
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := stubApp(113, 45, 6, 8)
			setup(a)
			a.paint()

			// Now the resize: the screen holds a frame drawn for another size.
			a.handle(vaxisResize(107, 41))
			a.vx.Window().Fill(sentinel)
			a.paint()

			for row := 0; row < 41; row++ {
				for col := 0; col < 107; col++ {
					if got := a.vx.Cell(col, row); got.Grapheme == sentinel.Grapheme {
						t.Fatalf("cell %d,%d was not drawn by any pane", col, row)
					}
				}
			}
		})
	}
}
