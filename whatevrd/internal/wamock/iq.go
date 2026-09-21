//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// iqResult builds the envelope every successful info query gets back. The
// response waiter matches on id alone, but whatsmeow rejects anything that is
// not an <iq> of type result or error.
func iqResult(req *waBinary.Node, content ...waBinary.Node) waBinary.Node {
	ag := req.AttrGetter()
	from := ag.OptionalJIDOrEmpty("to")
	if from.IsEmpty() {
		from = types.ServerJID
	}
	node := waBinary.Node{
		Tag: "iq",
		Attrs: waBinary.Attrs{
			"id":   ag.String("id"),
			"type": "result",
			"from": from,
		},
	}
	if len(content) > 0 {
		node.Content = content
	}
	return node
}

func iqError(req *waBinary.Node, code int, text string) waBinary.Node {
	ag := req.AttrGetter()
	return waBinary.Node{
		Tag: "iq",
		Attrs: waBinary.Attrs{
			"id":   ag.String("id"),
			"type": "error",
			"from": types.ServerJID,
		},
		Content: []waBinary.Node{{
			Tag:   "error",
			Attrs: waBinary.Attrs{"code": fmt.Sprintf("%d", code), "text": text},
		}},
	}
}

func (s *session) handleIQ(ctx context.Context, node *waBinary.Node) error {
	ag := node.AttrGetter()
	namespace := ag.OptionalString("xmlns")
	iqType := ag.OptionalString("type")

	// A result or error coming the other way is the client answering something
	// we asked, not a query for us. Pairing is the only thing that does this
	// today.
	if iqType == "result" || iqType == "error" {
		return s.handleIQResponse(ctx, node)
	}

	switch namespace {
	case "encrypt":
		return s.handleEncryptIQ(ctx, node)
	case "passive":
		return s.sendNode(ctx, iqResult(node))
	case "w:g2":
		return s.handleGroupIQ(ctx, node)
	case "usync":
		return s.handleUsyncIQ(ctx, node)
	case "w:profile:picture":
		return s.handleProfilePictureIQ(ctx, node)
	case "w:m":
		return s.handleMediaConnIQ(ctx, node)
	case "w:sync:app:state":
		return s.handleAppStateIQ(ctx, node)
	case "privacy":
		return s.handlePrivacyIQ(ctx, node)
	case "blocklist":
		return s.handleBlocklistIQ(ctx, node)
	case "urn:xmpp:whatsapp:dirty":
		return s.handleDirtyIQ(ctx, node)
	case "w:mex":
		return s.handleMexIQ(ctx, node)
	case "status":
		return s.handleStatusIQ(ctx, node)
	case "w:p":
		return s.handlePingIQ(ctx, node)
	case "md":
		return s.handleCompanionIQ(ctx, node)
	default:
		// Answering rather than dropping matters: an unanswered info query
		// stalls the daemon for the full 60s command timeout. Later stages
		// replace these with real data, and the log line is how we find them.
		s.srv.log.Printf("unanswered iq xmlns=%q type=%q, replying empty", namespace, iqType)
		return s.sendNode(ctx, iqResult(node))
	}
}

// handleEncryptIQ covers the two prekey queries whatsmeow makes right after
// <success>. The upload is also where the server learns the client's identity
// and prekeys, which is what later stages need to encrypt anything to it.
func (s *session) handleEncryptIQ(ctx context.Context, node *waBinary.Node) error {
	if node.AttrGetter().OptionalString("type") == "get" {
		if _, ok := node.GetOptionalChildByTag("count"); ok {
			return s.sendNode(ctx, iqResult(node, waBinary.Node{
				Tag:   "count",
				Attrs: waBinary.Attrs{"value": fmt.Sprintf("%d", s.srv.preKeyCount())},
			}))
		}
		if key, ok := node.GetOptionalChildByTag("key"); ok {
			return s.sendNode(ctx, iqResult(node, s.srv.preKeyBundles(&key)))
		}
		return s.sendNode(ctx, iqResult(node))
	}
	s.srv.capturePreKeys(node)
	return s.sendNode(ctx, iqResult(node))
}

// preKeyBundles answers the client's request for somebody else's keys, which is
// what it needs before it can send them anything. Every jid the world knows
// about gets a real bundle; anybody else gets the 404 a real server sends.
func (s *Server) preKeyBundles(key *waBinary.Node) waBinary.Node {
	var users []waBinary.Node
	for _, child := range key.GetChildren() {
		if child.Tag != "user" {
			continue
		}
		jid := child.AttrGetter().OptionalJIDOrEmpty("jid")
		p, err := s.peerForRecipient(jid)
		if err != nil {
			users = append(users, waBinary.Node{
				Tag:   "user",
				Attrs: waBinary.Attrs{"jid": jid},
				Content: []waBinary.Node{{
					Tag:   "error",
					Attrs: waBinary.Attrs{"code": "404", "text": "item-not-found"},
				}},
			})
			continue
		}
		users = append(users, p.preKeyBundleNode())
	}
	return waBinary.Node{Tag: "list", Content: users}
}
