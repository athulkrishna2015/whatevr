//go:build whatevr_mock

package wamock

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The control socket is how a test asks the mock whether it has finished
// talking. A golden frame taken while a history chunk is still on the wire is a
// different frame every run, and sleeping long enough to be safe is both slow
// and still a guess.
//
// Quiescence here is strictly the server's half: everything the scenario meant
// to send has been sent, no http request is open, and the wire has been quiet
// for a moment. The client's half (its subscriptions having emitted ready) is
// something only the frontend can see, so a caller waits on both.

// defaultIdle is how long the wire has to stay quiet before the mock calls
// itself settled. Long enough for the daemon to answer what it just received,
// short enough that a barrier is not itself the slowest part of a test.
const defaultIdle = 300 * time.Millisecond

// defaultSyncTimeout bounds a sync so a scenario with a slow timeline reports
// what it is waiting for rather than hanging the caller.
const defaultSyncTimeout = 30 * time.Second

// quiescence counts everything in flight. It is deliberately separate from the
// server's own lock: it is touched on every stanza in both directions, and
// nothing that touches it should ever wait on a scenario.
type quiescence struct {
	mu sync.Mutex
	// last is when anything last moved, in either direction.
	last time.Time
	// work is queued or executing outbox functions, http requests being served
	// and timeline actions not yet run, all counted together because a caller
	// only ever asks the one question.
	work     int
	timeline int
	http     int
	loggedIn bool
}

func newQuiescence() *quiescence { return &quiescence{last: time.Now()} }

func (q *quiescence) touch() {
	q.mu.Lock()
	q.last = time.Now()
	q.mu.Unlock()
}

func (q *quiescence) add(field *int, delta int) {
	q.mu.Lock()
	*field += delta
	q.last = time.Now()
	q.mu.Unlock()
}

func (q *quiescence) addWork(delta int)     { q.add(&q.work, delta) }
func (q *quiescence) addTimeline(delta int) { q.add(&q.timeline, delta) }
func (q *quiescence) addHTTP(delta int)     { q.add(&q.http, delta) }

func (q *quiescence) setLoggedIn() {
	q.mu.Lock()
	q.loggedIn = true
	q.last = time.Now()
	q.mu.Unlock()
}

// blockedOn names the one thing still moving, or empty when nothing is.
func (q *quiescence) blockedOn(idle time.Duration) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch {
	case !q.loggedIn:
		return "login"
	case q.work > 0:
		return fmt.Sprintf("%d queued stanzas", q.work)
	case q.timeline > 0:
		return fmt.Sprintf("%d timeline actions", q.timeline)
	case q.http > 0:
		return fmt.Sprintf("%d http requests", q.http)
	case time.Since(q.last) < idle:
		return "the wire is still busy"
	}
	return ""
}

// controlRequest is one line on the control socket.
type controlRequest struct {
	Cmd       string `json:"cmd"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	IdleMS    int    `json:"idle_ms,omitempty"`
	// say injects a live message from somebody already in the world.
	Chat string `json:"chat,omitempty"`
	From string `json:"from,omitempty"`
	Text string `json:"text,omitempty"`
}

type controlResponse struct {
	OK        bool     `json:"ok"`
	Error     string   `json:"error,omitempty"`
	BlockedOn string   `json:"blocked_on,omitempty"`
	WaitedMS  int64    `json:"waited_ms,omitempty"`
	Scenario  string   `json:"scenario,omitempty"`
	Seed      int64    `json:"seed,omitempty"`
	Now       string   `json:"now,omitempty"`
	Names     []string `json:"names,omitempty"`
	ID        string   `json:"id,omitempty"`
}

// StartControl binds the control socket. It is only ever asked for by a test
// harness; a person running a scenario has no use for it.
func (s *Server) StartControl(ctx context.Context, path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("control dir: %w", err)
	}
	// A leftover socket from a killed run would refuse the bind. Nothing else
	// is allowed to live at this path, so removing it is safe.
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("control listen: %w", err)
	}
	s.control = ln
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serveControl(conn)
		}
	}()
	s.log.Printf("control socket on %s", path)
	return nil
}

func (s *Server) serveControl(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req controlRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = encoder.Encode(controlResponse{Error: err.Error()})
			continue
		}
		if err := encoder.Encode(s.handleControl(req)); err != nil {
			return
		}
	}
}

func (s *Server) handleControl(req controlRequest) controlResponse {
	switch req.Cmd {
	case "sync":
		return s.controlSync(req)
	case "status":
		idle := durationOr(req.IdleMS, defaultIdle)
		blocked := s.quiet.blockedOn(idle)
		return controlResponse{OK: blocked == "", BlockedOn: blocked}
	case "scenario":
		return controlResponse{
			OK:       true,
			Scenario: s.opts.Scenario,
			Seed:     s.opts.Seed,
			Now:      Now().Format(time.RFC3339Nano),
		}
	case "list":
		names := make([]string, 0)
		for _, sc := range List() {
			names = append(names, sc.Name)
		}
		return controlResponse{OK: true, Names: names}
	case "say":
		return s.controlSay(req)
	default:
		return controlResponse{Error: fmt.Sprintf("unknown command %q", req.Cmd)}
	}
}

// controlSync blocks until the mock has nothing left to do, or says what it is
// still doing.
func (s *Server) controlSync(req controlRequest) controlResponse {
	idle := durationOr(req.IdleMS, defaultIdle)
	deadline := time.Now().Add(durationOr(req.TimeoutMS, defaultSyncTimeout))
	start := time.Now()
	for {
		blocked := s.quiet.blockedOn(idle)
		if blocked == "" {
			return controlResponse{OK: true, WaitedMS: time.Since(start).Milliseconds()}
		}
		if time.Now().After(deadline) {
			return controlResponse{
				BlockedOn: blocked,
				WaitedMS:  time.Since(start).Milliseconds(),
				Error:     "still busy at the deadline",
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// controlSay puts a message in a chat from outside the scenario, which is how a
// script drives a UI state the scenario did not think of.
func (s *Server) controlSay(req controlRequest) controlResponse {
	world := s.world
	if world == nil {
		return controlResponse{Error: "no world"}
	}
	chat, ok := world.chatNamed(req.Chat)
	if !ok {
		return controlResponse{Error: fmt.Sprintf("no chat named %q", req.Chat)}
	}
	from := chat.Other()
	if req.From != "" {
		person, ok := world.contactNamed(req.From)
		if !ok {
			return controlResponse{Error: fmt.Sprintf("nobody named %q", req.From)}
		}
		from = person
	}
	if from == nil {
		return controlResponse{Error: "nobody to say it"}
	}
	m := chat.Say(from, req.Text, time.Now())
	return controlResponse{OK: true, ID: m.ID}
}

func durationOr(ms int, fallback time.Duration) time.Duration {
	if ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}
