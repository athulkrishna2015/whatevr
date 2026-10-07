package commands

import (
	"context"
	"sync"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/frontends"
	"whatevrd/internal/links"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

// relaunchAfter keeps a burst of clicks from opening a terminal each.
const relaunchAfter = 15 * time.Second

// Opener is where a notification click or a link lands: a chat or a frontend
// brought forward, the default frontend started when none is connected.
type Opener struct {
	x *commands

	mu       sync.Mutex
	launched time.Time
}

// ChatKey opens the chat behind a notification, by its key.
func (o *Opener) ChatKey(key string) bool {
	if key == "" {
		return false
	}
	id, err := o.x.id(context.Background(), key)
	if err != nil {
		o.x.log.Warn().Err(err).Msg("open: no id for the clicked chat")
		return false
	}
	o.chat(id)
	return true
}

// Activate brings the frontend forward, or starts one.
func (o *Opener) Activate() {
	if !o.x.srv.Activate() {
		o.launch()
	}
}

// Link opens a whatevr:// link.
func (o *Opener) Link(ctx context.Context, raw string) error {
	l, err := links.Parse(raw)
	if err != nil {
		return invalid("%v", err)
	}
	switch l.Kind {
	case links.Activate:
		o.Activate()
		return nil
	case links.Chat:
		if _, err := o.x.chat(ctx, l.ChatID); err != nil {
			return err
		}
		o.chat(l.ChatID)
		return nil
	}
	a := v2.Address_builder{}
	switch {
	case l.Phone != "":
		a.Phone = &l.Phone
	case l.LID != "":
		a.Lid = &l.LID
	default:
		a.Username = &l.Username
	}
	key, err := o.x.person(ctx, a.Build())
	if err != nil {
		return err
	}
	if model.IsGroup(key) {
		return invalid("that is a group, not a person")
	}
	id, err := o.x.id(ctx, key)
	if err != nil {
		return err
	}
	o.chat(id)
	return nil
}

func (o *Opener) chat(id string) {
	if !o.x.srv.OpenChat(id) {
		o.launch()
	}
}

func (o *Opener) launch() {
	if !o.x.launch {
		return
	}
	o.mu.Lock()
	if time.Since(o.launched) < relaunchAfter {
		o.mu.Unlock()
		return
	}
	o.launched = time.Now()
	o.mu.Unlock()
	p := o.x.c.Prefs(context.Background())
	id := frontends.DefaultID(p.GetDefaultFrontend())
	f, ok := frontends.Find(o.x.dirs, id)
	if !ok {
		o.hint("No frontend to open", id+" isn't installed. Pick one: whatevrd frontend set-default <id>")
		return
	}
	if err := frontends.Launch(f, p.GetTerminal()); err != nil {
		o.x.log.Warn().Err(err).Str("frontend", id).Msg("open: start the default frontend")
		o.hint("Couldn't open "+id, err.Error())
		return
	}
	o.x.log.Info().Str("frontend", id).Msg("open: started the default frontend")
}

func (o *Opener) hint(title, body string) {
	o.x.log.Warn().Str("title", title).Msg(body)
	if o.x.hint != nil {
		o.x.hint(title, body)
	}
}

func (x *commands) linkOpen(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	return nil, x.opener.Link(ctx, req.GetLinkOpen().GetUrl())
}
