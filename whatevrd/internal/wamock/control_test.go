//go:build whatevr_mock

package wamock

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	Register(Scenario{
		Name:        "test:control",
		Description: "fixture for the control socket tests",
		Build: func(w *World) {
			asha := w.Contact("917770000001", "Asha")
			chat := w.DM(asha)
			chat.History(asha, "already here", Ago(time.Hour))
			// Something scheduled, so the barrier has a reason to wait rather
			// than returning on an empty world.
			w.After(300*time.Millisecond, func() {
				chat.Say(asha, "and one after login", Now())
			})
		},
	})
}

// controlClient is one connection to the quiescence socket, one line each way.
type controlClient struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dialControl(t *testing.T, path string) *controlClient {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &controlClient{t: t, conn: conn, r: bufio.NewReader(conn)}
}

func (c *controlClient) do(req controlRequest) controlResponse {
	c.t.Helper()
	line, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("encode %s: %v", req.Cmd, err)
	}
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		c.t.Fatalf("write %s: %v", req.Cmd, err)
	}
	raw, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("read %s: %v", req.Cmd, err)
	}
	var resp controlResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		c.t.Fatalf("decode %s: %v", req.Cmd, err)
	}
	return resp
}

// TestControlSyncWaitsForTheTimeline is the barrier doing its job: a scenario
// with something scheduled is not settled until that something has happened,
// and the message it produced is on the client before sync returns.
func TestControlSyncWaitsForTheTimeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "control.sock")
	srv, _, messages := dialMock(ctx, t, Options{Seed: 5, Scenario: "test:control"})
	if err := srv.StartControl(ctx, path); err != nil {
		t.Fatalf("start control: %v", err)
	}

	control := dialControl(t, path)
	if resp := control.do(controlRequest{Cmd: "sync", TimeoutMS: 60000}); !resp.OK {
		t.Fatalf("sync = %+v, want settled", resp)
	}
	// The scheduled message was sent before sync returned, so it is already
	// waiting rather than something the test has to sit and wait for.
	select {
	case evt := <-messages:
		if got := evt.Message.GetConversation(); got != "and one after login" {
			t.Fatalf("first message = %q, want the scheduled one", got)
		}
	default:
		t.Fatal("sync returned before the timeline had been delivered")
	}

	// A second sync on a settled world is immediate, which is what makes the
	// barrier cheap enough to call before every frame.
	resp := control.do(controlRequest{Cmd: "sync", TimeoutMS: 5000})
	if !resp.OK || resp.WaitedMS > 2000 {
		t.Fatalf("second sync = %+v, want an immediate settle", resp)
	}
}

// TestControlSaysAndLists covers the rest of the surface: what this run is, what
// could have been run, and putting a message in from outside the scenario.
func TestControlSaysAndLists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	at := time.Date(2025, 9, 16, 12, 0, 0, 0, time.UTC)
	// The clock is process wide, so it goes back afterwards: the next test in
	// this package builds its world off Ago too.
	restore := Now()
	t.Cleanup(func() { SetBootTime(restore) })

	path := filepath.Join(t.TempDir(), "control.sock")
	srv, _, messages := dialMock(ctx, t, Options{Seed: 6, Scenario: "test:control", Now: at})
	if err := srv.StartControl(ctx, path); err != nil {
		t.Fatalf("start control: %v", err)
	}
	control := dialControl(t, path)

	info := control.do(controlRequest{Cmd: "scenario"})
	if !info.OK || info.Scenario != "test:control" || info.Seed != 6 {
		t.Fatalf("scenario = %+v, want the one that was asked for", info)
	}
	if got := info.Now; got != at.Format(time.RFC3339Nano) {
		t.Fatalf("now = %q, want the pinned clock %q", got, at.Format(time.RFC3339Nano))
	}

	names := control.do(controlRequest{Cmd: "list"})
	if !names.OK || len(names.Names) == 0 {
		t.Fatalf("list = %+v, want the registry", names)
	}

	control.do(controlRequest{Cmd: "sync", TimeoutMS: 60000})
	drain(messages)

	said := control.do(controlRequest{Cmd: "say", Chat: "Asha", From: "Asha", Text: "from the control socket"})
	if !said.OK || said.ID == "" {
		t.Fatalf("say = %+v, want an id", said)
	}
	evt := awaitMessage(ctx, t, messages, said.ID)
	if got := evt.Message.GetConversation(); got != "from the control socket" {
		t.Fatalf("injected message = %q", got)
	}

	if bad := control.do(controlRequest{Cmd: "say", Chat: "nobody", Text: "x"}); bad.OK || bad.Error == "" {
		t.Fatalf("say into a chat that does not exist = %+v, want an error", bad)
	}
	if bad := control.do(controlRequest{Cmd: "nonsense"}); bad.OK || bad.Error == "" {
		t.Fatalf("unknown command = %+v, want an error", bad)
	}
}

// awaitMessage waits for one message by the id the mock gave it.
func awaitMessage(ctx context.Context, t *testing.T, ch <-chan *events.Message, id string) *events.Message {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case evt := <-ch:
			if evt.Info.ID == id {
				return evt
			}
		case <-deadline:
			t.Fatalf("message %s never arrived", id)
		case <-ctx.Done():
			t.Fatalf("context ended waiting for %s", id)
		}
	}
}

func drain[T any](ch <-chan T) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
