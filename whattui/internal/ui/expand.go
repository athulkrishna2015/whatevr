package ui

import (
	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// expansion is the whole text of a row the daemon cut, held against that row:
// a newer row for the message is a different pointer and drops it
type expansion struct {
	row  *v2.MessageRow
	text string
}

// textOf is the words to draw and copy: the whole of them once /expand fetched
// them for this very row, the row's own otherwise
func (a *App) textOf(m *v2.MessageRow) string {
	if text, ok := a.expandedOf(m); ok {
		return text
	}
	return m.GetText()
}

func (a *App) expandedOf(m *v2.MessageRow) (string, bool) {
	if !m.GetTextTruncated() {
		return "", false
	}
	a.expandMu.Lock()
	defer a.expandMu.Unlock()
	e, ok := a.expanded[m.GetId()]
	if !ok || e.row != m {
		return "", false
	}
	return e.text, true
}

// expandGeneration moves whenever an expansion lands, so the transcript lays
// out again
func (a *App) expandGeneration() uint64 {
	a.expandMu.Lock()
	defer a.expandMu.Unlock()
	return a.expandGen
}

// pruneExpanded drops expansions whose row changed or left the window
func (a *App) pruneExpanded(items []view.Item[*v2.MessageRow]) {
	a.expandMu.Lock()
	defer a.expandMu.Unlock()
	if len(a.expanded) == 0 {
		return
	}
	live := make(map[*v2.MessageRow]bool, len(a.expanded))
	for _, it := range items {
		if e, ok := a.expanded[it.ID]; ok && e.row == it.Value {
			live[it.Value] = true
		}
	}
	for id, e := range a.expanded {
		if !live[e.row] {
			delete(a.expanded, id)
		}
	}
}

// wholeText hands then the message's whole text, asking the daemon first when
// the row came cut and nothing has fetched the rest yet
func (a *App) wholeText(m *v2.MessageRow, then func(text string)) {
	if !m.GetTextTruncated() {
		then(m.GetText())
		return
	}
	if text, ok := a.expandedOf(m); ok {
		then(text)
		return
	}
	request := a.request
	if request == nil {
		return
	}
	req := &v2.Request{}
	req.SetMessageText(v2.MessageText_builder{MessageId: m.GetId()}.Build())
	request(req, func(resp *v2.Response, err *proto.Error) {
		if err != nil {
			a.refuse(err.Message)
			return
		}
		then(resp.GetMessageText().GetText())
	})
}

// expandSelected shows all of a message the daemon cut short
func (a *App) expandSelected() {
	m, ok := a.selectedMessage()
	if !ok {
		return
	}
	a.wholeText(m, func(text string) {
		a.expandMu.Lock()
		if a.expanded == nil {
			a.expanded = map[string]expansion{}
		}
		a.expanded[m.GetId()] = expansion{row: m, text: text}
		a.expandGen++
		a.expandMu.Unlock()
		a.vx.PostEvent(redraw{})
	})
}
