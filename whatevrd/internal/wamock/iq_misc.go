//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// accountSettings is the small pile of per-account state that is not part of
// the world: privacy categories, the blocklist, the about line. Scenarios do
// not script these yet, but the client changes them and expects the change to
// stick across a reconnect.
type accountSettings struct {
	mu sync.Mutex

	privacy   map[string]string
	blocked   []types.JID
	about     string
	aboutAt   time.Time
	dirtyRead map[string]time.Time
}

func newAccountSettings() *accountSettings {
	return &accountSettings{
		// The defaults a fresh account ships with.
		privacy: map[string]string{
			"groupadd":     "all",
			"last":         "all",
			"status":       "all",
			"profile":      "all",
			"readreceipts": "all",
			"online":       "all",
			"calladd":      "all",
			"messages":     "all",
		},
		about:     mockStatus,
		aboutAt:   Ago(72 * time.Hour),
		dirtyRead: map[string]time.Time{},
	}
}

// handlePrivacyIQ serves and updates the privacy categories.
func (s *session) handlePrivacyIQ(ctx context.Context, node *waBinary.Node) error {
	settings := s.srv.settings
	if node.AttrGetter().OptionalString("type") == "set" {
		if privacy, ok := node.GetOptionalChildByTag("privacy"); ok {
			settings.mu.Lock()
			for _, child := range privacy.GetChildren() {
				if child.Tag != "category" {
					continue
				}
				ag := child.AttrGetter()
				settings.privacy[ag.String("name")] = ag.String("value")
			}
			settings.mu.Unlock()
		}
		return s.sendNode(ctx, iqResult(node))
	}
	return s.sendNode(ctx, iqResult(node, settings.privacyNode()))
}

func (a *accountSettings) privacyNode() waBinary.Node {
	a.mu.Lock()
	defer a.mu.Unlock()
	categories := make([]waBinary.Node, 0, len(a.privacy))
	for _, name := range sortedKeys(a.privacy) {
		categories = append(categories, waBinary.Node{
			Tag:   "category",
			Attrs: waBinary.Attrs{"name": name, "value": a.privacy[name]},
		})
	}
	return waBinary.Node{Tag: "privacy", Content: categories}
}

// handleBlocklistIQ serves and updates the block list. The client addresses
// blocks by LID, which the mock hands out for everybody.
func (s *session) handleBlocklistIQ(ctx context.Context, node *waBinary.Node) error {
	settings := s.srv.settings
	if node.AttrGetter().OptionalString("type") == "set" {
		for _, child := range node.GetChildren() {
			if child.Tag != "item" {
				continue
			}
			ag := child.AttrGetter()
			settings.setBlocked(ag.OptionalJIDOrEmpty("jid"), ag.OptionalString("action") == "block")
		}
	}
	return s.sendNode(ctx, iqResult(node, settings.blocklistNode()))
}

func (a *accountSettings) setBlocked(jid types.JID, blocked bool) {
	if jid.IsEmpty() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.blocked[:0]
	for _, existing := range a.blocked {
		if existing != jid {
			out = append(out, existing)
		}
	}
	a.blocked = out
	if blocked {
		a.blocked = append(a.blocked, jid)
	}
}

func (a *accountSettings) blocklistNode() waBinary.Node {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := make([]waBinary.Node, 0, len(a.blocked))
	for _, jid := range a.blocked {
		items = append(items, waBinary.Node{Tag: "item", Attrs: waBinary.Attrs{"jid": jid}})
	}
	return waBinary.Node{
		Tag:     "list",
		Attrs:   waBinary.Attrs{"dhash": fmt.Sprintf("mock-%d", len(a.blocked))},
		Content: items,
	}
}

// handleDirtyIQ answers the "what have I not caught up on" query. Nothing in a
// mock account is ever dirty: the world is whatever the scenario said it is.
func (s *session) handleDirtyIQ(ctx context.Context, node *waBinary.Node) error {
	if node.AttrGetter().OptionalString("type") == "set" {
		return s.sendNode(ctx, iqResult(node))
	}
	var categories []waBinary.Node
	for _, name := range []string{"groups", "account_sync"} {
		categories = append(categories, waBinary.Node{
			Tag: "category",
			Attrs: waBinary.Attrs{
				"name":      name,
				"timestamp": fmt.Sprintf("%d", Ago(24*time.Hour).Unix()),
			},
		})
	}
	return s.sendNode(ctx, iqResult(node, categories...))
}

// handleMexIQ answers the GraphQL-shaped queries. The only one the daemon
// makes is the status update behind self.set_about, and the client does not
// read the result, so an acknowledgement in the right shape is the whole job.
func (s *session) handleMexIQ(ctx context.Context, node *waBinary.Node) error {
	if query, ok := node.GetOptionalChildByTag("query"); ok {
		if payload, ok := query.Content.([]byte); ok && strings.Contains(string(payload), "text") {
			s.srv.settings.setAbout(extractJSONString(string(payload), "text"))
		}
	}
	return s.sendNode(ctx, iqResult(node, waBinary.Node{
		Tag:     "result",
		Content: []byte(`{"data":{}}`),
	}))
}

func (a *accountSettings) setAbout(text string) {
	if text == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.about = text
	a.aboutAt = time.Now()
}

func (a *accountSettings) aboutText() (string, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.about, a.aboutAt
}

// extractJSONString pulls one string field out of a JSON blob without knowing
// the rest of its shape, which the mock deliberately does not model.
func extractJSONString(payload, field string) string {
	key := `"` + field + `":"`
	start := strings.Index(payload, key)
	if start < 0 {
		return ""
	}
	rest := payload[start+len(key):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// handleStatusIQ answers the about-line query and the broadcast list one, which
// share a namespace.
func (s *session) handleStatusIQ(ctx context.Context, node *waBinary.Node) error {
	about, at := s.srv.settings.aboutText()
	return s.sendNode(ctx, iqResult(node, waBinary.Node{
		Tag:     "status",
		Attrs:   waBinary.Attrs{"t": fmt.Sprintf("%d", at.Unix())},
		Content: []byte(about),
	}))
}

// handleAppStateIQ answers the app state sync with the real patches the mock
// encoded for this account: the push name, and whatever pins, archives and
// mutes the scenario declared.
func (s *session) handleAppStateIQ(ctx context.Context, node *waBinary.Node) error {
	sync, ok := node.GetOptionalChildByTag("sync")
	if !ok {
		return s.sendNode(ctx, iqResult(node))
	}
	if err := s.srv.appState.build(ctx, s.srv.world); err != nil {
		s.srv.log.Printf("app state: %v", err)
		return s.sendNode(ctx, iqError(node, 500, "internal-server-error"))
	}
	names := make([]string, 0, len(sync.GetChildren()))
	for _, child := range sync.GetChildren() {
		if child.Tag == "collection" {
			names = append(names, child.AttrGetter().String("name"))
		}
	}
	// Answering a client that has not got the key yet with real patches costs
	// it a failed decode and a key re-request. An empty collection costs it
	// nothing, and the sync that follows the key share brings the real thing.
	hasKey := s.srv.appState.clientHasKey(names)

	collections := make([]waBinary.Node, 0, len(sync.GetChildren()))
	for _, child := range sync.GetChildren() {
		if child.Tag != "collection" {
			continue
		}
		ag := child.AttrGetter()
		name := ag.String("name")
		// A collection carrying a patch is the client telling us something
		// changed, not asking what did.
		for _, patch := range collectPatchNodes(&child) {
			if err := s.srv.appState.accept(ctx, s.srv.world, appstate.WAPatchName(name), patch); err != nil {
				s.srv.log.Printf("app state: %v", err)
			}
		}
		if !hasKey {
			collections = append(collections, waBinary.Node{
				Tag: "collection",
				Attrs: waBinary.Attrs{
					"name":             name,
					"version":          "0",
					"has_more_patches": "false",
				},
			})
			continue
		}
		collections = append(collections, s.srv.appState.collection(name, uint64(ag.OptionalInt("version"))))
	}
	return s.sendNode(ctx, iqResult(node, waBinary.Node{Tag: "sync", Content: collections}))
}

// handleCompanionIQ answers the multi-device namespace. The only thing the
// daemon sends here is a logout, which unlinks this device: the mock forgets
// the pairing so a restart goes back through the QR.
func (s *session) handleCompanionIQ(ctx context.Context, node *waBinary.Node) error {
	if _, ok := node.GetOptionalChildByTag("remove-companion-device"); ok {
		s.srv.forgetPairing()
		s.srv.log.Printf("logged out %s", s.jid)
	}
	return s.sendNode(ctx, iqResult(node))
}

// handlePingIQ answers the keepalive. Answering it is not optional: whatsmeow
// tears the connection down after a few unanswered pings.
func (s *session) handlePingIQ(ctx context.Context, node *waBinary.Node) error {
	return s.sendNode(ctx, iqResult(node))
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// collectPatchNodes finds the patches in a collection the client sent. They
// arrive bare today, but the fetch response wraps them in <patches>, so both
// spellings are read here.
func collectPatchNodes(collection *waBinary.Node) [][]byte {
	var out [][]byte
	take := func(node waBinary.Node) {
		if raw, ok := node.Content.([]byte); ok && node.Tag == "patch" {
			out = append(out, raw)
		}
	}
	for _, child := range collection.GetChildren() {
		if child.Tag == "patches" {
			for _, patch := range child.GetChildren() {
				take(patch)
			}
			continue
		}
		take(child)
	}
	return out
}
