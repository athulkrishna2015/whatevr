package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/proto"
	"whattui/internal/term"
	"whattui/internal/theme"
	"whattui/internal/view"
)

// A real whatevrd, in mock mode, is what the golden frames and the paint
// benchmarks now draw. Everything below the socket is production code: the
// store computes the sort keys, the view engine pages the windows, and the
// frame is a frame of an account rather than of a fixture somebody typed.
//
// One daemon per scenario, shared by every test that asks for it, torn down in
// TestMain. Starting one costs about a second; starting one per test would cost
// a minute.

// goldenNow pins the clock every scenario timestamp hangs off. Without it a
// golden frame carries whatever time the test ran at, and no two runs agree.
const goldenNow = "2025-09-16T12:00:00Z"

// framesScenario is the account the golden frames are taken against, and
// framesChat is the chat they open. Both are load bearing: renaming either one
// in wamock breaks this harness, which is the point of the guard test.
const (
	framesScenario = "frames"
	framesChat     = "Reference"
	// The benchmarks draw the accounts nobody has: flood for volume, torture
	// for the scripts that have to be shaped.
	floodScenario   = "flood"
	floodDeepChat   = "Person 0000"
	tortureScenario = "torture"
	tortureChat     = "Text torture"
)

type mockDaemon struct {
	socket string
	cmd    *exec.Cmd
	dir    string
	ctl    net.Conn
	ctlR   *bufio.Reader
	ctlMu  sync.Mutex
}

var (
	daemonsMu sync.Mutex
	daemons   = map[string]*mockDaemon{}

	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	daemonsMu.Lock()
	for _, d := range daemons {
		d.stop()
	}
	daemonsMu.Unlock()
	os.Exit(code)
}

// whatevrdBinary finds the mock-enabled daemon, building it if nobody has.
// WHATTUI_WHATEVRD is how CI hands over a binary it already built; otherwise
// the build is cached under build/mock, so a second run is free.
func whatevrdBinary() (string, error) {
	binaryOnce.Do(func() {
		if path := os.Getenv("WHATTUI_WHATEVRD"); path != "" {
			binaryPath, binaryErr = path, nil
			return
		}
		root, err := repoRoot()
		if err != nil {
			binaryErr = err
			return
		}
		out := filepath.Join(root, "build", "mock", "whatevrd")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			binaryErr = err
			return
		}
		cmd := exec.Command("go", "build", "-buildvcs=false",
			"-tags", "sqlite_fts5 whatevr_mock", "-o", out, "./cmd/whatevrd")
		cmd.Dir = filepath.Join(root, "whatevrd")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			binaryErr = fmt.Errorf("build whatevrd with mock support: %v\n%s", err, output)
			return
		}
		binaryPath = out
	})
	return binaryPath, binaryErr
}

// repoRoot walks up from the test's directory looking for the whatevrd module,
// because whattui is a separate module and knows nothing about where it sits.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "whatevrd", "go.mod")); err == nil {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("no whatevrd module above %s", dir)
}

// mockDaemonFor starts the daemon for a scenario, or hands back the one that is
// already running it.
func mockDaemonFor(t testing.TB, scenario string) *mockDaemon {
	t.Helper()
	daemonsMu.Lock()
	defer daemonsMu.Unlock()
	if d, ok := daemons[scenario]; ok {
		return d
	}

	binary, err := whatevrdBinary()
	if err != nil {
		t.Fatalf("mock daemon: %v", err)
	}
	dir, err := os.MkdirTemp("", "whattui-mock-"+scenario+"-")
	if err != nil {
		t.Fatalf("mock daemon dir: %v", err)
	}
	// prepareMockDir only clears a directory carrying this marker, and it
	// refuses one that exists without it.
	if err := os.WriteFile(filepath.Join(dir, ".whatevr-mock"), []byte("whattui test\n"), 0o600); err != nil {
		t.Fatalf("mock marker: %v", err)
	}
	control := filepath.Join(dir, "control.sock")
	logPath := filepath.Join(dir + ".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("mock log: %v", err)
	}

	cmd := exec.Command(binary,
		"--mock", scenario,
		"--mock-dir", dir,
		"--mock-control", control,
		"--mock-seed", "1",
		"--mock-now", goldenNow,
	)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mock daemon: %v", err)
	}

	d := &mockDaemon{
		socket: filepath.Join(dir, "run", "whatevr", "whatevrd.sock"),
		cmd:    cmd,
		dir:    dir,
	}
	daemons[scenario] = d

	deadline := time.Now().Add(90 * time.Second)
	for {
		if _, err := os.Stat(control); err == nil {
			break
		}
		if time.Now().After(deadline) {
			d.stop()
			delete(daemons, scenario)
			t.Fatalf("mock daemon never bound its control socket\n%s", tail(logPath))
		}
		time.Sleep(20 * time.Millisecond)
	}
	conn, err := net.Dial("unix", control)
	if err != nil {
		d.stop()
		delete(daemons, scenario)
		t.Fatalf("dial mock control: %v", err)
	}
	d.ctl, d.ctlR = conn, bufio.NewReader(conn)
	return d
}

// sync blocks until the mock has nothing left to say. It is half the barrier: a
// caller still waits for its own subscriptions, because whether a view has
// emitted ready is something only this side can see.
func (d *mockDaemon) sync(t testing.TB) {
	t.Helper()
	d.ctlMu.Lock()
	defer d.ctlMu.Unlock()
	if _, err := d.ctl.Write([]byte(`{"cmd":"sync","timeout_ms":90000}` + "\n")); err != nil {
		t.Fatalf("mock sync: %v", err)
	}
	line, err := d.ctlR.ReadBytes('\n')
	if err != nil {
		t.Fatalf("mock sync: %v", err)
	}
	var resp struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		BlockedOn string `json:"blocked_on"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("mock sync: %v", err)
	}
	if !resp.OK {
		t.Fatalf("mock never settled: %s (%s)", resp.Error, resp.BlockedOn)
	}
}

func (d *mockDaemon) stop() {
	if d.ctl != nil {
		_ = d.ctl.Close()
	}
	if d.cmd != nil && d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
		_, _ = d.cmd.Process.Wait()
	}
	_ = os.RemoveAll(d.dir)
	_ = os.Remove(d.dir + ".log")
}

func tail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}

// mockApp is a whole frontend drawing into a cell buffer, fed by a real daemon
// over a real socket. No terminal; everything else is the production path.
func mockApp(t testing.TB, scenario, chatName string, cols, rows int) *App {
	t.Helper()
	d := mockDaemonFor(t, scenario)
	d.sync(t)

	// A cell of ten by twenty pixels, so anything that draws in pixels has
	// something to measure off.
	win := vaxis.NewOffscreenWindowPixels(cols, rows, 10, 20)
	a := &App{
		vx:      win.Vx,
		caps:    term.Caps{Tier: term.TierColor, RGB: true},
		theme:   theme.Derive(vaxis.RGBColor(0x12, 0x14, 0x18), vaxis.RGBColor(0xe4, 0xe4, 0xe6)),
		chats:   view.NewCollection[proto.ChatRow](),
		conn:    view.NewObject[proto.Connection](),
		focus:   FocusComposer,
		hovered: -1,
		images:  map[imgKey]*vaxis.KittyImage{},
		seen:    map[imgKey]bool{},
		glyphs:  map[glyphKey]*image.NRGBA{},
		drag:    drag{chat: -1},
	}
	a.client = proto.New(d.socket, "whattui-test")
	a.request = a.client.Do
	a.initCommands()
	// The same wiring Run uses: the frame says "connecting to whatevrd" until
	// the transport reports otherwise.
	a.client.OnState = a.onTransport
	a.client.Start()
	t.Cleanup(a.client.Stop)

	a.connSub = a.client.Subscribe("connection", nil, a.conn)
	a.subscribeChats()
	waitFor(t, "the socket", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.transport == proto.Ready
	})
	waitFor(t, "the chat list", func() bool { return a.chats.IsReady() && a.chats.Len() > 0 })
	waitFor(t, "the connection state", func() bool {
		c, ok := a.conn.Value()
		return ok && c.State == "online"
	})

	chatID := chatNamed(t, a, chatName)
	a.openChat(chatID)
	waitFor(t, "the transcript", func() bool {
		a.mu.Lock()
		c := a.conversation
		a.mu.Unlock()
		return c != nil && c.msgs.IsReady() && c.msgs.Len() > 0
	})
	// Both halves of the barrier. The mock says it has stopped talking; the
	// views say they have stopped changing. Either one alone leaves a frame
	// that depends on when the daemon happened to finish ingesting.
	d.sync(t)
	settle(t, a)
	return a
}

// settle waits until nothing the frame draws has changed for a while. The
// daemon keeps working after the last stanza is on the wire: a history chunk is
// downloaded, then parsed, then stored, and only then does a chat row move.
func settle(t testing.TB, a *App) {
	t.Helper()
	const quiet = 300 * time.Millisecond
	deadline := time.Now().Add(60 * time.Second)
	last, changed := versions(a), time.Now()
	for time.Since(changed) < quiet {
		if time.Now().After(deadline) {
			t.Fatal("the views never stopped changing")
		}
		time.Sleep(20 * time.Millisecond)
		if now := versions(a); now != last {
			last, changed = now, time.Now()
		}
	}
}

// versions is every counter that moves when something a frame draws changes.
func versions(a *App) [2]uint64 {
	a.mu.Lock()
	c := a.conversation
	a.mu.Unlock()
	out := [2]uint64{a.chats.Version(), 0}
	if c != nil {
		out[1] = c.msgs.Version()
	}
	return out
}

// chatNamed finds a chat by the name the scenario gave it. The frame is taken
// of a named conversation rather than of whatever happens to sort first, so a
// scenario can gain a chat without moving every golden.
func chatNamed(t testing.TB, a *App, name string) string {
	t.Helper()
	found := ""
	a.chats.Read(func(items []view.Item[proto.ChatRow], _ view.State) {
		for _, item := range items {
			if item.Value.Name == name {
				found = item.Value.ID
				return
			}
		}
	})
	if found == "" {
		t.Fatalf("no chat named %q in the mock account", name)
	}
	return found
}

func waitFor(t testing.TB, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
