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
		return s.sendNode(ctx, iqResult(node))
	}
	s.srv.capturePreKeys(node)
	return s.sendNode(ctx, iqResult(node))
}
