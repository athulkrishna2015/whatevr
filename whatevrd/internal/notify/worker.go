package notify

import (
	"context"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"

	"whatevrd/internal/app"
)

const (
	busName          = "org.freedesktop.Notifications"
	objectPath       = dbus.ObjectPath("/org/freedesktop/Notifications")
	interfaceName    = "org.freedesktop.Notifications"
	dbusObjectPath   = dbus.ObjectPath("/org/freedesktop/DBus")
	dbusInterface    = "org.freedesktop.DBus"
	defaultTimeoutMS = int32(-1)
)

// ChatOpener delivers an "open this chat" request to a running frontend. It
// reports whether at least one frontend received it. The protocol Server
// implements it by fanning out connection-directed open_chat events.
type ChatOpener interface {
	OpenChat(chatID string) bool
}

type Worker struct {
	conn   *dbus.Conn
	obj    dbus.BusObject
	caps   Capabilities
	opener ChatOpener

	mu     sync.Mutex
	active map[uint32]string
	queue  chan queuedMessage
}

type queuedMessage struct {
	message app.Message
	chat    app.Chat
	opts    Options
}

func NewWorker(ctx context.Context, opener ChatOpener) (*Worker, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	w := &Worker{
		conn:   conn,
		obj:    conn.Object(busName, objectPath),
		opener: opener,
		active: make(map[uint32]string),
		queue:  make(chan queuedMessage, 64),
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
			case item := <-w.queue:
				w.send(ctx, item.message, item.chat, item.opts)
			case signal := <-signals:
				w.handleSignal(ctx, signal)
			}
		}
	}()
}

func (w *Worker) NotifyMessage(ctx context.Context, message app.Message, chat app.Chat, opts Options) {
	// A daemon with no session bus has no worker, and a nil *Worker put into
	// an interface is not a nil interface: the caller's nil check passes and
	// the call lands here. Saying so once is cheaper than every caller
	// remembering, and the alternative is a segfault on the first message.
	if w == nil {
		return
	}
	select {
	case <-ctx.Done():
	case w.queue <- queuedMessage{message: message, chat: chat, opts: opts}:
	default:
		zerolog.Ctx(ctx).Warn().Str("chat", chat.ID).Str("msg", message.ID).Msg("notification queue full, dropping a notification")
	}
}

func (w *Worker) refreshCapabilities(ctx context.Context) {
	var values []string
	if err := w.obj.Call(interfaceName+".GetCapabilities", 0).Store(&values); err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Msg("notification capabilities unavailable")
		w.caps = Capabilities{}
		return
	}
	w.caps = ParseCapabilities(values)
}

func (w *Worker) send(ctx context.Context, message app.Message, chat app.Chat, opts Options) {
	content := FormatMessage(w.caps, message, chat, opts)
	hints := make(map[string]dbus.Variant, len(content.Hints))
	for key, value := range content.Hints {
		hints[key] = dbus.MakeVariant(value)
	}

	var id uint32
	call := w.obj.CallWithContext(ctx, interfaceName+".Notify", 0,
		"whatevr",
		uint32(0),
		content.Icon,
		content.Summary,
		content.Body,
		content.Actions,
		hints,
		defaultTimeoutMS,
	)
	if err := call.Store(&id); err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Str("chat", chat.ID).Str("msg", message.ID).Msg("send notification")
		return
	}
	zerolog.Ctx(ctx).Info().Str("chat", chat.ID).Str("msg", message.ID).Uint32("notification", id).Msg("notified")

	// Play the sound ourselves rather than trusting the server to honour the
	// sound-name hint — many notification daemons ignore or never advertise it.
	if opts.Sound {
		playSound()
	}

	w.mu.Lock()
	w.active[id] = chat.ID
	w.mu.Unlock()
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
		chatID, ok := w.activeChat(id)
		if !ok {
			return
		}
		w.openChat(ctx, chatID)
	case interfaceName + ".NotificationClosed":
		if len(signal.Body) < 1 {
			return
		}
		id, ok := signal.Body[0].(uint32)
		if ok {
			w.mu.Lock()
			delete(w.active, id)
			w.mu.Unlock()
		}
	case dbusInterface + ".NameOwnerChanged":
		if len(signal.Body) < 3 {
			return
		}
		name, ok := signal.Body[0].(string)
		if ok && name == busName {
			w.refreshCapabilities(ctx)
		}
	}
}

func (w *Worker) activeChat(id uint32) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	chatID, ok := w.active[id]
	return chatID, ok
}

// openChat hands a clicked notification to a running frontend. With none
// connected there is nothing to open: a terminal frontend cannot be started
// from a notification.
func (w *Worker) openChat(ctx context.Context, chatID string) {
	if w.opener != nil && w.opener.OpenChat(chatID) {
		return
	}
	zerolog.Ctx(ctx).Info().Str("chat", chatID).Msg("notification clicked with no frontend connected")
}
