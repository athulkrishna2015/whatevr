package views

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/server"
)

// logTail is how many of the run log's newest lines one fill serves. the
// page re-subscribes for older history; a fill bigger than this would build
// megabytes of rows on every tick during a busy sync.
const logTail = 200

type logLine struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Module  string    `json:"module"`
}

// logsView tails the daemon's own run log, newest last like every other
// collection. the rows are a debug page, not history: reopening re-reads.
func (rs *Reads) logsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		lines, err := tailLog(rs.runLog, logTail)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for i, l := range lines {
			row := v2.LogRow_builder{
				Level: l.logLine.Level,
				Text:  l.logLine.Message,
			}.Build()
			if !l.logLine.Time.IsZero() {
				row.SetTMs(l.logLine.Time.UnixMilli())
			}
			it := &v2.Upsert{}
			it.SetId(fmt.Sprintf("l:%d", l.off))
			it.SetSort([]byte(fmt.Sprintf("%020d", i)))
			it.SetLog(row)
			out = append(out, it)
		}
		return limited(out, max), nil
	}
	// A debug page refreshes with everything else; the tail read is cheap
	// and the page only exists while open.
	w.wake = func(c core.Change) bool { return true }
	return w, nil, nil
}

// tailLog reads the last n lines of path with their byte offsets as ids.
func tailLog(path string, n int) ([]numberedLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var all []numberedLine
	var off int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		start := off
		off += int64(len(line)) + 1
		var l logLine
		if err := json.Unmarshal(line, &l); err != nil || l.Message == "" {
			continue
		}
		all = append(all, numberedLine{logLine: l, off: start})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

type numberedLine struct {
	logLine
	off int64
}
