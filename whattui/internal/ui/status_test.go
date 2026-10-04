package ui

import (
	"strings"
	"testing"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"go.rockorager.dev/vaxis"
)

// headerText is what the header row says
func (a *App) headerText() string {
	r := a.layout().Header
	out := ""
	for col := r.Col; col < r.Col+r.Width; col++ {
		out += a.vx.Cell(col, r.Row).Grapheme
	}
	return out
}

func syncing(a *App, percent uint32) {
	upsert(a.syncs, v2.Upsert_builder{Sync: v2.SyncRow_builder{
		Type: v2.SyncType_SYNC_TYPE_FULL, Phase: v2.SyncPhase_SYNC_PHASE_RUNNING, Percent: percent, Chunk: 18,
	}.Build()})
	ready(a.syncs, false)
}

func problem(a *App, id, sort, text string, retryMs int64) {
	upsert(a.problems, v2.Upsert_builder{Id: id, Sort: []byte(sort), Problem: v2.ProblemRow_builder{
		Kind: v2.ProblemKind_PROBLEM_KIND_SEND_FAILING, Text: text, NextRetryMs: retryMs,
	}.Build()})
}

func TestTheHeaderSaysTheTopProblemAndTheSync(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	syncing(a, 43)
	problem(a, "a", "1", "sends are failing", 0)
	problem(a, "b", "2", "the disk is full", 0)
	ready(a.problems, false)
	a.paint()

	if got := a.headerText(); !strings.Contains(got, "sends are failing · syncing 43%") || strings.Contains(got, "disk") {
		t.Errorf("the header says %q", got)
	}

	// narrow, the sync goes before the problem does
	a = stubApp(60, 30, 4, 6)
	syncing(a, 43)
	problem(a, "a", "1", "messages are waiting on a resend from the sender's phone", 0)
	ready(a.problems, false)
	a.paint()
	got := a.headerText()
	if strings.Contains(got, "syncing") || !strings.Contains(got, "messages are") {
		t.Errorf("the narrow header says %q", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "contact 0") && !strings.HasPrefix(strings.TrimSpace(got), "whattui") {
		t.Errorf("the summary ran over the title: %q", got)
	}
}

func TestTheHeaderShowsASyncAlone(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	syncing(a, 7)
	ready(a.problems, false)
	a.paint()
	if got := strings.TrimSpace(a.headerText()); !strings.HasSuffix(got, "syncing 7%") || strings.Contains(got, "·") {
		t.Errorf("the header says %q", got)
	}
}

func TestStatusListsEverythingInTheDaemonsOrder(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	setConn(a.conn, v2.ConnectionRow_builder{
		State: v2.ConnectionState_CONNECTION_STATE_ONLINE, SinceMs: time.Date(2026, 10, 4, 14, 2, 0, 0, time.Local).UnixMilli(),
		PendingOutgoing: 2,
	}.Build())
	syncing(a, 43)
	problem(a, "b", "2", "the disk is full", 0)
	problem(a, "a", "1", "sends are failing", time.Now().Add(12*time.Second).UnixMilli())
	ready(a.problems, false)

	if !a.execute(cmdStatus) || a.modal.kind != modalStatus {
		t.Fatal("/status opened nothing")
	}
	a.paint()
	var got []string
	for _, c := range a.modal.selector.Items() {
		got = append(got, strings.TrimSpace(c.Label+" | "+c.Detail))
	}
	want := []string{
		"connection | online since 14:02, 2 sends waiting",
		"sync full | running 43%, chunk 18",
		"sends are failing, retry in 12 s |",
		"the disk is full |",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("status lists\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// live: a problem going away goes from the open panel too
	remove(a.problems, "a")
	a.paint()
	if n := len(a.modal.selector.Items()); n != 3 {
		t.Errorf("after the problem cleared the panel has %d rows", n)
	}

	if hints := a.hintBarText(); !strings.Contains(hints, "esc close") || strings.Contains(hints, "scroll") || strings.Contains(hints, "run") {
		t.Errorf("the hint line under /status says %q", hints)
	}

	// read only: enter leaves it open and does nothing
	a.onKey(arrow(vaxis.KeyEnter))
	if a.modal.kind != modalStatus {
		t.Error("enter closed the status panel")
	}
}

func TestClickingTheSummaryOpensStatus(t *testing.T) {
	a := stubApp(120, 30, 4, 6)
	syncing(a, 43)
	ready(a.problems, false)
	a.paint()

	at := a.statusAt
	if at.Empty() {
		t.Fatal("the summary has no place")
	}
	m := vaxis.Mouse{Col: at.Col + 1, Row: at.Row, EventType: vaxis.EventMotion, Button: vaxis.MouseNoButton}
	if !a.onMouse(m) || !a.statusHovered {
		t.Fatal("the summary did not answer the pointer")
	}
	a.paint()
	if bg := a.vx.Cell(at.Col, at.Row).Style.Background; bg != a.theme.BackgroundHover {
		t.Errorf("the hovered summary is on %v, want the hover colour", bg)
	}

	m.Button, m.EventType = vaxis.MouseLeftButton, vaxis.EventPress
	a.onMouse(m)
	m.EventType = vaxis.EventRelease
	a.onMouse(m)
	if a.modal.kind != modalStatus {
		t.Fatal("clicking the summary did not open /status")
	}
}

func TestStatusIsASlashCommand(t *testing.T) {
	a := stubApp(100, 26, 4, 6)
	for _, c := range a.commandChoices("stat", true, false) {
		if c.Command == cmdStatus {
			return
		}
	}
	t.Error("/status is not in the slash menu")
}
