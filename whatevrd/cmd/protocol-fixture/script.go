package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"whatevrd/internal/protocol"
)

// A recorded stream: one frame per line, as scripts/record-stream writes them.
// Only what a replay needs is kept. `view` names which view the frame belongs
// to; params are matched loosely (a chat_id, if the recording had one) so a
// replay of a transcript answers for the chat it was recorded from.
type scriptFrame struct {
	View    string          `json:"view"`
	ChatID  string          `json:"chat_id,omitempty"`
	Event   string          `json:"event"`
	Sort    string          `json:"sort,omitempty"`
	Item    json.RawMessage `json:"item,omitempty"`
	ID      string          `json:"id,omitempty"`
	Exhaust bool            `json:"exhausted,omitempty"`
	Anchor  string          `json:"anchor_id,omitempty"`
}

type scriptKey struct {
	view   string
	chatID string
}

// script holds the recorded frames grouped by the window they came from.
type script struct {
	windows map[scriptKey][]scriptFrame
	views   []string
}

func loadScript(path string) (*script, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	s := &script{windows: map[scriptKey][]scriptFrame{}}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var frame scriptFrame
		if err := json.Unmarshal([]byte(text), &frame); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if frame.View == "" {
			return nil, fmt.Errorf("%s:%d: frame has no view", path, line)
		}
		key := scriptKey{view: frame.View, chatID: frame.ChatID}
		s.windows[key] = append(s.windows[key], frame)
		if !seen[frame.View] {
			seen[frame.View] = true
			s.views = append(s.views, frame.View)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(s.windows) == 0 {
		return nil, fmt.Errorf("%s holds no frames", path)
	}
	return s, nil
}

// scriptedView replays one view's recorded frames. It is a full window session:
// the recording is the whole local history, and the window walks it, so a
// replay exercises the same extend/ready path a live view does.
type scriptedView struct {
	script *script
	faults *faultSet
}

func (v scriptedView) Open(params json.RawMessage, _ func()) (protocol.ViewSession, map[string]any, *protocol.Error) {
	var p struct {
		View   string `json:"view"`
		ChatID string `json:"chat_id"`
		Limit  int    `json:"limit"`
	}
	_ = json.Unmarshal(params, &p)

	frames, ok := v.script.windows[scriptKey{view: p.View, chatID: p.ChatID}]
	if !ok {
		// A recording of one chat still answers for a view opened without one.
		frames = v.script.windows[scriptKey{view: p.View}]
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	session := &scriptedSession{frames: frames, window: limit, faults: v.faults}

	meta := map[string]any{"replay": true}
	for _, frame := range frames {
		if frame.Anchor != "" {
			meta["anchor_id"] = frame.Anchor
			break
		}
	}
	return session, meta, nil
}

type scriptedSession struct {
	mu     sync.Mutex
	frames []scriptFrame
	window int
	faults *faultSet
}

func (s *scriptedSession) Items(max int) []protocol.Item {
	items, _ := s.ItemsErr(max)
	return items
}

func (s *scriptedSession) ItemsErr(max int) ([]protocol.Item, error) {
	if s.faults.fires(faultStoreError) {
		return nil, errors.New("fault: the store refused this read")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	limit := s.window
	if max > 0 && max < limit {
		limit = max
	}
	items := make([]protocol.Item, 0, limit)
	for _, frame := range s.frames {
		if frame.Event != "upsert" || len(items) >= limit {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(frame.Item, &data); err != nil {
			continue
		}
		id, _ := data["id"].(string)
		if id == "" {
			id = frame.ID
		}
		items = append(items, protocol.Item{ID: id, Sort: frame.Sort, Data: data})
	}
	return items, nil
}

func (s *scriptedSession) ExtendWindow(direction string, count int) {
	if direction != "older" || count <= 0 {
		return
	}
	s.mu.Lock()
	s.window += count
	s.mu.Unlock()
}

func (s *scriptedSession) Exhausted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	upserts := 0
	for _, frame := range s.frames {
		if frame.Event == "upsert" {
			upserts++
		}
	}
	return s.window >= upserts
}

func (s *scriptedSession) Close() {}

// ---------------------------------------------------------------- faults

type faultKind string

const (
	// Every view read fails. Exercises the FallibleSession path: an open
	// transcript must keep its rows rather than be wiped by one bad read.
	faultStoreError faultKind = "store-error"
	// The connection is dropped under the client, the way a daemon restart
	// does it.
	faultDropSocket faultKind = "drop-socket"
	// Connection state published out of order: a Disconnected behind a
	// Connected, which is what a flapping link actually delivers.
	faultReorderConnection faultKind = "reorder-connection"
	// The daemon stops publishing for a while and then resumes, the way a
	// laptop suspend looks from the socket.
	faultSleepResume faultKind = "sleep-resume"
)

var allFaults = []faultKind{faultStoreError, faultDropSocket, faultReorderConnection, faultSleepResume}

// faultSet is which faults are armed and how often each one fires. A fault that
// fires every time is not a fault, it is a broken build; the point is to make a
// rare path common enough to hit in a soak without making the ordinary path
// impossible to observe.
type faultSet struct {
	mu      sync.Mutex
	armed   map[faultKind]int // one in N reads
	counter map[faultKind]int
}

func parseFaults(spec string) (*faultSet, error) {
	set := &faultSet{armed: map[faultKind]int{}, counter: map[faultKind]int{}}
	if strings.TrimSpace(spec) == "" {
		return set, nil
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, rate := part, 5
		if colon := strings.IndexByte(part, ':'); colon >= 0 {
			name = part[:colon]
			if _, err := fmt.Sscanf(part[colon+1:], "%d", &rate); err != nil || rate < 1 {
				return nil, fmt.Errorf("fault %q: rate must be a positive integer", part)
			}
		}
		if name == "all" {
			for _, kind := range allFaults {
				set.armed[kind] = rate
			}
			continue
		}
		known := false
		for _, kind := range allFaults {
			if string(kind) == name {
				set.armed[kind] = rate
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown fault %q", name)
		}
	}
	return set, nil
}

func (f *faultSet) enabled(kind faultKind) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.armed[kind] > 0
}

// fires reports whether this call is the one that trips. Deterministic (every
// Nth), not random: a soak failure has to be reproducible from the log.
func (f *faultSet) fires(kind faultKind) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rate := f.armed[kind]
	if rate <= 0 {
		return false
	}
	f.counter[kind]++
	return f.counter[kind]%rate == 0
}

func (f *faultSet) sleepIfArmed() {
	if f.fires(faultSleepResume) {
		time.Sleep(250 * time.Millisecond)
	}
}
