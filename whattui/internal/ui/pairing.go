package ui

import (
	"fmt"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"
	"rsc.io/qr"

	"whattui/internal/paint"
	"whattui/internal/proto"
	"whattui/internal/term"
)

// qrQuiet is the blank border the qr spec asks for, in modules.
const qrQuiet = 4

// qrModules is the grid both drawings read, quiet zone included.
func qrModules(text string) (int, []bool, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return 0, nil, err
	}
	n := code.Size + 2*qrQuiet
	dark := make([]bool, n*n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dark[y*n+x] = code.Black(x-qrQuiet, y-qrQuiet)
		}
	}
	return n, dark, nil
}

var pairingSteps = []string{
	"on your phone: WhatsApp > Linked devices > Link a device,",
	"then point it at this code.",
}

// pairing is the daemon waiting for a phone. the whole window is the code
// then: every pane around it is about an account that is not there yet.
func (a *App) pairing() bool {
	a.mu.Lock()
	ready := a.transport == proto.Ready
	a.mu.Unlock()
	if !ready {
		return false
	}
	c, ok := a.conn.Value()
	return ok && connState(c) == v2.ConnectionState_CONNECTION_STATE_NEED_LOGIN
}

// drawPairing fills pane with the code, or with why it cannot.
func (a *App) drawPairing(pane vaxis.Window) {
	login, _ := a.login.Value()
	fill(pane, a.theme.Background)
	w, h := pane.Size()

	const title = "link a phone"
	if login.GetQr() == "" {
		body := []string{"waiting for whatevrd to hand over a code."}
		if d := login.GetDetail(); d != "" {
			body = append(body, "", d)
		}
		a.drawNotice(pane, title, a.theme.Warning, body)
		return
	}
	n, dark, err := qrModules(login.GetQr())
	if err != nil {
		a.drawNotice(pane, title, a.theme.Error, []string{
			"the code would not encode: " + err.Error(),
			"", "run this in a terminal instead:", "", "  whatevrd pair",
		})
		return
	}

	// title, a blank row, the code, a blank row, the steps
	rows := (n + 1) / 2
	need := 2 + rows + 1 + len(pairingSteps)
	if w < n+2 || h < need {
		a.drawNotice(pane, title, a.theme.Warning, []string{
			fmt.Sprintf("the code needs %d by %d cells and this window is %d by %d.", n+2, need, w, h),
			"make it bigger, or run this in another terminal:",
			"", "  whatevrd pair",
		})
		return
	}

	top := (h - need) / 2
	a.print(pane, (w-a.width(title))/2, top, vaxis.Style{Foreground: a.theme.Text, Attribute: vaxis.AttrBold}, title)
	left, qrTop := (w-n)/2, top+2
	if a.painted() {
		a.paintRect(pane, left, qrTop, n, rows, func(pw, ph int) paint.Spec {
			return paint.QR{W: pw, H: ph, N: n, Dark: dark, Code: login.GetQr()}
		})
	} else {
		a.drawQRCells(pane, left, qrTop, n, dark)
	}
	for i, line := range pairingSteps {
		a.print(pane, maxInt((w-a.width(line))/2, 0), qrTop+rows+1+i,
			vaxis.Style{Foreground: a.theme.TextMuted}, a.clip(line, w))
	}
}

// drawQRCells is the code in half blocks, two modules to a cell, so it claims
// exactly the cells the painted one does.
func (a *App) drawQRCells(pane vaxis.Window, left, top, n int, dark []bool) {
	ink, ground := vaxis.RGBColor(0, 0, 0), vaxis.RGBColor(0xff, 0xff, 0xff)
	if a.caps.Tier < term.TierColor {
		ink, ground = vaxis.IndexColor(0), vaxis.IndexColor(15)
	}
	at := func(x, y int) bool { return y < n && dark[y*n+x] }
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			g := " "
			switch t, b := at(x, y), at(x, y+1); {
			case t && b:
				g = "█"
			case t:
				g = "▀"
			case b:
				g = "▄"
			}
			pane.SetCell(left+x, top+y/2, vaxis.Cell{
				Character: vaxis.Character{Grapheme: g, Width: 1},
				Style:     vaxis.Style{Foreground: ink, Background: ground},
			})
		}
	}
}
