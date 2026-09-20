// Package layout turns a terminal size into geometry.
//
// Nothing here knows what tier the terminal is: a capability difference
// changes ink, never position, and the golden-frame tests hold that.
package layout

// Shape is how much room there is, which is the only thing the panes ask.
type Shape int

const (
	// ShapeRail is too narrow for names: the chat list is avatars and unread
	// dots, or it is gone and the transcript has the screen.
	ShapeRail Shape = iota
	// ShapeStack is one pane at a time, with a back affordance.
	ShapeStack
	// ShapeCompact is two panes, chat rows without their preview line.
	ShapeCompact
	// ShapeWide is two panes with everything.
	ShapeWide
)

func (s Shape) String() string {
	switch s {
	case ShapeWide:
		return "wide"
	case ShapeCompact:
		return "compact"
	case ShapeStack:
		return "stack"
	default:
		return "rail"
	}
}

// Chrome is how much furniture fits vertically.
type Chrome int

const (
	// ChromeMinimal has no room for a header: it merges into the hint line.
	ChromeMinimal Chrome = iota
	// ChromeTight drops the hint bar to a glyph cluster.
	ChromeTight
	// ChromeFull is everything.
	ChromeFull
)

// Rect is a cell rectangle. Zero width or height means the pane is not drawn.
type Rect struct {
	Col, Row, Width, Height int
}

// Empty reports whether there is nothing to draw here.
func (r Rect) Empty() bool { return r.Width <= 0 || r.Height <= 0 }

// Layout is where everything goes this frame.
type Layout struct {
	Shape  Shape
	Chrome Chrome

	ChatList   Rect
	Header     Rect
	Transcript Rect
	Composer   Rect
	HintBar    Rect

	// ListFocused is meaningful only in ShapeStack, where the two panes share
	// the screen and only one is on it.
	ListFocused bool
}

const (
	wideAt    = 100
	compactAt = 68
	stackAt   = 40

	fullChromeAt  = 20
	tightChromeAt = 12

	maxComposerRows   = 5
	minTranscriptRows = 4

	wideListWidth    = 34
	compactListWidth = 26
	railWidth        = 4
)

// Compute lays out one frame. listFocused only matters when the screen is too
// narrow to hold both panes. composerRows is how many rows the text being
// written wants; it gets them only as long as the transcript keeps enough to
// still be a transcript.
func Compute(cols, rows int, listFocused bool, composerRows int) Layout {
	l := Layout{Shape: shapeFor(cols), Chrome: chromeFor(rows), ListFocused: listFocused}

	listWidth := 0
	switch l.Shape {
	case ShapeWide:
		listWidth = wideListWidth
	case ShapeCompact:
		listWidth = compactListWidth
	case ShapeStack:
		if listFocused {
			listWidth = cols
		}
	case ShapeRail:
		listWidth = railWidth
		if listFocused {
			listWidth = cols
		}
	}
	if listWidth > cols {
		listWidth = cols
	}

	l.ChatList = Rect{Col: 0, Row: 0, Width: listWidth, Height: rows}

	// In stack shape the list is the whole screen, so there is no
	// conversation beside it.
	if l.Shape == ShapeStack && listFocused {
		return l
	}

	convCol := listWidth
	convWidth := cols - listWidth
	if convWidth <= 0 {
		return l
	}

	headerRows, hintRows := 1, 1
	if composerRows < 1 {
		composerRows = 1
	}
	if composerRows > maxComposerRows {
		composerRows = maxComposerRows
	}
	switch l.Chrome {
	case ChromeMinimal:
		headerRows = 0
	case ChromeTight:
		headerRows = 1
	}
	// The composer gives its rows back before the header and the hint bar do:
	// a long draft is worth less than being able to see what it answers.
	for composerRows > 1 && rows-headerRows-hintRows-composerRows < minTranscriptRows {
		composerRows--
	}
	if rows < headerRows+hintRows+composerRows+1 {
		headerRows, hintRows = 0, 0
	}

	row := 0
	l.Header = Rect{Col: convCol, Row: row, Width: convWidth, Height: headerRows}
	row += headerRows

	transcriptRows := rows - headerRows - hintRows - composerRows
	if transcriptRows < 0 {
		transcriptRows = 0
	}
	l.Transcript = Rect{Col: convCol, Row: row, Width: convWidth, Height: transcriptRows}
	row += transcriptRows

	l.Composer = Rect{Col: convCol, Row: row, Width: convWidth, Height: composerRows}
	row += composerRows

	l.HintBar = Rect{Col: convCol, Row: row, Width: convWidth, Height: hintRows}
	return l
}

// ChatRowHeight is how many rows one chat occupies. Wide has the room for
// what a chat row is actually for: who it is, and what they last said.
//
// One function because three callers have to agree: the one that draws the
// rows, the one that pages the list, and the one that turns a click into the
// row under the pointer. They disagreed once, and a click landing on a
// different chat than the pointer was over is the result.
func (l Layout) ChatRowHeight() int {
	if l.Shape == ShapeWide {
		return 2
	}
	return 1
}

// VisibleChats is how many chats fit in the list.
func (l Layout) VisibleChats() int {
	return l.ChatList.Height / l.ChatRowHeight()
}

// ChatAt turns a screen row into an index into the visible chats, counting
// from the one at the top of the list. Out of the pane is -1.
func (l Layout) ChatAt(row int) int {
	if l.ChatList.Empty() || row < l.ChatList.Row || row >= l.ChatList.Row+l.ChatList.Height {
		return -1
	}
	at := (row - l.ChatList.Row) / l.ChatRowHeight()
	if at >= l.VisibleChats() {
		return -1
	}
	return at
}

func shapeFor(cols int) Shape {
	switch {
	case cols >= wideAt:
		return ShapeWide
	case cols >= compactAt:
		return ShapeCompact
	case cols >= stackAt:
		return ShapeStack
	default:
		return ShapeRail
	}
}

func chromeFor(rows int) Chrome {
	switch {
	case rows >= fullChromeAt:
		return ChromeFull
	case rows >= tightChromeAt:
		return ChromeTight
	default:
		return ChromeMinimal
	}
}
