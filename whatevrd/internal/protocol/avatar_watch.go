package protocol

import (
	"sync"

	"whatevrd/internal/store"
)

// avatarWatch is whose avatar a window of message rows shows (each sender,
// and the chat), so an avatar landing wakes only the windows it is in. it
// says yes before the first read and while one is under way, as that read
// may have missed it.
type avatarWatch struct {
	mu       sync.Mutex
	subjects map[string]bool
	reads    int
}

func (a *avatarWatch) begin() {
	a.mu.Lock()
	a.reads++
	a.mu.Unlock()
}

// end closes a read; ok says rows is what the window now shows.
func (a *avatarWatch) end(chatID string, rows []store.Message, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads--
	if !ok {
		return
	}
	a.subjects = map[string]bool{chatID: true}
	for _, m := range rows {
		if m.SenderID != "" {
			a.subjects[m.SenderID] = true
		}
	}
}

func (a *avatarWatch) shows(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.subjects == nil || a.reads > 0 || a.subjects[id]
}
