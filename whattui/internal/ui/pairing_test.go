package ui

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"

	"whattui/internal/paint"
	"whattui/internal/proto"
	"whattui/internal/term"
)

// about as long as whatsmeow's real pairing text, so the code is the real size
var pairingCode = "https://wa.me/settings/linked_devices#2@" + strings.Repeat("r", 88) + "," +
	strings.Repeat("n", 44) + "," + strings.Repeat("i", 44) + "," + strings.Repeat("a", 44) + ",1"

func unpaired(a *App, code string) {
	a.conn.Upsert("", mustJSON(proto.Connection{State: "need_login"}))
	login := proto.Login{State: "need_login"}
	if code != "" {
		login.QR = &proto.LoginQR{Code: code}
	}
	a.login.Upsert("", mustJSON(login))
}

func screenText(a *App) string {
	cols, rows := a.vx.Window().Size()
	var b strings.Builder
	for row := 0; row < rows; row++ {
		b.WriteString(rowText(a, row, cols))
		b.WriteByte('\n')
	}
	return b.String()
}

// qrOrigin is the top left cell of the half-block code, found by its ground.
func qrOrigin(t *testing.T, a *App) (int, int) {
	t.Helper()
	cols, rows := a.vx.Window().Size()
	white := vaxis.RGBColor(0xff, 0xff, 0xff)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			if a.vx.Cell(col, row).Background == white {
				return col, row
			}
		}
	}
	t.Fatalf("no code on screen:\n%s", screenText(a))
	return 0, 0
}

func TestThePairingCodeIsEveryModuleInHalfBlocks(t *testing.T) {
	a := tierApp(120, 40, term.TierColor)
	unpaired(a, pairingCode)
	a.paint()

	n, dark, err := qrModules(pairingCode)
	if err != nil {
		t.Fatal(err)
	}
	left, top := qrOrigin(t, a)
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			c := a.vx.Cell(left+x, top+y/2)
			up := c.Grapheme == "█" || c.Grapheme == "▀"
			down := c.Grapheme == "█" || c.Grapheme == "▄"
			if up != dark[y*n+x] || (y+1 < n && down != dark[(y+1)*n+x]) {
				t.Fatalf("module column %d rows %d-%d drawn %q", x, y, y+1, c.Grapheme)
			}
			if c.Foreground != vaxis.RGBColor(0, 0, 0) || c.Background != vaxis.RGBColor(0xff, 0xff, 0xff) {
				t.Fatalf("cell %d,%d is not dark on light: %+v", left+x, top+y/2, c.Style)
			}
		}
	}
	if s := screenText(a); !strings.Contains(s, "link a phone") || !strings.Contains(s, "Link a device") {
		t.Errorf("the code has no words around it:\n%s", s)
	}
}

// a default sized terminal has to fit the code; that is why it takes the window
func TestThePairingCodeFitsADefaultTerminal(t *testing.T) {
	a := tierApp(120, 40, term.TierColor)
	unpaired(a, pairingCode)
	a.paint()
	if strings.Contains(screenText(a), "whatevrd pair") {
		t.Fatalf("120x40 fell back to whatevrd pair:\n%s", screenText(a))
	}
}

// geometry is the same at every tier: the painted code claims the half-block cells
func TestThePaintedCodeClaimsTheCellsTheBlocksDo(t *testing.T) {
	cells := tierApp(120, 40, term.TierColor)
	unpaired(cells, pairingCode)
	cells.paint()
	left, top := qrOrigin(t, cells)

	a := tierApp(120, 40, term.TierGraphics)
	unpaired(a, pairingCode)
	a.paint()
	n, _, _ := qrModules(pairingCode)
	var found []surface
	for _, s := range a.surfaces {
		if _, ok := s.spec.(paint.QR); ok {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d painted codes, want 1", len(found))
	}
	s := found[0]
	if s.col != left || s.row != top || s.w != n || s.h != (n+1)/2 {
		t.Errorf("painted at %d,%d %dx%d, the blocks are at %d,%d %dx%d",
			s.col, s.row, s.w, s.h, left, top, n, (n+1)/2)
	}
	if strings.ContainsAny(screenText(a), "▀▄█") {
		t.Error("the graphics tier drew blocks under its picture")
	}
}

func TestATooSmallWindowPointsAtWhatevrdPair(t *testing.T) {
	a := tierApp(80, 24, term.TierColor)
	unpaired(a, pairingCode)
	a.paint()
	s := screenText(a)
	if !strings.Contains(s, "whatevrd pair") {
		t.Errorf("no way out offered:\n%s", s)
	}
	if strings.ContainsAny(s, "▀▄█") {
		t.Error("drew a code that cannot fit")
	}
}

func TestNoCodeYetSaysItIsWaiting(t *testing.T) {
	a := tierApp(120, 40, term.TierColor)
	unpaired(a, "")
	a.paint()
	if !strings.Contains(screenText(a), "waiting for whatevrd") {
		t.Errorf("says nothing while the code is on its way:\n%s", screenText(a))
	}
}

// the panes are gone, so a key or a click must not reach one
func TestNothingTypedWhilePairingLandsInAPane(t *testing.T) {
	a := tierApp(120, 40, term.TierColor)
	unpaired(a, pairingCode)
	a.mu.Lock()
	a.focus = FocusComposer
	a.mu.Unlock()
	a.onKey(vaxis.Key{Keycode: 'h', Text: "h"})
	if got := a.composer.String(); got != "" {
		t.Errorf("typing reached the hidden composer: %q", got)
	}
	// a draft left from before the phone unlinked must not go anywhere
	a.composer.insert("left over")
	var sent []string
	a.request = func(method string, _ proto.Params, _ proto.ResponseFunc) { sent = append(sent, method) }
	a.onKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if len(sent) != 0 || a.composer.String() != "left over" {
		t.Errorf("enter sent %v from a hidden composer, draft now %q", sent, a.composer.String())
	}
	if a.onMouse(vaxis.Mouse{Col: 2, Row: 2, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress}) {
		t.Error("a click on the pairing screen changed something")
	}
}

func TestLinkingPutsTheWindowBack(t *testing.T) {
	a := tierApp(120, 40, term.TierColor)
	unpaired(a, pairingCode)
	a.paint()
	a.conn.Upsert("", mustJSON(proto.Connection{State: "online"}))
	a.paint()
	s := screenText(a)
	if strings.Contains(s, "link a phone") || !strings.Contains(s, "contact 0") {
		t.Errorf("still pairing after the phone linked:\n%s", s)
	}
}
