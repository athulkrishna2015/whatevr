package whatsapp

import (
	"context"
	"testing"
	"time"

	"whatevrd/internal/live"
	"whatevrd/internal/model"
)

func TestAnAnsweredOlderRequestClearsAtOnce(t *testing.T) {
	hub := live.New()
	c := &Client{live: hub, o: Options{World: func(context.Context) (*model.World, error) { return &model.World{}, nil }}}
	const chat = "917770000001@s.whatsapp.net"
	c.older.out = map[string]*time.Timer{chat: time.AfterFunc(time.Hour, func() {})}
	hub.SetLoadingOlder(chat, true)

	c.olderAnswered([]string{"917770000002@s.whatsapp.net"})
	if !hub.LoadingOlder(chat) || c.older.out[chat] == nil {
		t.Fatal("another chat's answer cleared this one")
	}
	c.olderAnswered([]string{chat})
	if hub.LoadingOlder(chat) || c.older.out[chat] != nil {
		t.Fatal("still out after the answer")
	}
}
