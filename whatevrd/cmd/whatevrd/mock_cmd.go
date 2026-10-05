//go:build whatevr_mock

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"sort"
	"strings"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/encoding/protojson"

	"whatevrd/internal/probe"
	"whatevrd/internal/views"
)

// per-chat views loading or closing at once: a first sync's worth at once
// outgrows the daemon's queue for this connection, and with the global views
// this stays under its 64 subscriptions
const chatViewsAtOnce = 32

// mockCommand is for scripts/replay and scripts/netfault against a mock daemon.
func mockCommand() *cli.Command {
	socket := &cli.StringFlag{Name: "socket", Usage: "the daemon's socket", Required: true}
	act := func(name string, do func(c *cli.Command, stdout io.Writer) error) cli.ActionFunc {
		return func(_ context.Context, c *cli.Command) error {
			stdout, stderr := outputs(c)
			if err := do(c, stdout); err != nil {
				fmt.Fprintf(stderr, "whatevrd mock %s: %v\n", name, err)
				return code(1)
			}
			return nil
		}
	}
	return group(&cli.Command{
		Name:     "mock",
		Usage:    "probe a mock daemon",
		Category: "debug",
		Commands: []*cli.Command{
			{
				Name:        "snapshot",
				Usage:       "print every view once all are filled and quiet",
				Description: "subscribes to every view, every chat's included.",
				Flags:       []cli.Flag{socket},
				Action: act("snapshot", func(c *cli.Command, stdout io.Writer) error {
					if c.Args().Present() {
						return usage(c, "snapshot takes no arguments")
					}
					return mockSnapshot(c.String("socket"), stdout)
				}),
			},
			{
				Name:  "send",
				Usage: "write to someone through the daemon",
				Flags: []cli.Flag{
					socket,
					&cli.StringFlag{Name: "to", Usage: "the phone number to write to"},
					&cli.StringFlag{Name: "text", Usage: "what to write"},
				},
				Action: act("send", func(c *cli.Command, _ io.Writer) error {
					if c.Args().Present() {
						return usage(c, "send takes no arguments")
					}
					return mockSend(c.String("socket"), c.String("to"), c.String("text"))
				}),
			},
			{
				Name:        "watch",
				Usage:       "print every row of the named global views as a json line",
				Description: "rows print as they land, until the daemon goes.",
				ArgsUsage:   "VIEW...",
				Flags:       []cli.Flag{socket},
				Action: act("watch", func(c *cli.Command, stdout io.Writer) error {
					if !c.Args().Present() {
						return usage(c, "watch takes at least one view")
					}
					return mockWatch(c.String("socket"), c.Args().Slice(), stdout)
				}),
			},
		},
	})
}

func mockSend(socket, to, text string) error {
	c, err := probe.Dial(socket, "mock send", 30*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	r, err := c.Ask(v2.Request_builder{ChatEnsureDirect: v2.ChatEnsureDirect_builder{
		Person: v2.Address_builder{Phone: &to}.Build()}.Build()}.Build(), 30*time.Second)
	if err != nil {
		return fmt.Errorf("chat_ensure_direct: %w", err)
	}
	chat := r.GetChatEnsureDirect().GetChatId()
	_, err = c.Ask(v2.Request_builder{SendText: v2.SendText_builder{ChatId: chat, Text: text}.Build()}.Build(), 30*time.Second)
	return err
}

func mockWatch(socket string, views []string, stdout io.Writer) error {
	c, err := probe.Dial(socket, "mock watch", 30*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	names := map[uint64]string{}
	enc := json.NewEncoder(stdout)
	opts := protojson.MarshalOptions{UseProtoNames: true}
	c.OnUpsert = func(sub uint64, u *v2.Upsert) {
		b, _ := opts.Marshal(u)
		_ = enc.Encode(map[string]any{"view": names[sub], "item": json.RawMessage(b)})
	}
	globals := globalViews()
	for _, v := range views {
		s, ok := globals[v]
		if !ok {
			return fmt.Errorf("no global view %q", v)
		}
		r, err := c.Ask(v2.Request_builder{Subscribe: s}.Build(), 30*time.Second)
		if err != nil {
			return fmt.Errorf("%s: %w", v, err)
		}
		names[r.GetSubscribe().GetSub()] = v
	}
	for {
		if _, err := c.Read(time.Hour); err != nil && !errors.Is(err, probe.ErrQuiet) {
			return nil
		}
	}
}

func globalViews() map[string]*v2.Subscribe {
	all := v2.ChatFilter_CHAT_FILTER_ALL
	return map[string]*v2.Subscribe{
		"connection":     v2.Subscribe_builder{Connection: &v2.ConnectionView{}}.Build(),
		"login":          v2.Subscribe_builder{Login: &v2.LoginView{}}.Build(),
		"sync":           v2.Subscribe_builder{Sync: &v2.SyncView{}}.Build(),
		"problems":       v2.Subscribe_builder{Problems: &v2.ProblemsView{}}.Build(),
		"self":           v2.Subscribe_builder{Self: &v2.SelfView{}}.Build(),
		"chats":          v2.Subscribe_builder{Chats: v2.ChatsView_builder{Filter: all}.Build()}.Build(),
		"chats archived": v2.Subscribe_builder{Chats: v2.ChatsView_builder{Filter: all, Archived: true}.Build()}.Build(),
		"typing":         v2.Subscribe_builder{Typing: &v2.TypingView{}}.Build(),
		"privacy":        v2.Subscribe_builder{Privacy: &v2.PrivacyView{}}.Build(),
		"preferences":    v2.Subscribe_builder{Preferences: &v2.PreferencesView{}}.Build(),
		"blocklist":      v2.Subscribe_builder{Blocklist: &v2.BlocklistView{}}.Build(),
		"starred":        v2.Subscribe_builder{Starred: &v2.StarredView{}}.Build(),
		"stickers":       v2.Subscribe_builder{Stickers: v2.StickersView_builder{Source: v2.StickerSource_STICKER_SOURCE_ALL}.Build()}.Build(),
		"sticker_packs":  v2.Subscribe_builder{StickerPacks: &v2.StickerPacksView{}}.Build(),
		"transfers":      v2.Subscribe_builder{Transfers: &v2.TransfersView{}}.Build(),
		"notifications":  v2.Subscribe_builder{Notifications: &v2.NotificationsView{}}.Build(),
	}
}

// chatViews leaves presence out: subscribing to it asks whatsapp.
func chatViews(id string, group bool) map[string]*v2.Subscribe {
	vs := map[string]*v2.Subscribe{
		"chat":           v2.Subscribe_builder{Chat: v2.ChatView_builder{ChatId: id}.Build()}.Build(),
		"messages":       v2.Subscribe_builder{Limit: uint32(views.MessageCap), Messages: v2.MessagesView_builder{ChatId: id, Latest: &v2.Latest{}}.Build()}.Build(),
		"pinned":         v2.Subscribe_builder{Pinned: v2.PinnedView_builder{ChatId: id}.Build()}.Build(),
		"live_locations": v2.Subscribe_builder{LiveLocations: v2.LiveLocationsView_builder{ChatId: id}.Build()}.Build(),
		"chat_media":     v2.Subscribe_builder{ChatMedia: v2.ChatMediaView_builder{ChatId: id}.Build()}.Build(),
	}
	if group {
		vs["group"] = v2.Subscribe_builder{Group: v2.GroupView_builder{ChatId: id}.Build()}.Build()
		vs["group_members"] = v2.Subscribe_builder{GroupMembers: v2.GroupMembersView_builder{ChatId: id}.Build()}.Build()
	}
	return vs
}

type snapItem struct {
	Sort string          `json:"sort"`
	Item json.RawMessage `json:"item"`
}

func mockSnapshot(socket string, stdout io.Writer) error {
	c, err := probe.Dial(socket, "mock snapshot", 30*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()

	type pend struct {
		name string
		chat bool
	}
	pending := map[uint64]pend{}
	names := map[uint64]string{}
	chatSubs := map[uint64]bool{}
	// per-chat views as they were filled: each is closed once it is, or a
	// first sync's chats would outgrow a connection's subscriptions
	taken := map[string]*probe.View{}
	var errs []string
	type queued struct {
		name string
		sub  *v2.Subscribe
	}
	var waiting []queued
	opened := map[string]bool{}

	subscribe := func(name string, s *v2.Subscribe, chat bool) error {
		id, err := c.Send(v2.Request_builder{Subscribe: s}.Build())
		pending[id] = pend{name, chat}
		return err
	}
	globals := globalViews()
	for _, name := range sortedKeys(globals) {
		if err := subscribe(name, globals[name], false); err != nil {
			return err
		}
	}
	// a chat row's chats get their own views once it shows up
	discover := func() {
		for sub, name := range names {
			if name != "chats" && name != "chats archived" {
				continue
			}
			for id, it := range c.Views[sub].Items {
				if opened[id] {
					continue
				}
				opened[id] = true
				t := it.Row.GetChat().GetType()
				vs := chatViews(id, t == v2.ChatType_CHAT_TYPE_GROUP || t == v2.ChatType_CHAT_TYPE_COMMUNITY)
				for _, n := range sortedKeys(vs) {
					waiting = append(waiting, queued{n + " " + id, vs[n]})
				}
			}
		}
	}
	take := func() error {
		for sub := range chatSubs {
			if !c.Views[sub].Ready {
				continue
			}
			taken[names[sub]] = c.Views[sub]
			delete(chatSubs, sub)
			delete(names, sub)
			delete(c.Views, sub)
			id, err := c.Send(v2.Request_builder{Unsubscribe: v2.Unsubscribe_builder{Sub: sub}.Build()}.Build())
			if err != nil {
				return err
			}
			pending[id] = pend{"unsubscribe", true}
		}
		return nil
	}
	// open and closing alike
	loading := func() int {
		n := len(chatSubs)
		for _, p := range pending {
			if p.chat {
				n++
			}
		}
		return n
	}
	settled := func() bool {
		if len(waiting) > 0 || len(pending) > 0 || len(chatSubs) > 0 {
			return false
		}
		for sub := range names {
			if !c.Views[sub].Ready {
				return false
			}
		}
		return true
	}

	quiet := time.Now()
	for {
		for len(waiting) > 0 && loading() < chatViewsAtOnce {
			q := waiting[0]
			waiting = waiting[1:]
			if err := subscribe(q.name, q.sub, true); err != nil {
				return err
			}
		}
		resp, err := c.Read(500 * time.Millisecond)
		if errors.Is(err, probe.ErrQuiet) {
			if settled() && time.Since(quiet) > time.Second {
				break
			}
			continue
		}
		if err != nil {
			return err
		}
		quiet = time.Now()
		if resp != nil {
			p, ok := pending[resp.GetId()]
			if !ok {
				continue
			}
			delete(pending, resp.GetId())
			if e := resp.GetError(); e != nil {
				errs = append(errs, fmt.Sprintf("%s: %s: %s", p.name, e.GetCode(), e.GetMessage()))
				continue
			}
			if s := resp.GetSubscribe(); s != nil {
				names[s.GetSub()] = p.name
				if p.chat {
					chatSubs[s.GetSub()] = true
				}
			}
		}
		discover()
		if err := take(); err != nil {
			return err
		}
	}
	for sub, name := range names {
		taken[name] = c.Views[sub]
	}

	// past a window, a chat's history is paged; anything else is a view the
	// snapshot cannot hold whole, and says so
	for name, v := range taken {
		if v.Exhausted {
			continue
		}
		chat, ok := strings.CutPrefix(name, "messages ")
		if !ok {
			errs = append(errs, name+": more than one window")
			continue
		}
		if err := olderPages(c, chat, v.Items); err != nil {
			return err
		}
	}

	out := map[string]map[string]snapItem{}
	opts := protojson.MarshalOptions{UseProtoNames: true}
	for name, v := range taken {
		items := map[string]snapItem{}
		for id, it := range v.Items {
			row := rowJSON(opts, it.Row)
			items[id] = snapItem{Sort: hex.EncodeToString(it.Sort), Item: row}
		}
		out[name] = items
	}
	if errs == nil {
		errs = []string{}
	}
	sort.Strings(errs)
	enc := json.NewEncoder(stdout)
	return enc.Encode(map[string]any{"views": out, "errors": errs})
}

// olderPages adds the rest of chat's history to items, a window at a time,
// each anchored at the oldest message so far and grown up from it.
func olderPages(c *probe.Client, chat string, items map[string]probe.Item) error {
	for {
		oldest := ""
		for id, it := range items {
			if oldest == "" || bytes.Compare(it.Sort, items[oldest].Sort) < 0 {
				oldest = id
			}
		}
		r, err := c.Ask(v2.Request_builder{Subscribe: v2.Subscribe_builder{Limit: 1,
			Messages: v2.MessagesView_builder{ChatId: chat, MessageId: &oldest}.Build()}.Build()}.Build(), 30*time.Second)
		if err != nil {
			return fmt.Errorf("messages %s at %s: %w", chat, oldest, err)
		}
		sub := r.GetSubscribe().GetSub()
		if err := readies(c, sub, 1); err != nil {
			return err
		}
		_, err = c.Ask(v2.Request_builder{Extend: v2.Extend_builder{Sub: sub, Count: uint32(views.MessageCap - 1),
			Direction: v2.Direction_DIRECTION_OLDER}.Build()}.Build(), 30*time.Second)
		if err != nil {
			return fmt.Errorf("messages %s older than %s: %w", chat, oldest, err)
		}
		if err := readies(c, sub, 2); err != nil {
			return err
		}
		page := c.Views[sub]
		if _, err := c.Ask(v2.Request_builder{Unsubscribe: v2.Unsubscribe_builder{Sub: sub}.Build()}.Build(), 30*time.Second); err != nil {
			return err
		}
		delete(c.Views, sub)
		maps.Copy(items, page.Items)
		if page.Exhausted {
			return nil
		}
		if len(page.Items) < 2 {
			return fmt.Errorf("messages %s: nothing older than %s, yet not exhausted", chat, oldest)
		}
	}
}

// readies reads until sub's window has been filled n times.
func readies(c *probe.Client, sub uint64, n int) error {
	deadline := time.Now().Add(time.Minute)
	for c.Views[sub].Readies < n {
		if _, err := c.Read(time.Until(deadline)); err != nil {
			return fmt.Errorf("subscription %d never filled: %w", sub, err)
		}
	}
	return nil
}

// rowJSON is the row without the id and sort the snapshot keeps beside it.
func rowJSON(opts protojson.MarshalOptions, u *v2.Upsert) json.RawMessage {
	b, err := opts.Marshal(u)
	if err != nil {
		return json.RawMessage(fmt.Sprintf("%q", err.Error()))
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	delete(m, "id")
	delete(m, "sort")
	b, _ = json.Marshal(m)
	return b
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
