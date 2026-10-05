//go:build !darwin

package notify

import (
	"context"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"

	"whatevrd/internal/live"
)

const (
	busName          = "org.freedesktop.Notifications"
	objectPath       = dbus.ObjectPath("/org/freedesktop/Notifications")
	interfaceName    = "org.freedesktop.Notifications"
	dbusObjectPath   = dbus.ObjectPath("/org/freedesktop/DBus")
	dbusInterface    = "org.freedesktop.DBus"
	defaultTimeoutMS = int32(-1)
	queueSize        = 64
)

// Worker shows notifications on the session bus, one desktop notification
// per id, replaced in place as it changes.
type Worker struct {
	conn *dbus.Conn
	obj  dbus.BusObject
	caps Capabilities
	log  zerolog.Logger
	open Handlers

	mu sync.Mutex
	// ids is ours to the server's, chats the server's to the chat it opens
	ids   map[string]uint32
	chats map[uint32]string
	queue chan op
}

type op struct {
	show  *live.Notification
	close string
}

func NewWorker(ctx context.Context, log zerolog.Logger, open Handlers) (*Worker, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	w := &Worker{
		conn:  conn,
		obj:   conn.Object(busName, objectPath),
		log:   log,
		open:  open,
		ids:   map[string]uint32{},
		chats: map[uint32]string{},
		queue: make(chan op, queueSize),
	}
	w.refreshCapabilities(ctx)
	return w, nil
}

func (w *Worker) Start(ctx context.Context) {
	signals := make(chan *dbus.Signal, 16)
	w.conn.Signal(signals)
	_ = w.conn.AddMatchSignal(dbus.WithMatchObjectPath(objectPath), dbus.WithMatchInterface(interfaceName))
	_ = w.conn.AddMatchSignal(dbus.WithMatchObjectPath(dbusObjectPath), dbus.WithMatchInterface(dbusInterface), dbus.WithMatchMember("NameOwnerChanged"))

	go func() {
		defer w.conn.RemoveSignal(signals)
		for {
			select {
			case <-ctx.Done():
				return
			case o := <-w.queue:
				if o.show != nil {
					w.show(ctx, *o.show)
				} else {
					w.close(ctx, o.close)
				}
			case signal := <-signals:
				w.handleSignal(ctx, signal)
			}
		}
	}()
}

// Show puts n up, over the one with its id if that is still up.
func (w *Worker) Show(n live.Notification) { w.push(op{show: &n}) }

// Close takes the notification with id down.
func (w *Worker) Close(id string) { w.push(op{close: id}) }

func (w *Worker) push(o op) {
	// a nil *Worker in an interface is not a nil interface; without a
	// session bus this is the call that lands
	if w == nil {
		return
	}
	select {
	case w.queue <- o:
	default:
		w.log.Warn().Msg("notify: queue full, dropping one")
	}
}

func (w *Worker) refreshCapabilities(ctx context.Context) {
	var values []string
	if err := w.obj.CallWithContext(ctx, interfaceName+".GetCapabilities", 0).Store(&values); err != nil {
		w.log.Warn().Err(err).Msg("notify: capabilities unavailable")
		w.caps = Capabilities{}
		return
	}
	w.caps = ParseCapabilities(values)
}

func (w *Worker) show(ctx context.Context, n live.Notification) {
	content := Format(w.caps, n)
	hints := make(map[string]dbus.Variant, len(content.Hints))
	for key, value := range content.Hints {
		hints[key] = dbus.MakeVariant(value)
	}
	w.mu.Lock()
	replaces := w.ids[n.ID]
	w.mu.Unlock()

	var id uint32
	call := w.obj.CallWithContext(ctx, interfaceName+".Notify", 0,
		"whatevr", replaces, content.Icon, content.Summary, content.Body, content.Actions, hints, defaultTimeoutMS)
	if err := call.Store(&id); err != nil {
		w.log.Warn().Err(err).Str("chat", n.Chat).Msg("notify: send")
		return
	}
	// many servers neither advertise nor honour the sound hint
	if n.Sound {
		playSound()
	}
	w.mu.Lock()
	if replaces != 0 && replaces != id {
		delete(w.chats, replaces)
	}
	w.ids[n.ID] = id
	w.chats[id] = n.Chat
	w.mu.Unlock()
}

func (w *Worker) close(ctx context.Context, key string) {
	w.mu.Lock()
	id, ok := w.ids[key]
	delete(w.ids, key)
	delete(w.chats, id)
	w.mu.Unlock()
	if !ok {
		return
	}
	if err := w.obj.CallWithContext(ctx, interfaceName+".CloseNotification", 0, id).Err; err != nil {
		w.log.Debug().Err(err).Msg("notify: close")
	}
}

func (w *Worker) handleSignal(ctx context.Context, signal *dbus.Signal) {
	if signal == nil {
		return
	}
	switch signal.Name {
	case interfaceName + ".ActionInvoked":
		if len(signal.Body) < 2 {
			return
		}
		id, ok := signal.Body[0].(uint32)
		if !ok {
			return
		}
		w.mu.Lock()
		chat, ok := w.chats[id]
		w.mu.Unlock()
		if ok && (w.open.Chat == nil || !w.open.Chat(chat)) {
			w.log.Info().Str("chat", chat).Msg("notify: click went nowhere")
		}
	case interfaceName + ".NotificationClosed":
		if len(signal.Body) < 1 {
			return
		}
		id, ok := signal.Body[0].(uint32)
		if !ok {
			return
		}
		w.mu.Lock()
		delete(w.chats, id)
		for k, v := range w.ids {
			if v == id {
				delete(w.ids, k)
			}
		}
		w.mu.Unlock()
	case dbusInterface + ".NameOwnerChanged":
		if len(signal.Body) < 3 {
			return
		}
		if name, ok := signal.Body[0].(string); ok && name == busName {
			w.refreshCapabilities(ctx)
		}
	}
}
