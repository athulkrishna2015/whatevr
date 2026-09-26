//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// handleGroupIQ answers the w:g2 queries the daemon makes: one for a named
// group, one for every group the account is in. Without them a group chat
// arrives with no subject and no member list, so the frontends would render it
// as a bare id.
func (s *session) handleGroupIQ(ctx context.Context, node *waBinary.Node) error {
	world := s.srv.world
	if world == nil {
		return s.sendNode(ctx, iqResult(node))
	}
	to := node.AttrGetter().OptionalJIDOrEmpty("to")

	if _, ok := node.GetOptionalChildByTag("participating"); ok {
		var groups []waBinary.Node
		world.mu.Lock()
		for _, chat := range world.order {
			if chat.IsGroup {
				groups = append(groups, s.srv.groupNode(chat))
			}
		}
		world.mu.Unlock()
		return s.sendNode(ctx, iqResult(node, waBinary.Node{Tag: "groups", Content: groups}))
	}

	chat, ok := world.chatByJID(to)
	if !ok || !chat.IsGroup {
		return s.sendNode(ctx, iqError(node, 404, "item-not-found"))
	}
	return s.sendNode(ctx, iqResult(node, s.srv.groupNode(chat)))
}

// groupNode is the <group> element parseGroupNode reads. Participants carry
// both spellings of their address, because the daemon needs the mapping before
// it can send anything to them.
func (s *Server) groupNode(chat *Chat) waBinary.Node {
	owner := chat.Members[0]
	for _, member := range chat.Members {
		if !member.isSelf {
			owner = member
			break
		}
	}
	participants := make([]waBinary.Node, 0, len(chat.Members))
	for _, member := range chat.Members {
		attrs := waBinary.Attrs{"jid": member.JID, "lid": lidFor(member.JID)}
		if member == owner {
			attrs["type"] = "superadmin"
		}
		participants = append(participants, waBinary.Node{Tag: "participant", Attrs: attrs})
	}
	return waBinary.Node{
		Tag: "group",
		Attrs: waBinary.Attrs{
			"id":       chat.JID.User,
			"creator":  owner.JID,
			"subject":  chat.Name,
			"s_t":      fmt.Sprintf("%d", chat.CreatedAt.Unix()),
			"s_o":      owner.JID,
			"creation": fmt.Sprintf("%d", chat.CreatedAt.Unix()),
			"size":     fmt.Sprintf("%d", len(chat.Members)),
		},
		Content: participants,
	}
}

// announceGroups tells the client about every group in the world, as the real
// server does when the account is added to one. Without it a group chat has no
// subject and no member list: the daemon deliberately skips its group info
// lookup for messages that arrive in an offline sync, and the names a real
// account gets at that point come from a history sync it has not had yet.
func (s *session) announceGroups() {
	world := s.srv.world
	if world == nil {
		return
	}
	world.mu.Lock()
	groups := make([]*Chat, 0, len(world.order))
	for _, chat := range world.order {
		if chat.IsGroup {
			groups = append(groups, chat)
		}
	}
	world.mu.Unlock()

	for _, chat := range groups {
		if !world.markAnnounced(chat) {
			continue
		}
		s.enqueue(func(ctx context.Context) error { return s.sendGroupCreate(ctx, chat) })
	}
}

func (s *session) sendGroupCreate(ctx context.Context, chat *Chat) error {
	creator := chat.Members[0]
	for _, member := range chat.Members {
		if !member.isSelf {
			creator = member
			break
		}
	}
	return s.sendNode(ctx, waBinary.Node{
		Tag: "notification",
		Attrs: waBinary.Attrs{
			"id":          s.srv.rng.stanzaID(),
			"from":        chat.JID,
			"t":           fmt.Sprintf("%d", chat.CreatedAt.Unix()),
			"type":        "w:gp2",
			"participant": creator.JID,
			"notify":      creator.Name,
		},
		Content: []waBinary.Node{{
			Tag:     "create",
			Attrs:   waBinary.Attrs{"type": "new"},
			Content: []waBinary.Node{s.srv.groupNode(chat)},
		}},
	})
}

// handleUsyncIQ answers contact lookups. The mock knows exactly the people the
// scenario put in the world, so anybody else is reported as not on WhatsApp,
// which is the honest answer and one the daemon already handles.
func (s *session) handleUsyncIQ(ctx context.Context, node *waBinary.Node) error {
	usync, ok := node.GetOptionalChildByTag("usync")
	if !ok {
		return s.sendNode(ctx, iqResult(node))
	}
	query, _ := usync.GetOptionalChildByTag("query")
	list, _ := usync.GetOptionalChildByTag("list")

	wanted := map[string]bool{}
	for _, child := range query.GetChildren() {
		wanted[child.Tag] = true
	}

	results := make([]waBinary.Node, 0, len(list.GetChildren()))
	for _, child := range list.GetChildren() {
		if child.Tag != "user" {
			continue
		}
		results = append(results, s.srv.usyncUserNode(&child, wanted))
	}

	return s.sendNode(ctx, iqResult(node, waBinary.Node{
		Tag:   "usync",
		Attrs: waBinary.Attrs{"sid": usync.AttrGetter().OptionalString("sid")},
		Content: []waBinary.Node{{
			Tag:     "list",
			Content: results,
		}},
	}))
}

// usyncUserNode answers for one queried user. A lookup by phone number carries
// no jid attribute, only a <contact> holding the number the caller typed, and
// the caller matches the answer back up by echoing that string.
func (s *Server) usyncUserNode(req *waBinary.Node, wanted map[string]bool) waBinary.Node {
	jid := req.AttrGetter().OptionalJIDOrEmpty("jid")
	contactQuery, _ := req.GetOptionalChildByTag("contact")
	queried := nodeText(contactQuery.Content)
	if jid.IsEmpty() {
		jid = phoneToJID(queried)
	}

	node := waBinary.Node{Tag: "user", Attrs: waBinary.Attrs{"jid": jid}}
	// The client asks by whichever address it happens to hold, and since
	// whatsmeow went LID-first that is usually the LID. The world only knows
	// people by number, so the lookup normalises and the answer echoes back
	// whatever was asked for.
	lookup := jid
	if lookup.Server == types.HiddenUserServer {
		lookup.Server = types.DefaultUserServer
	}
	known := false
	if world := s.world; world != nil && !lookup.IsEmpty() {
		world.mu.Lock()
		_, known = world.contacts[lookup.ToNonAD().String()]
		world.mu.Unlock()
	}

	var content []waBinary.Node
	if wanted["contact"] {
		presence := "out"
		if known {
			presence = "in"
		}
		content = append(content, waBinary.Node{
			Tag:     "contact",
			Attrs:   waBinary.Attrs{"type": presence},
			Content: []byte(queried),
		})
	}
	if !known {
		node.Content = content
		return node
	}
	if wanted["status"] {
		status := mockStatus
		if lookup.User == s.opts.AccountPhone {
			status, _ = s.settings.aboutText()
		}
		content = append(content, waBinary.Node{Tag: "status", Content: []byte(status)})
	}
	if wanted["lid"] {
		content = append(content, waBinary.Node{
			Tag:   "lid",
			Attrs: waBinary.Attrs{"val": lidFor(lookup)},
		})
	}
	if wanted["devices"] {
		content = append(content, waBinary.Node{
			Tag: "devices",
			Content: []waBinary.Node{{
				Tag:     "device-list",
				Content: []waBinary.Node{{Tag: "device", Attrs: waBinary.Attrs{"id": "0"}}},
			}},
		})
	}
	node.Content = content
	return node
}

// mockStatus is the about line every contact in a mock world carries. A blank
// one would be indistinguishable from a lookup that failed.
const mockStatus = "mocked by whatevrd"

func nodeText(content any) string {
	switch value := content.(type) {
	case []byte:
		return string(value)
	case string:
		return value
	default:
		return ""
	}
}

// phoneToJID recovers a number from the string IsOnWhatsApp puts in <contact>,
// which is the caller's raw input with an @c.us suffix.
func phoneToJID(queried string) types.JID {
	digits := make([]byte, 0, len(queried))
	for i := 0; i < len(queried); i++ {
		if queried[i] == '@' {
			break
		}
		if queried[i] >= '0' && queried[i] <= '9' {
			digits = append(digits, queried[i])
		}
	}
	if len(digits) == 0 {
		return types.JID{}
	}
	return types.JID{User: string(digits), Server: types.DefaultUserServer}
}
