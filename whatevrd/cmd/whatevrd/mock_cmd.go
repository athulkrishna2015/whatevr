//go:build whatevr_mock

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"google.golang.org/protobuf/encoding/protojson"

	"whatevrd/internal/probe"
)

const mockUsage = `usage: whatevrd mock snapshot --socket PATH
       whatevrd mock send --socket PATH --to PHONE --text TEXT
       whatevrd mock watch --socket PATH VIEW...

snapshot subscribes to every view, every chat's included, and prints what
each holds once all are filled and quiet. send writes to someone through
the daemon. watch prints every row of the named global views as a json
line as it lands, until the daemon goes. for scripts/replay and
scripts/netfault against a mock daemon.
`

// per-chat views loading at once: a first sync's worth at once outgrows
// the daemon's queue for this connection
const chatViewsAtOnce = 32

func runMock(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, mockUsage)
		return 2
	}
	fs := flag.NewFlagSet("whatevrd mock "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	socket := fs.String("socket", "", "the daemon's socket")
	to := fs.String("to", "", "send: the phone number to write to")
	text := fs.String("text", "", "send: what to write")
	if err := fs.Parse(args[1:]); err != nil || *socket == "" || (fs.NArg() > 0) != (args[0] == "watch") {
		fmt.Fprint(stderr, mockUsage)
		return 2
	}
	var err error
	switch args[0] {
	case "snapshot":
		err = mockSnapshot(*socket, stdout)
	case "send":
		err = mockSend(*socket, *to, *text)
	case "watch":
		err = mockWatch(*socket, fs.Args(), stdout)
	default:
		fmt.Fprint(stderr, mockUsage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "whatevrd mock %s: %v\n", args[0], err)
		return 1
	}
	return 0
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
		"chats":          v2.Subscribe_builder{Limit: 10000, Chats: v2.ChatsView_builder{Filter: all}.Build()}.Build(),
		"chats archived": v2.Subscribe_builder{Limit: 10000, Chats: v2.ChatsView_builder{Filter: all, Archived: true}.Build()}.Build(),
		"typing":         v2.Subscribe_builder{Typing: &v2.TypingView{}}.Build(),
		"privacy":        v2.Subscribe_builder{Privacy: &v2.PrivacyView{}}.Build(),
		"preferences":    v2.Subscribe_builder{Preferences: &v2.PreferencesView{}}.Build(),
		"blocklist":      v2.Subscribe_builder{Blocklist: &v2.BlocklistView{}}.Build(),
		"starred":        v2.Subscribe_builder{Limit: 10000, Starred: &v2.StarredView{}}.Build(),
		"stickers":       v2.Subscribe_builder{Limit: 10000, Stickers: v2.StickersView_builder{Source: v2.StickerSource_STICKER_SOURCE_ALL}.Build()}.Build(),
		"sticker_packs":  v2.Subscribe_builder{StickerPacks: &v2.StickerPacksView{}}.Build(),
		"transfers":      v2.Subscribe_builder{Transfers: &v2.TransfersView{}}.Build(),
		"notifications":  v2.Subscribe_builder{Notifications: &v2.NotificationsView{}}.Build(),
	}
}

// chatViews leaves presence out: subscribing to it asks whatsapp.
func chatViews(id string, group bool) map[string]*v2.Subscribe {
	vs := map[string]*v2.Subscribe{
		"chat":           v2.Subscribe_builder{Chat: v2.ChatView_builder{ChatId: id}.Build()}.Build(),
		"messages":       v2.Subscribe_builder{Limit: 10000, Messages: v2.MessagesView_builder{ChatId: id, Latest: &v2.Latest{}}.Build()}.Build(),
		"pinned":         v2.Subscribe_builder{Pinned: v2.PinnedView_builder{ChatId: id}.Build()}.Build(),
		"live_locations": v2.Subscribe_builder{LiveLocations: v2.LiveLocationsView_builder{ChatId: id}.Build()}.Build(),
		"chat_media":     v2.Subscribe_builder{Limit: 10000, ChatMedia: v2.ChatMediaView_builder{ChatId: id}.Build()}.Build(),
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
	loading := func() int {
		n := 0
		for _, p := range pending {
			if p.chat {
				n++
			}
		}
		for sub := range chatSubs {
			if !c.Views[sub].Ready {
				n++
			}
		}
		return n
	}
	settled := func() bool {
		if len(waiting) > 0 || len(pending) > 0 {
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
			sub := resp.GetSubscribe().GetSub()
			names[sub] = p.name
			if p.chat {
				chatSubs[sub] = true
			}
		}
		discover()
	}

	out := map[string]map[string]snapItem{}
	opts := protojson.MarshalOptions{UseProtoNames: true}
	for sub, name := range names {
		items := map[string]snapItem{}
		for id, it := range c.Views[sub].Items {
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
