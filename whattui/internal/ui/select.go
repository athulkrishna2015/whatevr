package ui

import (
	"strings"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
)

// Selection is the terminal gesture, not a whattui one: drag to select, and
// what you selected is on the clipboard when you let go. Double click takes
// the word, triple click takes the block it is in.
//
// It reads the screen back rather than the model, so it works over every pane
// without any pane knowing about it, and what you get is exactly what you saw.
// The one thing the screen cannot tell us is a rasterised word, whose cells
// are blank by design, so those are remembered as they are drawn and put back
// here.

// point is a cell on the screen, absolute.
type point struct{ col, row int }

func (p point) before(q point) bool {
	return p.row < q.row || (p.row == q.row && p.col < q.col)
}

// selection is a flowing range of cells, the way a terminal selects: from one
// cell to another in reading order, clamped to the columns of the region the
// drag started in. Bounded to a region because whattui is two panes side by
// side, and a selection that ran across the divider would cut every line in
// half with the chat list.
type selection struct {
	on     bool
	bounds layout.Rect
	from   point
	to     point
}

// drag is a press that has not been let go of yet.
type drag struct {
	down  bool
	moved bool
	// chat is the row the press landed on, opened on release if the press
	// never turned into a drag. A click that opens on the way down is a click
	// that cannot be the start of a selection.
	chat int

	// clicks counts a burst, for the double and triple gestures.
	clicks int
	at     point
	when   time.Time
}

// clickBurst is how long a second click still counts as part of the first.
const clickBurst = 400 * time.Millisecond

// blocks are the runs of text a triple click can take whole: a message's
// words, a chat row, the composer. Recorded as they are drawn, because the
// thing that knows where a message's text landed is the thing that put it
// there.
func (a *App) noteBlock(win vaxis.Window, col, row, w, h int) {
	// Clamped to the window, because a message taller than the pane hangs off
	// both ends of it and an unclamped block would claim rows belonging to
	// whatever is drawn below.
	winW, winH := win.Size()
	if row < 0 {
		h, row = h+row, 0
	}
	if col < 0 {
		w, col = w+col, 0
	}
	h = minInt(h, winH-row)
	w = minInt(w, winW-col)
	if w <= 0 || h <= 0 {
		return
	}
	ox, oy := win.Origin()
	a.blocks = append(a.blocks, layout.Rect{Col: ox + col, Row: oy + row, Width: w, Height: h})
}

// blockAt is the block under a point, or the region it fell in.
func (a *App) blockAt(p point) (layout.Rect, bool) {
	for _, b := range a.blocks {
		if p.col >= b.Col && p.col < b.Col+b.Width && p.row >= b.Row && p.row < b.Row+b.Height {
			return b, true
		}
	}
	return layout.Rect{}, false
}

// onSelectPress handles a left press, and reports whether it consumed it. A
// double or triple click is a selection and nothing else; a single press is
// only ever the start of one, so whatever the click would otherwise do waits
// for the release.
func (a *App) onSelectPress(p point) bool {
	now := time.Now()
	a.mu.Lock()
	burst := a.drag.at == p && now.Sub(a.drag.when) < clickBurst
	if burst {
		a.drag.clicks++
	} else {
		a.drag.clicks = 1
	}
	a.drag.at, a.drag.when = p, now
	clicks := a.drag.clicks
	a.mu.Unlock()

	switch clicks {
	case 2:
		return a.selectWord(p)
	case 3:
		return a.selectBlock(p)
	}

	// Bounded to the block the press landed in. A drag that started inside a
	// message stays inside it: running through the bubble's own walls and
	// into the one below would copy the frame along with the words, which is
	// not what anybody is pointing at. And a press on the ground between two
	// messages selects nothing, because there is nothing there to select.
	a.clearSelection()
	bounds, ok := a.blockAt(p)
	if !ok {
		return false
	}

	a.mu.Lock()
	a.drag.down = true
	a.drag.moved = false
	a.sel = selection{bounds: bounds, from: p, to: p}
	a.mu.Unlock()
	return false
}

// onSelectMotion extends a live drag.
func (a *App) onSelectMotion(p point) bool {
	a.mu.Lock()
	if !a.drag.down {
		a.mu.Unlock()
		return false
	}
	if p != a.sel.from {
		a.drag.moved = true
		a.sel.on = true
	}
	a.sel.to = clampTo(p, a.sel.bounds)
	on := a.sel.on
	a.mu.Unlock()
	return on
}

// onSelectRelease ends a drag and reports whether it was one. A press that
// never moved was a click, and the caller still owes it its action.
func (a *App) onSelectRelease() bool {
	a.mu.Lock()
	moved := a.drag.down && a.drag.moved
	a.drag.down = false
	a.mu.Unlock()
	if moved {
		a.copySelection()
	}
	return moved
}

// selectWord takes the run of word characters around a point. Border glyphs
// are not word characters, so a double click inside a bubble stops at its
// walls rather than swallowing them.
// clampTo keeps a drag inside the block it started in.
func clampTo(p point, r layout.Rect) point {
	p.col = minInt(maxInt(p.col, r.Col), r.Col+r.Width-1)
	p.row = minInt(maxInt(p.row, r.Row), r.Row+r.Height-1)
	return p
}

func (a *App) selectWord(p point) bool {
	bounds, ok := a.blockAt(p)
	if !ok {
		return false
	}
	if !wordish(a.grapheme(p)) {
		return false
	}
	from, to := p, p
	for from.col > bounds.Col && wordish(a.grapheme(point{from.col - 1, from.row})) {
		from.col--
	}
	for to.col < bounds.Col+bounds.Width-1 && wordish(a.grapheme(point{to.col + 1, to.row})) {
		to.col++
	}
	a.setSelection2(selection{on: true, bounds: bounds, from: from, to: to})
	a.copySelection()
	return true
}

// selectBlock takes a whole message, chat row or draft.
func (a *App) selectBlock(p point) bool {
	b, ok := a.blockAt(p)
	if !ok {
		return false
	}
	a.setSelection2(selection{
		on:     true,
		bounds: b,
		from:   point{b.Col, b.Row},
		to:     point{b.Col + b.Width - 1, b.Row + b.Height - 1},
	})
	a.copySelection()
	return true
}

func (a *App) setSelection2(s selection) {
	a.mu.Lock()
	a.sel = s
	a.mu.Unlock()
}

// clearSelection drops the highlight and reports whether there was one.
func (a *App) clearSelection() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	had := a.sel.on
	a.sel = selection{}
	return had
}

func wordish(g string) bool {
	if g == "" || strings.TrimSpace(g) == "" {
		return false
	}
	return !strings.ContainsAny(g, "│─╭╮╰╯▎┃")
}

// cellText is what a column says and how many columns that text covers.
//
// The columns a rasterised word occupies are blank by design, so the screen
// cannot answer for them and the run that drew them does instead. Answering
// per cluster rather than per run is what makes half of a devanagari phrase
// selectable: a cluster is the smallest thing that is still text.
func (a *App) cellText(placed []placement, p point) (string, int) {
	if r := runAt(placed, p.col, p.row); r != nil {
		text, from, span := r.run.ClusterAt(p.col - r.col)
		// A selection can start in the middle of a cluster. It still gets the
		// whole cluster, because half of one is not text, but the walk has to
		// carry on from the column it was asked about and not from the
		// cluster's own start.
		if lead := p.col - r.col - from; lead > 0 {
			span -= lead
		}
		return text, maxInt(span, 1)
	}
	c := a.vx.Cell(p.col, p.row)
	if c.Grapheme == "" {
		return "", 1
	}
	return c.Grapheme, maxInt(int(c.Width), 1)
}

func (a *App) grapheme(p point) string {
	a.mu.Lock()
	placed := a.placements
	a.mu.Unlock()
	text, _ := a.cellText(placed, p)
	return text
}

// rowRange is the columns of one row that the selection covers.
func (s selection) rowRange(row int) (int, int, bool) {
	from, to := s.from, s.to
	if to.before(from) {
		from, to = to, from
	}
	if row < from.row || row > to.row {
		return 0, 0, false
	}
	lo, hi := s.bounds.Col, s.bounds.Col+s.bounds.Width-1
	if row == from.row {
		lo = maxInt(lo, from.col)
	}
	if row == to.row {
		hi = minInt(hi, to.col)
	}
	return lo, hi, lo <= hi
}

func (s selection) rows() (int, int) {
	from, to := s.from, s.to
	if to.before(from) {
		from, to = to, from
	}
	return from.row, to.row
}

// paintSelection recolours the selected cells, after every pane has drawn and
// before any of it goes out. A cell that owns a scaled grapheme is left alone:
// rewriting it would give back the rows it reserved and the glyph with them.
func (a *App) paintSelection() {
	a.mu.Lock()
	s := a.sel
	a.mu.Unlock()
	if !s.on {
		return
	}
	win := a.vx.Window()
	top, bottom := s.rows()
	for row := top; row <= bottom; row++ {
		lo, hi, ok := s.rowRange(row)
		if !ok {
			continue
		}
		for col := lo; col <= hi; col++ {
			c := a.vx.Cell(col, row)
			if c.Size != 0 {
				continue
			}
			if a.caps.Tier < 1 {
				c.Style.Attribute |= vaxis.AttrReverse
			} else {
				c.Style.Background = a.theme.Selection
				c.Style.Foreground = a.theme.Text
			}
			win.SetCell(col, row, c)
		}
	}
}

// selectedText reads the selection back off the screen, putting the text of
// every rasterised word back where its blank cells are.
func (a *App) selectedText() string {
	a.mu.Lock()
	s := a.sel
	placed := a.placements
	a.mu.Unlock()
	if !s.on {
		return ""
	}

	var b strings.Builder
	top, bottom := s.rows()
	for row := top; row <= bottom; row++ {
		lo, hi, ok := s.rowRange(row)
		if !ok {
			continue
		}
		var out strings.Builder
		for col := lo; col <= hi; {
			text, span := a.cellText(placed, point{col, row})
			// An empty cell is the continuation of something wide, whose
			// owner already wrote the whole grapheme.
			out.WriteString(text)
			col += span
		}
		if row > top {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(out.String(), " "))
	}
	return strings.Trim(b.String(), "\n")
}

// runAt is the rasterised word covering a column.
func runAt(placed []placement, col, row int) *placement {
	for i := range placed {
		p := &placed[i]
		if p.row == row && col >= p.col && col < p.col+p.cells {
			return p
		}
	}
	return nil
}

// copySelection puts the selection on the system clipboard through OSC 52,
// which is also what makes copy work over ssh, where there is no clipboard on
// this side to put it on.
func (a *App) copySelection() {
	text := a.selectedText()
	if strings.TrimSpace(text) == "" {
		return
	}
	a.vx.ClipboardPush(text)
	a.toast("copied " + plural(len(strings.Split(text, "\n")), "line") + " to the clipboard")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}
