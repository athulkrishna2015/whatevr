package ui

import (
	"fmt"
	"strings"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/view"
)

// syncSlot is the header's second slot: a history sync that is running. a
// stalled one is a problem and shows in the first slot
func (a *App) syncSlot() string {
	s, ok := a.syncs.Value()
	if !ok || syncPhase(s) != v2.SyncPhase_SYNC_PHASE_RUNNING {
		return ""
	}
	return fmt.Sprintf("syncing %d%%", s.GetPercent())
}

// topProblem is the first problem in the daemon's order, most severe first
func (a *App) topProblem() (*v2.ProblemRow, bool) {
	var top *v2.ProblemRow
	a.problems.Read(func(items []view.Item[*v2.ProblemRow], _ view.State) {
		if len(items) > 0 {
			top = items[0].Value
		}
	})
	return top, top != nil
}

func syncPhase(s *v2.SyncRow) v2.SyncPhase {
	p := s.GetPhase()
	if _, ok := v2.SyncPhase_name[int32(p)]; !ok {
		return v2.SyncPhase_SYNC_PHASE_UNSPECIFIED
	}
	return p
}

// enumWord turns CONNECTION_STATE_NEED_LOGIN into "need login"
func enumWord(name, prefix string) string {
	w := strings.ToLower(strings.TrimPrefix(name, prefix))
	if w == "unspecified" || w == "" {
		return "unknown"
	}
	return strings.ReplaceAll(w, "_", " ")
}

// retryIn is the countdown to a retry, empty when there is none to wait for
func retryIn(atMs int64, now time.Time) string {
	if atMs == 0 {
		return ""
	}
	d := time.UnixMilli(atMs).Sub(now)
	if d <= 0 {
		return "retrying"
	}
	return fmt.Sprintf("retry in %d s", int(d.Round(time.Second)/time.Second))
}

// statusChoices is the /status panel: the connection, the sync, then every
// problem in the order the daemon sent them, and whether any of it counts down.
// read outside App.mu
func (a *App) statusChoices(now time.Time) ([]modalChoice, bool) {
	a.mu.Lock()
	ready := a.transport == proto.Ready
	a.mu.Unlock()

	var out []modalChoice
	ticking := false
	conn := "unknown"
	if !ready {
		conn, _, _ = a.status()
	} else if c, ok := a.conn.Value(); ok {
		state := connState(c)
		conn = enumWord(state.String(), "CONNECTION_STATE_")
		if since := c.GetSinceMs(); since > 0 {
			conn += " since " + time.UnixMilli(since).Format("15:04")
		}
		if r := retryIn(c.GetNextRetryMs(), now); r != "" && state != v2.ConnectionState_CONNECTION_STATE_ONLINE {
			conn += ", " + r
			ticking = true
		}
		if n := c.GetPendingOutgoing(); n > 0 {
			conn += ", " + plural(int(n), "send") + " waiting"
		}
	}
	out = append(out, modalChoice{Label: "connection", Detail: conn})

	if s, ok := a.syncs.Value(); ok {
		detail := enumWord(syncPhase(s).String(), "SYNC_PHASE_")
		if syncPhase(s) == v2.SyncPhase_SYNC_PHASE_RUNNING {
			detail += fmt.Sprintf(" %d%%", s.GetPercent())
		}
		if n := s.GetChunk(); n > 0 {
			detail += fmt.Sprintf(", chunk %d", n)
		}
		label := "sync"
		if t := s.GetType(); t != v2.SyncType_SYNC_TYPE_UNSPECIFIED {
			if _, known := v2.SyncType_name[int32(t)]; known {
				label += " " + enumWord(t.String(), "SYNC_TYPE_")
			}
		}
		out = append(out, modalChoice{Label: label, Detail: detail})
	}

	a.problems.Read(func(items []view.Item[*v2.ProblemRow], _ view.State) {
		for _, it := range items {
			line := it.Value.GetText()
			if r := retryIn(it.Value.GetNextRetryMs(), now); r != "" {
				line += ", " + r
				ticking = true
			}
			out = append(out, modalChoice{Label: line})
		}
	})
	return out, ticking
}

// openStatus opens the /status panel
func (a *App) openStatus() { a.openModal(modalStatus) }

// overStatus is whether the pointer is on the header's summary
func (a *App) overStatus(m vaxis.Mouse) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return inRect(m, a.statusAt)
}

// hoverStatus records the pointer being on the summary, true when that changed
func (a *App) hoverStatus(on bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	changed := a.statusHovered != on
	a.statusHovered = on
	return changed
}
