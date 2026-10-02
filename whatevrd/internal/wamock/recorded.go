//go:build whatevr_mock

package wamock

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"go.mau.fi/libsignal/protocol"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/capture"
)

// RecordedScenario is the run --mock-capture makes: no world of its own, the
// server plays back one segment of a capture.
const RecordedScenario = "recorded"

func init() {
	Register(Scenario{Name: RecordedScenario, Description: "play back a capture (--mock-capture, --mock-segment)"})
}

const (
	// replayAttachWait is how long a block waits for the client to connect.
	replayAttachWait = 90 * time.Second
	// replayIdle is how quiet the wire has to be before a frontend request,
	// so the daemon has taken in what came before it.
	replayIdle    = 150 * time.Millisecond
	replayIdleMax = 5 * time.Second
)

// replay plays one segment. a segment is one daemon run: the frontend lines
// and the websocket connections it made, each connection a block that starts
// at success (or at pair-device, a block the mock's own pairing replaces).
type replay struct {
	srv   *Server
	cap   *capture.Capture
	seg   int
	acct  capture.Account
	pn    types.JID
	lid   types.JID
	items []replayItem

	answers   *answerBook
	decrypted map[uint64]map[int]*capture.Payload
	// gens is the identity generation of each payload's sender, see identities
	gens     map[uint64]map[int]int
	http     map[string][]*capture.HTTP
	httpUsed map[string]int
	httpMu   sync.Mutex
	front    *frontendPlayer

	sessions chan *session
	cur      *session

	countMu  sync.Mutex
	have     map[string]int
	want     map[string]int
	forgiven map[string]int
	wake     chan struct{}

	// the client picks new ids for what it sends, the capture's pushes still
	// name the old ones: receipts, replies, the phone answering a request.
	// sent queues the recorded ids per recipient, ids maps old to new.
	idMu  sync.Mutex
	sent  map[string][]string
	ids   map[string]string
	newID map[string]bool
	ackT  map[string]string

	// what the control socket reports
	pos, total                 atomic.Int64
	misses, gateTimeouts, junk atomic.Int64
	done                       atomic.Bool
}

type replayItem struct {
	rec     *capture.Record
	node    *waBinary.Node
	block   int
	pairing bool
	// answer is a recv that answered a recorded request, it goes out only
	// when the replayed client asks the same thing
	answer bool
	// first is the block's opening stanza, sent again if the client
	// reconnects inside the block
	first bool
}

func (r *replay) log() *zerolog.Logger { return &r.srv.log }

func loadReplay(srv *Server) (*replay, error) {
	c, err := capture.Load(srv.opts.Capture)
	if err != nil {
		return nil, err
	}
	seg := srv.opts.Segment
	if seg == 0 {
		seg = 1
	}
	found := false
	for _, n := range c.Segments {
		found = found || n == seg
	}
	if !found {
		return nil, fmt.Errorf("%s has no segment %d (it has %v)", c.Dir, seg, c.Segments)
	}
	acct, err := c.Account(seg)
	if err != nil {
		return nil, err
	}
	pn, err := types.ParseJID(acct.PN)
	if err != nil {
		return nil, fmt.Errorf("account pn %q: %w", acct.PN, err)
	}
	lid, _ := types.ParseJID(acct.LID)
	recs, err := c.Records(seg)
	if err != nil {
		return nil, err
	}
	r := &replay{
		srv: srv, cap: c, seg: seg, acct: acct, pn: pn, lid: lid,
		answers:   newAnswerBook(),
		decrypted: map[uint64]map[int]*capture.Payload{},
		gens:      map[uint64]map[int]int{},
		http:      map[string][]*capture.HTTP{},
		httpUsed:  map[string]int{},
		sessions:  make(chan *session, 8),
		have:      map[string]int{}, want: map[string]int{}, forgiven: map[string]int{},
		wake: make(chan struct{}, 1),
		sent: map[string][]string{}, ids: map[string]string{}, newID: map[string]bool{}, ackT: map[string]string{},
	}
	if err := r.index(recs); err != nil {
		return nil, err
	}
	if err := r.identities(recs); err != nil {
		return nil, err
	}
	// http answers come from every segment: a body fetched once is cached
	// by the original daemon and may be asked for in a later run of a replay
	for _, n := range c.Segments {
		other := recs
		if n != seg {
			if other, err = c.Records(n); err != nil {
				return nil, err
			}
		}
		for i := range other {
			if h := other[i].HTTP; h != nil && h.Err == "" {
				k := httpKey(h.Method, h.URL, h.Range)
				r.http[k] = append(r.http[k], h)
			}
		}
	}
	r.front = newFrontendPlayer(r, srv.opts.Socket, recs)
	return r, nil
}

func (r *replay) index(recs []capture.Record) error {
	requests := map[string]*waBinary.Node{}
	block, pairing := 0, false
	for i := range recs {
		rec := &recs[i]
		switch rec.Kind {
		case capture.KindDecrypted:
			if rec.Payload.Ref != 0 {
				m := r.decrypted[rec.Payload.Ref]
				if m == nil {
					m = map[int]*capture.Payload{}
					r.decrypted[rec.Payload.Ref] = m
				}
				m[rec.Payload.Child] = rec.Payload
			}
		case capture.KindFrontend:
			r.items = append(r.items, replayItem{rec: rec})
		case capture.KindSend:
			node, err := waBinary.Unmarshal(rec.Frame.Data)
			if err != nil {
				return fmt.Errorf("segment %d record %d: %w", r.seg, rec.Seq, err)
			}
			id, _ := node.Attrs["id"].(string)
			switch node.Tag {
			case "iq":
				if t, _ := node.Attrs["type"].(string); t == "get" || t == "set" {
					requests[id] = node
				}
			case "message":
				to := fmt.Sprint(node.Attrs["to"])
				if q := r.sent[to]; len(q) == 0 || q[len(q)-1] != id {
					r.sent[to] = append(q, id)
				}
			}
			r.items = append(r.items, replayItem{rec: rec, node: node})
		case capture.KindRecv:
			node, err := waBinary.Unmarshal(rec.Frame.Data)
			if err != nil {
				return fmt.Errorf("segment %d record %d: %w", r.seg, rec.Seq, err)
			}
			it := replayItem{rec: rec, node: node}
			switch {
			case node.Tag == "success" || node.Tag == "failure":
				block, pairing = block+1, false
				it.first = true
			case node.Tag == "iq" && hasChild(node, "pair-device"):
				block, pairing = block+1, true
			}
			it.block, it.pairing = block, pairing
			if node.Tag == "ack" {
				if id, _ := node.Attrs["id"].(string); id != "" {
					r.ackT[id], _ = node.Attrs["t"].(string)
				}
			}
			if node.Tag == "iq" {
				t, _ := node.Attrs["type"].(string)
				id, _ := node.Attrs["id"].(string)
				if t == "result" || t == "error" {
					it.answer = true
					if req := requests[id]; req != nil {
						r.answers.add(requestKey(req), rec.Frame.Data)
					}
				}
			}
			if block == 0 {
				// before any connection opened, nothing to play it on
				continue
			}
			r.items = append(r.items, it)
		}
	}
	r.total.Store(int64(len(r.items)))
	return nil
}

func hasChild(n *waBinary.Node, tag string) bool {
	_, ok := n.GetOptionalChildByTag(tag)
	return ok
}

func httpKey(method, raw, rng string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return method + " " + raw + "|" + rng
	}
	return method + " " + strings.ToLower(u.Host) + u.RequestURI() + "|" + rng
}

// hosts are the recorded http hosts, the dial guard lets them through.
func (r *replay) hosts() []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range r.http {
		u, err := url.Parse(list[0].URL)
		if err != nil || seen[u.Hostname()] {
			continue
		}
		seen[u.Hostname()] = true
		out = append(out, u.Hostname())
	}
	return out
}

// serveHTTP answers a request the capture saw, false when it did not.
func (r *replay) serveHTTP(w http.ResponseWriter, req *http.Request) bool {
	k := httpKey(req.Method, "https://"+req.Host+req.URL.RequestURI(), req.Header.Get("Range"))
	r.httpMu.Lock()
	list := r.http[k]
	i := r.httpUsed[k]
	if i >= len(list) {
		i = len(list) - 1
	}
	r.httpUsed[k]++
	r.httpMu.Unlock()
	if len(list) == 0 {
		return false
	}
	h := list[i]
	var body []byte
	if h.Blob != "" {
		var err error
		if body, err = r.cap.Blob(h.Blob); err != nil {
			r.log().Warn().Err(err).Str("url", h.URL).Msg("replay blob")
			http.Error(w, "blob missing", http.StatusNotFound)
			return true
		}
	} else if h.Size > 0 {
		// over the cap at capture time, nothing to serve
		http.NotFound(w, req)
		return true
	}
	for k, v := range h.Header {
		w.Header().Set(k, v)
	}
	w.WriteHeader(h.Status)
	w.Write(body)
	return true
}

// attach is the login half of onConnected under a replay: the engine sends
// success, not the session.
func (r *replay) attach(ctx context.Context, s *session) error {
	go s.pumpOutbox(ctx)
	s.srv.setLive(s)
	s.srv.quiet.setLoggedIn()
	select {
	case r.sessions <- s:
	default:
		return fmt.Errorf("too many connections waiting for the replay")
	}
	return nil
}

// noteSent counts what the replayed client sends, for the gates.
func (r *replay) noteSent(n *waBinary.Node) {
	if n.Tag == "message" {
		r.learnID(n)
	}
	class := stanzaClass(n)
	if class == "" {
		return
	}
	r.countMu.Lock()
	r.have[class]++
	r.countMu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// learnID pairs a message the client sends with the next one the capture
// sent to the same recipient. a retry reuses its id and learns nothing.
func (r *replay) learnID(n *waBinary.Node) {
	id, _ := n.Attrs["id"].(string)
	to := fmt.Sprint(n.Attrs["to"])
	r.idMu.Lock()
	defer r.idMu.Unlock()
	if id == "" || r.newID[id] {
		return
	}
	r.newID[id] = true
	q := r.sent[to]
	if len(q) == 0 {
		return
	}
	r.sent[to] = q[1:]
	if q[0] != id {
		r.ids[q[0]] = id
	}
}

// ackTime is the server time the capture's ack gave the message the client
// just sent, so a replayed send lands at the recorded instant.
func (r *replay) ackTime(newID string) (string, bool) {
	r.idMu.Lock()
	defer r.idMu.Unlock()
	old := newID
	for o, n := range r.ids {
		if n == newID {
			old = o
			break
		}
	}
	t, ok := r.ackT[old]
	return t, ok && t != ""
}

// rewriteIDs swaps every recorded id the client has replaced, in attributes
// anywhere in the stanza. it reports whether anything changed.
func (r *replay) rewriteIDs(n *waBinary.Node) bool {
	r.idMu.Lock()
	defer r.idMu.Unlock()
	if len(r.ids) == 0 {
		return false
	}
	return r.rewriteLocked(n)
}

func (r *replay) rewriteLocked(n *waBinary.Node) bool {
	changed := false
	for k, v := range n.Attrs {
		if s, ok := v.(string); ok {
			if m, ok := r.ids[s]; ok {
				n.Attrs[k] = m
				changed = true
			}
		}
	}
	if children, ok := n.Content.([]waBinary.Node); ok {
		for i := range children {
			changed = r.rewriteLocked(&children[i]) || changed
		}
	}
	return changed
}

// rewritePlaintext does the same inside a payload. ids of one length only:
// a protobuf string cannot change length in place.
func (r *replay) rewritePlaintext(p []byte) []byte {
	r.idMu.Lock()
	defer r.idMu.Unlock()
	for old, n := range r.ids {
		if len(old) == len(n) && bytes.Contains(p, []byte(old)) {
			p = bytes.ReplaceAll(p, []byte(old), []byte(n))
		}
	}
	return p
}

// run plays the segment. it is the only goroutine that writes pushes.
func (r *replay) run(ctx context.Context) {
	defer r.finish()
	if len(r.items) == 0 {
		return
	}
	wallStart, recStart := time.Now(), r.items[0].rec.T
	block := 0
	var blockFirst *replayItem
	r.log().Info().Str("capture", r.cap.Dir).Int("segment", r.seg).Int("items", len(r.items)).Str("account", r.pn.String()).Msg("replay starting")
	for i := range r.items {
		if ctx.Err() != nil {
			return
		}
		r.pos.Store(int64(i))
		it := &r.items[i]
		r.pace(ctx, wallStart, recStart, it.rec.T)
		switch it.rec.Kind {
		case capture.KindSend:
			if class := stanzaClass(it.node); class != "" {
				r.countMu.Lock()
				r.want[class]++
				r.countMu.Unlock()
			}
		case capture.KindFrontend:
			if it.rec.Frontend.Dir == capture.FrontendReq {
				r.settle(ctx)
			}
			r.front.play(ctx, it.rec.Frontend)
		case capture.KindRecv:
			if it.answer {
				continue
			}
			if it.pairing {
				if it.block != block {
					block = it.block
					r.awaitPairing(ctx)
				}
				continue
			}
			if it.block != block {
				block, blockFirst = it.block, it
				if !r.switchTo(ctx) {
					return
				}
			}
			r.gate(ctx)
			if !r.ensureLive(ctx, blockFirst, it) {
				return
			}
			r.push(ctx, it)
		}
	}
	r.settle(ctx)
}

func (r *replay) finish() {
	r.pos.Store(r.total.Load())
	r.done.Store(true)
	r.srv.quiet.touch()
	r.log().Info().Int64("misses", r.misses.Load()).Int64("gate_timeouts", r.gateTimeouts.Load()).
		Int64("junk", r.junk.Load()).Msg("replay finished")
}

func (r *replay) pace(ctx context.Context, wallStart, recStart, at time.Time) {
	if r.srv.opts.Speed <= 0 {
		return
	}
	target := wallStart.Add(time.Duration(float64(at.Sub(recStart)) / r.srv.opts.Speed))
	if d := time.Until(target); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
}

// settle waits for the outbox to drain and the wire to go quiet.
func (r *replay) settle(ctx context.Context) {
	deadline := time.Now().Add(replayIdleMax)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if r.srv.quiet.wireBlockedOn(replayIdle) == "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// gate holds a push until the client sent what it had sent by then. a class
// the client never catches up on is forgiven for the rest of the segment.
func (r *replay) gate(ctx context.Context) {
	timeout := r.srv.opts.Gate
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		r.countMu.Lock()
		var behind []string
		for class, n := range r.want {
			if r.have[class]+r.forgiven[class] < n {
				behind = append(behind, class)
			}
		}
		if len(behind) == 0 {
			r.countMu.Unlock()
			return
		}
		if time.Now().After(deadline) {
			for _, class := range behind {
				r.forgiven[class] = r.want[class] - r.have[class]
			}
			r.countMu.Unlock()
			r.gateTimeouts.Add(1)
			r.log().Warn().Strs("classes", behind).Msg("replay gate timed out, the client sent less than the capture")
			return
		}
		r.countMu.Unlock()
		select {
		case <-r.wake:
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}

func (r *replay) awaitPairing(ctx context.Context) {
	if _, ok := r.srv.Paired(); ok {
		// the capture paired again after a logout
		r.srv.forgetPairing()
	}
	deadline := time.Now().Add(replayAttachWait)
	for ctx.Err() == nil && time.Now().Before(deadline) {
		if _, ok := r.srv.Paired(); ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.log().Warn().Msg("replay waited for a pairing that never came")
}

// switchTo ends the connection of the last block and takes the next one.
func (r *replay) switchTo(ctx context.Context) bool {
	if r.cur != nil {
		r.cur.close(websocket.StatusGoingAway, "replay: next connection")
		r.cur = nil
	}
	return r.takeSession(ctx)
}

func (r *replay) takeSession(ctx context.Context) bool {
	timer := time.NewTimer(replayAttachWait)
	defer timer.Stop()
	for {
		select {
		case s := <-r.sessions:
			if s.isClosed() {
				continue
			}
			r.cur = s
			return true
		case <-timer.C:
			r.log().Error().Msg("replay: the client never connected for the next block")
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// ensureLive takes a new connection when the client dropped the old one in
// the middle of a block, and logs it in again with the block's first stanza.
func (r *replay) ensureLive(ctx context.Context, first, it *replayItem) bool {
	if r.cur != nil && !r.cur.isClosed() {
		return true
	}
	r.log().Warn().Int("block", it.block).Msg("replay: client reconnected inside a block")
	if !r.takeSession(ctx) {
		return false
	}
	if first != nil && first != it {
		r.push(ctx, first)
	}
	return true
}

func (r *replay) push(ctx context.Context, it *replayItem) {
	s := r.cur
	if it.node.Tag == "message" && hasChild(it.node, "enc") {
		// nothing can be encrypted to the client before it uploaded its keys
		if err := r.srv.awaitClientKeys(ctx); err != nil {
			r.log().Warn().Err(err).Msg("replay message without client keys")
		}
		s.enqueue(func(ctx context.Context) error {
			node, err := r.reencrypt(ctx, s, it)
			if err != nil {
				return fmt.Errorf("re-encrypt %s: %w", it.rec.Frame.ID, err)
			}
			return s.sendNode(ctx, node)
		})
		return
	}
	data := it.rec.Frame.Data
	s.enqueue(func(ctx context.Context) error {
		if node, err := waBinary.Unmarshal(data); err == nil && r.rewriteIDs(node) {
			return s.sendNode(ctx, *node)
		}
		return s.sendRaw(ctx, data)
	})
}

// reencrypt swaps each <enc> for the same plaintext under the mock's own
// session with the client. an skmsg goes out pairwise, the mock holds no
// sender keys. an <enc> the original client never opened goes out as junk,
// so the replayed one fails the same way.
func (r *replay) reencrypt(ctx context.Context, s *session, it *replayItem) (waBinary.Node, error) {
	node := *it.node
	node.Attrs = waBinary.Attrs{}
	for k, v := range it.node.Attrs {
		node.Attrs[k] = v
	}
	r.rewriteIDs(&node)
	children := append([]waBinary.Node(nil), it.node.GetChildren()...)
	payloads := r.decrypted[it.rec.Seq]
	for i := range children {
		child := children[i]
		if child.Tag != "enc" {
			continue
		}
		attrs := waBinary.Attrs{}
		for k, v := range child.Attrs {
			attrs[k] = v
		}
		p := payloads[i]
		if p == nil {
			r.junk.Add(1)
			children[i] = waBinary.Node{Tag: "enc", Attrs: attrs, Content: r.srv.rng.bytes(64)}
			continue
		}
		addr, err := types.ParseJID(p.Addr)
		if err != nil {
			return node, fmt.Errorf("payload address %q: %w", p.Addr, err)
		}
		peer, err := r.srv.replayPeer(addr, r.gens[it.rec.Seq][i])
		if err != nil {
			return node, err
		}
		plaintext := r.rewritePlaintext(append([]byte(nil), p.Data...))
		if v, _ := attrs["v"].(string); v != "3" {
			plaintext = padMessage(plaintext, r.srv.rng)
		}
		enc, err := peer.encryptPadded(ctx, r.srv, s.jid, plaintext)
		if err != nil {
			return node, err
		}
		attrs["type"] = enc.Attrs["type"]
		children[i] = waBinary.Node{Tag: "enc", Attrs: attrs, Content: enc.Content}
	}
	node.Content = children
	return node, nil
}

// answer is a recorded reply to the client's request, false when the capture
// never saw it asked.
func (r *replay) answer(ctx context.Context, s *session, req *waBinary.Node) bool {
	frame, ok := r.answers.take(requestKey(req))
	if !ok {
		return false
	}
	resp, err := waBinary.Unmarshal(frame)
	if err != nil {
		return false
	}
	resp.Attrs["id"] = req.Attrs["id"]
	if err := s.sendNode(ctx, *resp); err != nil {
		r.log().Warn().Err(err).Msg("replay answer")
	}
	return true
}

func (r *replay) missed(req *waBinary.Node) {
	r.misses.Add(1)
	r.log().Warn().Str("request", requestKey(req)).Msg("replay has no recorded answer, the mock makes one up")
}

// peerSeed gives every replayed device the same keys in every run, so a
// daemon restarted between segments still trusts them.
func (r *replay) peerSeed(addr string) int64 {
	h := fnv.New64a()
	h.Write([]byte(addr))
	return r.srv.opts.Seed ^ int64(h.Sum64())
}

// isAccount is any device of the captured account, by phone number or lid.
func (r *replay) isAccount(jid types.JID) bool {
	return (jid.Server == types.DefaultUserServer && jid.User == r.pn.User) ||
		(jid.Server == types.HiddenUserServer && jid.User == r.lid.User)
}

func (r *replay) status() controlResponse {
	r.idMu.Lock()
	ids := make(map[string]string, len(r.ids))
	for k, v := range r.ids {
		ids[k] = v
	}
	r.idMu.Unlock()
	return controlResponse{
		IDs:          ids,
		OK:           r.done.Load(),
		Pos:          r.pos.Load(),
		Total:        r.total.Load(),
		Misses:       r.misses.Load(),
		GateTimeouts: r.gateTimeouts.Load(),
		Junk:         r.junk.Load(),
	}
}

// holding keeps the quiescence barrier shut until the segment is played.
func (r *replay) holding() string {
	if r.done.Load() {
		return ""
	}
	return fmt.Sprintf("replay at %d of %d", r.pos.Load(), r.total.Load())
}

// answerIQ answers a client query from the capture. prekeys and pairing stay
// the mock's own: the keys it hands out are the ones it encrypts with.
func (r *replay) answerIQ(ctx context.Context, s *session, node *waBinary.Node) bool {
	t, _ := node.Attrs["type"].(string)
	xmlns, _ := node.Attrs["xmlns"].(string)
	// pings depend on how long the run lasts, the mock's answer is the real one
	if (t != "get" && t != "set") || xmlns == "encrypt" || xmlns == "md" || xmlns == "w:p" {
		return false
	}
	if r.answer(ctx, s, node) {
		return true
	}
	r.missed(node)
	if xmlns != "w:sync:app:state" {
		return false
	}
	// the mock's own app state is under a key this client never got, so
	// say nothing changed rather than hand it patches it cannot open
	sync, _ := node.GetOptionalChildByTag("sync")
	var collections []waBinary.Node
	for _, c := range sync.GetChildrenByTag("collection") {
		collections = append(collections, waBinary.Node{Tag: "collection", Attrs: waBinary.Attrs{
			"name": c.Attrs["name"], "version": c.Attrs["version"],
		}})
	}
	if err := s.sendNode(ctx, iqResult(node, waBinary.Node{Tag: "sync", Content: collections})); err != nil {
		r.log().Warn().Err(err).Msg("replay answer")
	}
	return true
}

// identities finds the contacts that reinstalled. their pkmsg carries a new
// identity key, and the mock gives the address a new identity of its own from
// then on, so the client sees the change it saw when the capture was taken.
// every earlier segment counts, the generation has to carry across restarts.
func (r *replay) identities(current []capture.Record) error {
	last := map[string][32]byte{}
	gen := map[string]int{}
	for _, n := range r.cap.Segments {
		if n > r.seg {
			break
		}
		recs := current
		if n != r.seg {
			var err error
			if recs, err = r.cap.Records(n); err != nil {
				return err
			}
		}
		payloads := map[uint64]map[int]*capture.Payload{}
		for i := range recs {
			if p := recs[i].Payload; recs[i].Kind == capture.KindDecrypted && p.Ref != 0 {
				if payloads[p.Ref] == nil {
					payloads[p.Ref] = map[int]*capture.Payload{}
				}
				payloads[p.Ref][p.Child] = p
			}
		}
		for i := range recs {
			rec := &recs[i]
			if rec.Kind != capture.KindRecv || rec.Frame.Tag != "message" || payloads[rec.Seq] == nil {
				continue
			}
			node, err := waBinary.Unmarshal(rec.Frame.Data)
			if err != nil {
				continue
			}
			for child, enc := range node.GetChildren() {
				p := payloads[rec.Seq][child]
				if p == nil {
					continue
				}
				addr, err := types.ParseJID(p.Addr)
				if err != nil || r.isAccount(addr) {
					continue
				}
				if t, _ := enc.Attrs["type"].(string); t == "pkmsg" {
					if id, ok := pkmsgIdentity(enc.Content); ok {
						if prev, seen := last[p.Addr]; seen && prev != id {
							gen[p.Addr]++
						}
						last[p.Addr] = id
					}
				}
				if n == r.seg {
					if r.gens[rec.Seq] == nil {
						r.gens[rec.Seq] = map[int]int{}
					}
					r.gens[rec.Seq][child] = gen[p.Addr]
				}
			}
		}
	}
	return nil
}

func pkmsgIdentity(content any) ([32]byte, bool) {
	raw, ok := content.([]byte)
	if !ok {
		return [32]byte{}, false
	}
	msg, err := protocol.NewPreKeySignalMessageFromBytes(raw, pbSerializer.PreKeySignalMessage, pbSerializer.SignalMessage)
	if err != nil {
		return [32]byte{}, false
	}
	return msg.IdentityKey().PublicKey().PublicKey(), true
}

// replayPeer is the device at addr in its gen-th identity.
func (s *Server) replayPeer(addr types.JID, gen int) (*peer, error) {
	key := addr.String()
	if gen > 0 {
		key = fmt.Sprintf("%s#%d", key, gen)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peerByKeyLocked(key, addr, addr)
}
