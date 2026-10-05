package notify

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/codelif/whatevr/platform"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
	"whatevrd/internal/live"
)

const nativeVersion = 1
const queueSize = 64

type nativeNotice struct {
	ID     string `json:"id"`
	Chat   string `json:"chat"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Sender string `json:"sender"`
	Avatar string `json:"avatar"`
	Sound  bool   `json:"sound"`
}
type nativeMessage struct {
	Version   int           `json:"version"`
	Type      string        `json:"type"`
	Namespace string        `json:"namespace,omitempty"`
	ID        string        `json:"id,omitempty"`
	Chat      string        `json:"chat,omitempty"`
	Status    string        `json:"status,omitempty"`
	Notice    *nativeNotice `json:"notice,omitempty"`
}
type Worker struct {
	log       zerolog.Logger
	open      func(string) bool
	namespace string
	queue     chan nativeMessage
	once      sync.Once
}

func NewWorker(_ context.Context, log zerolog.Logger, open func(string) bool) (*Worker, error) {
	if _, err := helperPath(); err != nil {
		return nil, err
	}
	socket, err := platform.SocketPath()
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(socket))
	return &Worker{log: log, open: open, namespace: fmt.Sprintf("%x", hash[:16]), queue: make(chan nativeMessage, queueSize)}, nil
}
func (w *Worker) Show(n live.Notification) {
	if w == nil {
		return
	}
	content := Format(Capabilities{Body: true}, n)
	w.push(nativeMessage{Type: "show", Notice: &nativeNotice{ID: n.ID, Chat: n.Chat, Title: content.Summary, Body: content.Body, Sender: n.Sender, Avatar: n.Avatar, Sound: n.Sound}})
}
func (w *Worker) Close(id string) {
	if w != nil {
		w.push(nativeMessage{Type: "close", ID: id})
	}
}
func (w *Worker) push(m nativeMessage) {
	m.Version = nativeVersion
	m.Namespace = w.namespace
	select {
	case w.queue <- m:
	default:
		w.log.Warn().Msg("notify: queue full, dropping notification")
	}
}
func connectHelper(ctx context.Context) (net.Conn, error) {
	app, err := helperPath()
	if err != nil {
		return nil, err
	}
	id, err := helperID(app)
	if err != nil {
		return nil, err
	}
	path, err := platform.NotificationSocket(id)
	if err != nil {
		return nil, err
	}
	dial := func() (net.Conn, error) {
		info, err := os.Lstat(filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode().Perm()&0o077 != 0 || stat.Uid != uint32(os.Geteuid()) {
			return nil, errors.New("notification socket directory is not private and owned by this user")
		}
		c, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
		if err != nil {
			return nil, err
		}
		raw, err := c.(*net.UnixConn).SyscallConn()
		if err != nil {
			c.Close()
			return nil, err
		}
		var peer *unix.Xucred
		var credentialErr error
		err = raw.Control(func(fd uintptr) {
			peer, credentialErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		})
		if err != nil || credentialErr != nil || peer == nil || peer.Uid != uint32(os.Geteuid()) {
			c.Close()
			return nil, errors.New("notification helper peer is not this user")
		}
		return c, nil
	}
	if c, err := dial(); err == nil {
		return c, nil
	}
	if err := launchHelper(app); err != nil {
		return nil, err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if c, err := dial(); err == nil {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, errors.New("notification helper did not create its socket")
		case <-ticker.C:
		}
	}
}
func readNative(r io.Reader) (nativeMessage, error) {
	// A line is limited to 1 MiB; notifications cannot grow memory without bound.
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nativeMessage{}, err
		}
		return nativeMessage{}, io.EOF
	}
	var m nativeMessage
	err := json.Unmarshal(scanner.Bytes(), &m)
	if err == nil && m.Version != nativeVersion {
		err = errors.New("unsupported native notification protocol version")
	}
	return m, err
}
func Configure(ctx context.Context, setup bool) (string, error) {
	c, err := connectHelper(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(deadline)
	}
	typ := "status"
	if setup {
		typ = "authorize"
	}
	if err := json.NewEncoder(c).Encode(nativeMessage{Version: nativeVersion, Type: typ}); err != nil {
		return "", err
	}
	m, err := readNative(c)
	if err != nil {
		return "", err
	}
	if m.Type != "status" {
		return "", errors.New("unexpected notification helper response")
	}
	return m.Status, nil
}
func (w *Worker) Start(ctx context.Context) {
	if w != nil {
		w.once.Do(func() { go w.run(ctx) })
	}
}
func (w *Worker) run(ctx context.Context) {
	var pending *nativeMessage
	for ctx.Err() == nil {
		c, err := connectHelper(ctx)
		if err == nil {
			pending, err = w.session(ctx, c, pending)
			_ = c.Close()
		}
		if ctx.Err() != nil {
			return
		}
		w.log.Warn().Err(err).Msg("notify: helper disconnected; retrying")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
func (w *Worker) session(ctx context.Context, c net.Conn, pending *nativeMessage) (*nativeMessage, error) {
	encoder := json.NewEncoder(c)
	write := func(m nativeMessage) error {
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return encoder.Encode(m)
	}
	if err := write(nativeMessage{Version: nativeVersion, Type: "hello", Namespace: w.namespace}); err != nil {
		return pending, err
	}
	events := make(chan nativeMessage, 16)
	failures := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		scanner := bufio.NewScanner(c)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var m nativeMessage
			err := json.Unmarshal(scanner.Bytes(), &m)
			if err != nil || m.Version != nativeVersion {
				failures <- errors.New("invalid native notification response")
				return
			}
			select {
			case events <- m:
			case <-done:
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		failures <- err
	}()
	if pending != nil {
		if err := write(*pending); err != nil {
			return pending, err
		}
		pending = nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-failures:
			return nil, err
		case m := <-w.queue:
			if err := write(m); err != nil {
				return &m, err
			}
		case m := <-events:
			switch m.Type {
			case "click":
				if m.Namespace == w.namespace && (w.open == nil || !w.open(m.Chat)) {
					w.log.Info().Str("chat", m.Chat).Msg("notify: clicked with no frontend connected")
				}
			case "status":
				w.log.Info().Str("authorization", m.Status).Msg("notify: macOS notification settings")
			}
		}
	}
}
