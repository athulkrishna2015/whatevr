package whatsapp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"whatevrd/internal/core"
	"whatevrd/internal/ingest"
	"whatevrd/internal/model"
	"whatevrd/internal/turbojpeg"
)

// Ref names a message the way the model does: the address it came under and
// whatsapp's id.
type Ref struct{ Chat, ID string }

// maxSendBytes is whatsapp's own cap on a document
const maxSendBytes = 2 << 30

// sendJID is where a chat's messages go: a group as itself, a person by
// phone number when we know it, whatsmeow picks the addressing from there.
func (c *Client) sendJID(chat string) (types.JID, error) {
	w, err := c.world()
	if err != nil {
		return types.JID{}, err
	}
	key := w.Now(model.Norm(chat))
	to := key
	if !model.IsGroup(key) {
		if pn := w.PN(key); pn != "" {
			to = pn
		}
	}
	j, err := types.ParseJID(to)
	if err != nil {
		return types.JID{}, Errorf(ErrInvalid, "chat %q", chat)
	}
	return j, nil
}

// guard is the daemon's half of the send guard, for what skips the queue.
func (c *Client) guard(ctx context.Context, to types.JID) error {
	if err := c.ingest.Guard(ctx, to.ToNonAD().String()); err != nil {
		if errors.Is(err, ingest.ErrGuarded) {
			return Errorf(ErrGuarded, "%s is not on the send guard's list", to)
		}
		return err
	}
	return nil
}

// message is a message the model has.
func (c *Client) message(ctx context.Context, ref Ref) (model.Message, *model.World, error) {
	w, err := c.world()
	if err != nil {
		return model.Message{}, nil, err
	}
	m, ok, err := c.r.Message(ctx, w.Addrs(w.Now(model.Norm(ref.Chat))), ref.ID)
	if err != nil {
		return model.Message{}, nil, err
	}
	if !ok {
		return model.Message{}, nil, Errorf(ErrNotFound, "no message %s/%s", ref.Chat, ref.ID)
	}
	return m, w, nil
}

// Edits returns a message's superseded bodies, oldest first.
func (c *Client) Edits(ctx context.Context, addrs []string, id string) ([]model.Edit, error) {
	return c.r.Edits(ctx, addrs, id)
}

// Draft is what a send says besides its body.
type Draft struct {
	Chat     string
	ReplyTo  *Ref
	Mentions []string
	Once     Once
}

// Once is a send's key and a digest of the rest of its params: a second send
// under the key queues nothing and answers what the first queued.
type Once = ingest.Once

// turns makes sends under one key go one at a time, so a repeat sees what the
// first queued.
type turns struct {
	mu sync.Mutex
	on map[string]chan struct{}
}

func (t *turns) take(ctx context.Context, key string) (func(), error) {
	for {
		t.mu.Lock()
		wait, busy := t.on[key]
		if !busy {
			if t.on == nil {
				t.on = map[string]chan struct{}{}
			}
			done := make(chan struct{})
			t.on[key] = done
			t.mu.Unlock()
			return func() {
				t.mu.Lock()
				delete(t.on, key)
				t.mu.Unlock()
				close(done)
			}, nil
		}
		t.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// earlier is what sends under o's key already queued, oldest first. the key
// with other params is ErrInvalid.
func (c *Client) earlier(ctx context.Context, o Once) ([]Ref, error) {
	// the first one's queue may still be folding
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.ingest.Folded(wctx); err != nil {
		return nil, err
	}
	out, params, err := c.r.Keyed(ctx, o.Key)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 && params != o.Params {
		return nil, Errorf(ErrInvalid, "key %q was used for a different send", o.Key)
	}
	refs := make([]Ref, len(out))
	for i, x := range out {
		refs[i] = Ref{Chat: x.Chat, ID: x.ID}
	}
	return refs, nil
}

// once runs send unless a send under o's key already queued, and then
// answers that one.
func (c *Client) once(ctx context.Context, o Once, send func() (Ref, error)) (Ref, error) {
	if o.Key == "" {
		return send()
	}
	done, err := c.turns.take(ctx, o.Key)
	if err != nil {
		return Ref{}, err
	}
	defer done()
	refs, err := c.earlier(ctx, o)
	if err != nil || len(refs) > 0 {
		return first(refs), err
	}
	return send()
}

func first(refs []Ref) Ref {
	if len(refs) == 0 {
		return Ref{}
	}
	return refs[0]
}

// context is the reply and mentions a draft carries, nil for none.
func (c *Client) context(ctx context.Context, cli *whatsmeow.Client, d Draft) (*waE2E.ContextInfo, error) {
	var ci *waE2E.ContextInfo
	if d.ReplyTo != nil {
		m, w, err := c.message(ctx, *d.ReplyTo)
		if err != nil {
			return nil, err
		}
		if w.Now(model.Norm(m.Chat)) != w.Now(model.Norm(d.Chat)) && m.Chat != statusChat {
			// a status answers in its author's DM, carrying the status as
			// its quote the way official clients reply to stories
			return nil, Errorf(ErrInvalid, "the reply is not in this chat")
		}
		ci = &waE2E.ContextInfo{StanzaID: proto.String(m.ID), QuotedMessage: quoted(m)}
		if p := c.quotedSender(cli, w, m); p != "" {
			ci.Participant = proto.String(p)
		}
	}
	if len(d.Mentions) > 0 {
		if ci == nil {
			ci = &waE2E.ContextInfo{}
		}
		w, err := c.world()
		if err != nil {
			return nil, err
		}
		for _, k := range d.Mentions {
			j := k
			if pn := w.PN(k); pn != "" && !model.IsGroup(d.Chat) {
				j = pn
			}
			ci.MentionedJID = append(ci.MentionedJID, j)
		}
	}
	return ci, nil
}

// quotedSender is the participant a quote names: the member as the group
// knows them, a person by number in a direct chat.
func (c *Client) quotedSender(cli *whatsmeow.Client, w *model.World, m model.Message) string {
	if m.FromMe {
		if m.Sender != "" && m.Sender != model.Me && model.IsGroup(m.Chat) {
			return m.Sender
		}
		if cli.Store.ID != nil {
			return cli.Store.ID.ToNonAD().String()
		}
		return ""
	}
	s := m.Sender
	if s == "" {
		s = m.Chat
	}
	if !model.IsGroup(m.Chat) {
		if pn := w.PN(w.Now(model.Norm(s))); pn != "" {
			return pn
		}
	}
	return s
}

// quoted is the body a reply embeds: the original's content, its own
// context taken off.
func quoted(m model.Message) *waE2E.Message {
	raw, _ := m.Content()
	if raw == nil {
		return &waE2E.Message{Conversation: proto.String(m.Text)}
	}
	q := proto.Clone(model.Unwrap(raw).Msg).(*waE2E.Message)
	stripContext(q)
	return q
}

// stripContext clears contextInfo on every content field of m.
func stripContext(m *waE2E.Message) {
	r := m.ProtoReflect()
	r.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		sub := v.Message()
		if ci := sub.Descriptor().Fields().ByName("contextInfo"); ci != nil {
			sub.Clear(ci)
		}
		return true
	})
}

// queue logs a send and wakes the sender. the token it returns is final:
// the sent message comes back under the same address and id.
func (c *Client) queue(ctx context.Context, cli *whatsmeow.Client, to types.JID, body *waE2E.Message, file string, o Once) (Ref, error) {
	chat := to.ToNonAD().String()
	if err := c.guard(ctx, to); err != nil {
		return Ref{}, err
	}
	id := string(cli.GenerateMessageID())
	if err := c.ingest.Queued(ctx, chat, id, body, file, o); err != nil {
		return Ref{}, err
	}
	c.signalSender()
	c.avatars.want(chat)
	return Ref{Chat: chat, ID: id}, nil
}

// SendText queues a text message.
func (c *Client) SendText(ctx context.Context, d Draft, text string) (Ref, error) {
	return c.once(ctx, d.Once, func() (Ref, error) { return c.sendText(ctx, d, text) })
}

func (c *Client) sendText(ctx context.Context, d Draft, text string) (Ref, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return Ref{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Ref{}, Errorf(ErrInvalid, "text is required")
	}
	to, err := c.sendJID(d.Chat)
	if err != nil {
		return Ref{}, err
	}
	ci, err := c.context(ctx, cli, d)
	if err != nil {
		return Ref{}, err
	}
	body := &waE2E.Message{Conversation: proto.String(text)}
	if ci != nil {
		body = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: ci}}
	}
	return c.queue(ctx, cli, to, body, "", d.Once)
}

// SendMedia queues a file: a picture, video or audio as itself, anything
// else, or anything asked to, as a document. the file is copied first, the
// caller may drop it once this returns.
func (c *Client) SendMedia(ctx context.Context, d Draft, path, caption string, asDocument, viewOnce bool) (Ref, error) {
	return c.once(ctx, d.Once, func() (Ref, error) { return c.sendMedia(ctx, d, path, caption, asDocument, viewOnce) })
}

func (c *Client) sendMedia(ctx context.Context, d Draft, path, caption string, asDocument, viewOnce bool) (Ref, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return Ref{}, err
	}
	to, err := c.sendJID(d.Chat)
	if err != nil {
		return Ref{}, err
	}
	ci, err := c.context(ctx, cli, d)
	if err != nil {
		return Ref{}, err
	}
	src, size, err := openOutbound(path)
	if err != nil {
		return Ref{}, err
	}
	defer src.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	mimeType := http.DetectContentType(head[:n])
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" && (mimeType == "application/octet-stream" || strings.HasPrefix(mimeType, "text/plain")) {
		mimeType = t
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return Ref{}, err
	}
	kind := sendKind(mimeType, asDocument)
	if viewOnce && kind == sendDocument {
		return Ref{}, Errorf(ErrInvalid, "a document can't be view once")
	}
	dir := filepath.Join(c.o.Paths.MediaCacheDir, "sent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Ref{}, err
	}
	tmp, err := os.CreateTemp(dir, "send-*"+filepath.Ext(path))
	if err != nil {
		return Ref{}, err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return Ref{}, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return Ref{}, err
	}
	body := c.mediaBody(ctx, kind, tmp.Name(), mimeType, filepath.Base(path), caption, uint64(size), viewOnce, ci)
	ref, err := c.queue(ctx, cli, to, body, tmp.Name(), d.Once)
	if err != nil {
		os.Remove(tmp.Name())
	}
	return ref, err
}

type sendAs int

const (
	sendImage sendAs = iota
	sendVideo
	sendAudio
	sendDocument
)

func sendKind(mimeType string, asDocument bool) sendAs {
	switch {
	case asDocument:
		return sendDocument
	case mimeType == "image/jpeg", mimeType == "image/png", mimeType == "image/webp":
		return sendImage
	case mimeType == "video/mp4", mimeType == "video/3gpp":
		return sendVideo
	case strings.HasPrefix(mimeType, "audio/"):
		return sendAudio
	}
	// a gif is a short mp4 to whatsapp; without a transcode it goes as what
	// it is
	return sendDocument
}

// mediaBody is a media message with everything but the upload's keys,
// which the sender fills in.
func (c *Client) mediaBody(ctx context.Context, kind sendAs, path, mimeType, name, caption string, size uint64, viewOnce bool, ci *waE2E.ContextInfo) *waE2E.Message {
	var capt *string
	if caption != "" {
		capt = proto.String(caption)
	}
	vo := proto.Bool(viewOnce)
	if !viewOnce {
		vo = nil
	}
	switch kind {
	case sendImage:
		img := &waE2E.ImageMessage{Mimetype: proto.String(mimeType), Caption: capt, FileLength: proto.Uint64(size), ContextInfo: ci, ViewOnce: vo}
		if src, w, h, err := readImage(path); w > 0 {
			img.Width, img.Height = proto.Uint32(uint32(w)), proto.Uint32(uint32(h))
			if err == nil {
				img.JPEGThumbnail = thumbnail(src)
			}
		}
		return &waE2E.Message{ImageMessage: img}
	case sendVideo:
		v := &waE2E.VideoMessage{Mimetype: proto.String(mimeType), Caption: capt, FileLength: proto.Uint64(size), ContextInfo: ci, ViewOnce: vo}
		if f, ok := probe(ctx, path); ok {
			v.Width, v.Height, v.Seconds = proto.Uint32(f.w), proto.Uint32(f.h), proto.Uint32(f.seconds)
			if src, err := turbojpeg.Decode(f.frame, thumbMax); err == nil {
				v.JPEGThumbnail = thumbnail(src)
			}
		}
		return &waE2E.Message{VideoMessage: v}
	case sendAudio:
		a := &waE2E.AudioMessage{Mimetype: proto.String(mimeType), FileLength: proto.Uint64(size), ContextInfo: ci, ViewOnce: vo}
		if f, ok := probe(ctx, path); ok && f.seconds > 0 {
			a.Seconds = proto.Uint32(f.seconds)
		}
		return &waE2E.Message{AudioMessage: a}
	}
	d := &waE2E.DocumentMessage{Mimetype: proto.String(mimeType), FileName: proto.String(name), Title: proto.String(name),
		Caption: capt, FileLength: proto.Uint64(size), ContextInfo: ci}
	if capt != nil {
		// whatsapp only shows a document's caption from inside this wrapper
		return &waE2E.Message{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{DocumentMessage: d}}}
	}
	return &waE2E.Message{DocumentMessage: d}
}

// openOutbound opens a file a frontend asked to send: an absolute path to a
// regular file of ours, not a link, not empty, not past whatsapp's cap.
func openOutbound(path string) (*os.File, int64, error) {
	if !filepath.IsAbs(path) {
		return nil, 0, Errorf(ErrInvalid, "the path must be absolute")
	}
	info, err := os.Lstat(path)
	switch {
	case err != nil:
		return nil, 0, Errorf(ErrInvalid, "the file is not there")
	case info.Mode()&os.ModeSymlink != 0:
		return nil, 0, Errorf(ErrInvalid, "the file must not be a link")
	case !info.Mode().IsRegular():
		return nil, 0, Errorf(ErrInvalid, "not a regular file")
	case info.Size() == 0:
		return nil, 0, Errorf(ErrInvalid, "the file is empty")
	case info.Size() > maxSendBytes:
		return nil, 0, Errorf(ErrInvalid, "the file is over 2 GiB")
	}
	if !ownedByCurrentUser(info) {
		return nil, 0, Errorf(ErrRejected, "the file belongs to someone else")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, Errorf(ErrInvalid, "the file can't be read")
	}
	return f, info.Size(), nil
}

// SendSticker queues a sticker from the library. an upload of it that is
// still good goes again as is.
func (c *Client) SendSticker(ctx context.Context, d Draft, key string) (Ref, error) {
	return c.once(ctx, d.Once, func() (Ref, error) { return c.sendSticker(ctx, d, key) })
}

func (c *Client) sendSticker(ctx context.Context, d Draft, key string) (Ref, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return Ref{}, err
	}
	to, err := c.sendJID(d.Chat)
	if err != nil {
		return Ref{}, err
	}
	s, ok, err := c.r.Sticker(ctx, key)
	if err != nil {
		return Ref{}, err
	}
	if !ok {
		return Ref{}, Errorf(ErrNotFound, "no sticker %q", key)
	}
	ci, err := c.context(ctx, cli, d)
	if err != nil {
		return Ref{}, err
	}
	var sm waE2E.StickerMessage
	file := ""
	if len(s.UploadPayload) == 0 || time.Since(time.Unix(s.UploadTS, 0)) > stickerUploadGood ||
		proto.Unmarshal(s.UploadPayload, &sm) != nil || sm.GetDirectPath() == "" {
		// a lottie goes out as its archive, not the json we show
		if s, err = c.stickers.file(ctx, s.CacheKey, true); err != nil {
			return Ref{}, err
		}
		lottie := isAnimatedSticker(s.MimeType)
		file = s.LocalPath
		mime := "image/webp"
		if lottie {
			file, mime = s.ArchivePath, s.MimeType
		}
		w, h := uint32(max(s.Width, 0)), uint32(max(s.Height, 0))
		if w == 0 || h == 0 {
			w, h = 512, 512
		}
		sm = waE2E.StickerMessage{Mimetype: proto.String(mime), IsAnimated: proto.Bool(s.IsAnimated || lottie),
			Width: proto.Uint32(w), Height: proto.Uint32(h)}
		if lottie {
			sm.IsLottie = proto.Bool(true)
		}
		// the library's key rides in the hash until the upload puts the real one
		if k, err := hex.DecodeString(s.CacheKey); err == nil {
			sm.FileSHA256 = k
		}
		var orig waE2E.StickerMessage
		if proto.Unmarshal(s.StickerPayload, &orig) == nil {
			sm.PngThumbnail = orig.GetPngThumbnail()
		}
	}
	sm.ContextInfo = ci
	return c.queue(ctx, cli, to, &waE2E.Message{StickerMessage: &sm}, file, d.Once)
}

// SendPoll queues a poll: 2-12 options after trimming and dedup, like
// official clients.
func (c *Client) SendPoll(ctx context.Context, d Draft, question string, options []string, multi bool) (Ref, error) {
	return c.once(ctx, d.Once, func() (Ref, error) { return c.sendPoll(ctx, d, question, options, multi) })
}

func (c *Client) sendPoll(ctx context.Context, d Draft, question string, options []string, multi bool) (Ref, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return Ref{}, err
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return Ref{}, Errorf(ErrInvalid, "poll question is required")
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(options))
	for _, o := range options {
		if o = strings.TrimSpace(o); o == "" || seen[o] {
			continue
		}
		seen[o] = true
		clean = append(clean, o)
	}
	if len(clean) < 2 {
		return Ref{}, Errorf(ErrInvalid, "a poll needs at least two options")
	}
	if len(clean) > 12 {
		return Ref{}, Errorf(ErrInvalid, "a poll holds at most 12 options")
	}
	to, err := c.sendJID(d.Chat)
	if err != nil {
		return Ref{}, err
	}
	ci, err := c.context(ctx, cli, d)
	if err != nil {
		return Ref{}, err
	}
	selectable := 1
	if multi {
		selectable = len(clean)
	}
	body := cli.BuildPollCreation(question, clean, selectable)
	if ci != nil {
		body.GetPollCreationMessage().ContextInfo = ci
	}
	return c.queue(ctx, cli, to, body, "", d.Once)
}

// SendContact queues a contact card: name plus phone, shared as a vCard.
func (c *Client) SendContact(ctx context.Context, d Draft, name, phone string) (Ref, error) {
	return c.once(ctx, d.Once, func() (Ref, error) { return c.sendContact(ctx, d, name, phone) })
}

func (c *Client) sendContact(ctx context.Context, d Draft, name, phone string) (Ref, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return Ref{}, err
	}
	name, phone = strings.TrimSpace(name), strings.TrimSpace(phone)
	if name == "" {
		return Ref{}, Errorf(ErrInvalid, "contact name is required")
	}
	if phone == "" {
		return Ref{}, Errorf(ErrInvalid, "contact phone is required")
	}
	to, err := c.sendJID(d.Chat)
	if err != nil {
		return Ref{}, err
	}
	ci, err := c.context(ctx, cli, d)
	if err != nil {
		return Ref{}, err
	}
	vcard := fmt.Sprintf("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:%s\r\nTEL;TYPE=CELL:%s\r\nEND:VCARD",
		escapeVCardValue(name), escapeVCardValue(phone))
	body := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String(name), Vcard: proto.String(vcard), ContextInfo: ci}}
	return c.queue(ctx, cli, to, body, "", d.Once)
}

// escapeVCardValue escapes a vCard 3.0 text value.
func escapeVCardValue(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, ",", `\,`, ";", `\;`).Replace(s)
}

// SendLocation queues a location pin: coordinates plus an optional place
// name and address.
func (c *Client) SendLocation(ctx context.Context, d Draft, lat, lng float64, name, address string) (Ref, error) {
	return c.once(ctx, d.Once, func() (Ref, error) { return c.sendLocation(ctx, d, lat, lng, name, address) })
}

func (c *Client) sendLocation(ctx context.Context, d Draft, lat, lng float64, name, address string) (Ref, error) {
	cli, err := c.loggedIn()
	if err != nil {
		return Ref{}, err
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return Ref{}, Errorf(ErrInvalid, "coordinates out of range")
	}
	to, err := c.sendJID(d.Chat)
	if err != nil {
		return Ref{}, err
	}
	ci, err := c.context(ctx, cli, d)
	if err != nil {
		return Ref{}, err
	}
	loc := &waE2E.LocationMessage{
		DegreesLatitude: proto.Float64(lat), DegreesLongitude: proto.Float64(lng), ContextInfo: ci}
	if name = strings.TrimSpace(name); name != "" {
		loc.Name = proto.String(name)
	}
	if address = strings.TrimSpace(address); address != "" {
		loc.Address = proto.String(address)
	}
	return c.queue(ctx, cli, to, &waE2E.Message{LocationMessage: loc}, "", d.Once)
}

// stickerUploadGood is how long an upload of ours is sent again rather than
// made anew: the media servers keep a file about a month
const stickerUploadGood = 14 * 24 * time.Hour

func (c *Client) signalSender() {
	select {
	case c.sends <- struct{}{}:
	default:
	}
}

// runSender sends what is queued, oldest first, whenever the socket is up.
// a failed try waits its backoff; every reconnect tries everything again.
func (c *Client) runSender(ctx context.Context) {
	tries := map[Ref]time.Time{}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		next := c.sendDue(ctx, tries)
		timer.Reset(next)
		select {
		case <-ctx.Done():
			return
		case <-c.sends:
			if c.conn.Status().Kind == "online" {
				clear(tries)
			}
		case <-timer.C:
		}
	}
}

// sendDue sends every queued message that is due and says how long until
// the next one is.
func (c *Client) sendDue(ctx context.Context, tries map[Ref]time.Time) time.Duration {
	cli := c.client()
	if cli == nil || !cli.IsLoggedIn() {
		return time.Hour
	}
	// a send queued a moment ago is only in the log until it folds
	if err := c.ingest.Folded(ctx); err != nil {
		return 5 * time.Second
	}
	next := time.Hour
	var after *model.Outgoing
	for {
		page, err := c.r.Unsent(ctx, after, sendPage)
		if err != nil {
			c.log.Warn().Err(err).Msg("whatsapp: read the outbox")
			return 5 * time.Second
		}
		for _, o := range page {
			if ctx.Err() != nil {
				return next
			}
			d, gone := c.sendIfDue(ctx, cli, tries, o)
			if gone {
				return d
			}
			next = min(next, d)
		}
		if len(page) < sendPage {
			return next
		}
		after = &page[len(page)-1]
	}
}

// sendPage is how much of the outbox is read at once
const sendPage = 32

// sendIfDue sends o unless it waits out a backoff, and says how long until
// it is due again. gone is the socket down: stop and wait that long.
func (c *Client) sendIfDue(ctx context.Context, cli *whatsmeow.Client, tries map[Ref]time.Time, o model.Outgoing) (time.Duration, bool) {
	ref := Ref{Chat: o.Chat, ID: o.ID}
	if at, ok := tries[ref]; ok && time.Now().Before(at) {
		return time.Until(at), false
	}
	if !cli.IsConnected() {
		c.conn.Kick("send")
		return time.Hour, true
	}
	err := c.sendOne(ctx, cli, o)
	if err == nil {
		delete(tries, ref)
		return time.Hour, false
	}
	final := errors.Is(err, errFinal)
	c.ingest.Attempted(ctx, o.Chat, o.ID, err, final)
	c.log.Warn().Err(err).Str("chat", o.Chat).Str("id", o.ID).Int("attempt", o.Attempts+1).Bool("final", final).Msg("whatsapp: send")
	if final {
		return time.Hour, false
	}
	d := sendBackoff(o.Attempts + 1)
	tries[ref] = time.Now().Add(d)
	return d, false
}

var errFinal = errors.New("will not go")

func sendBackoff(attempt int) time.Duration {
	d := 10 * time.Second << min(max(attempt-1, 0), 5)
	return min(d, 5*time.Minute)
}

// sendOne uploads a queued send's file into its body and sends it.
func (c *Client) sendOne(ctx context.Context, cli *whatsmeow.Client, o model.Outgoing) error {
	switch err := c.ingest.MaySend(ctx, o.Chat, o.ID); {
	case errors.Is(err, ingest.ErrSent), errors.Is(err, ingest.ErrCancelled):
		return nil
	case errors.Is(err, ingest.ErrGuarded), errors.Is(err, ingest.ErrNotQueued):
		return fmt.Errorf("%w: %v", errFinal, err)
	case err != nil:
		return err
	}
	to, err := types.ParseJID(o.Chat)
	if err != nil {
		return fmt.Errorf("%w: bad chat %q", errFinal, o.Chat)
	}
	var body waE2E.Message
	if proto.Unmarshal(o.Body, &body) != nil {
		return fmt.Errorf("%w: the queued body does not decode", errFinal)
	}
	key := hex.EncodeToString(body.GetStickerMessage().GetFileSHA256())
	if o.File != "" {
		if err := c.upload(ctx, cli, &body, o.File); err != nil {
			return err
		}
	}
	if _, err := cli.SendMessage(ctx, to, &body, whatsmeow.SendRequestExtra{ID: types.MessageID(o.ID)}); err != nil {
		return err
	}
	if o.File != "" && strings.HasPrefix(o.File, filepath.Join(c.o.Paths.MediaCacheDir, "sent")+string(filepath.Separator)) {
		// the sent copy is the message's file from now on
		c.logLocal(ctx, core.LocalHead{Chat: o.Chat, ID: o.ID, Op: core.MediaFile, Path: o.File})
	}
	if sm := body.GetStickerMessage(); sm != nil && o.File != "" {
		c.stickers.uploaded(ctx, key, sm)
	}
	return nil
}

// upload puts file on the media servers and its keys into body's media.
func (c *Client) upload(ctx context.Context, cli *whatsmeow.Client, body *waE2E.Message, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("%w: the file to send is gone: %v", errFinal, err)
	}
	defer f.Close()
	kind := whatsmeow.MediaDocument
	switch {
	case body.ImageMessage != nil:
		kind = whatsmeow.MediaImage
	case body.VideoMessage != nil:
		kind = whatsmeow.MediaVideo
	case body.AudioMessage != nil:
		kind = whatsmeow.MediaAudio
	case body.StickerMessage != nil:
		kind = whatsmeow.MediaImage
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	resp, err := cli.UploadReader(ctx, f, tmp, kind)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	set := func(url, direct *string, key, enc, sha *[]byte, length **uint64) {
		*url, *direct = resp.URL, resp.DirectPath
		*key, *enc, *sha = resp.MediaKey, resp.FileEncSHA256, resp.FileSHA256
		*length = proto.Uint64(resp.FileLength)
	}
	switch {
	case body.ImageMessage != nil:
		m := body.ImageMessage
		m.URL, m.DirectPath = new(string), new(string)
		set(m.URL, m.DirectPath, &m.MediaKey, &m.FileEncSHA256, &m.FileSHA256, &m.FileLength)
	case body.VideoMessage != nil:
		m := body.VideoMessage
		m.URL, m.DirectPath = new(string), new(string)
		set(m.URL, m.DirectPath, &m.MediaKey, &m.FileEncSHA256, &m.FileSHA256, &m.FileLength)
	case body.AudioMessage != nil:
		m := body.AudioMessage
		m.URL, m.DirectPath = new(string), new(string)
		set(m.URL, m.DirectPath, &m.MediaKey, &m.FileEncSHA256, &m.FileSHA256, &m.FileLength)
	case body.StickerMessage != nil:
		m := body.StickerMessage
		m.URL, m.DirectPath = new(string), new(string)
		set(m.URL, m.DirectPath, &m.MediaKey, &m.FileEncSHA256, &m.FileSHA256, &m.FileLength)
	default:
		m := body.GetDocumentMessage()
		if m == nil {
			m = body.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage()
		}
		if m == nil {
			return fmt.Errorf("%w: a file with no media to put it in", errFinal)
		}
		m.URL, m.DirectPath = new(string), new(string)
		set(m.URL, m.DirectPath, &m.MediaKey, &m.FileEncSHA256, &m.FileSHA256, &m.FileLength)
	}
	return nil
}

// Cancel takes a queued send back, if it has not gone out.
func (c *Client) Cancel(ctx context.Context, ref Ref) error {
	return c.ingest.Cancel(ctx, ref.Chat, ref.ID)
}

const thumbMax = 100

// imageCap is the most pixels a png is decoded at for its thumbnail: go's
// decoder holds all of them, 64 MiB of rgba at the cap. a jpeg shrinks while
// it decodes, so it goes much further, see turbojpeg.
const imageCap = 16 << 20

// readImage is the image at path, at least thumbMax on its longer side and
// small enough to hold, and its size. the size comes off the header, so it is
// there even when the image is too big to decode.
func readImage(path string) (image.Image, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(bufio.NewReader(f))
	if err != nil {
		return nil, 0, 0, err
	}
	if format == "jpeg" {
		src, err := turbojpeg.DecodeFile(path, thumbMax)
		return src, cfg.Width, cfg.Height, err
	}
	if cfg.Width*cfg.Height > imageCap {
		return nil, cfg.Width, cfg.Height, fmt.Errorf("%dx%d is past %d pixels", cfg.Width, cfg.Height, imageCap)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, 0, err
	}
	src, _, err := image.Decode(bufio.NewReader(f))
	return src, cfg.Width, cfg.Height, err
}

// thumbnail is the small jpeg whatsapp shows while the full file loads. a
// box average is plenty at this size.
func thumbnail(src image.Image) []byte {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return nil
	}
	dw, dh := sw, sh
	if sw > thumbMax || sh > thumbMax {
		if sw >= sh {
			dw, dh = thumbMax, max(sh*thumbMax/sw, 1)
		} else {
			dw, dh = max(sw*thumbMax/sh, 1), thumbMax
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := range dh {
		y0, y1 := b.Min.Y+dy*sh/dh, b.Min.Y+(dy+1)*sh/dh
		y1 = max(y1, y0+1)
		for dx := range dw {
			x0, x1 := b.Min.X+dx*sw/dw, b.Min.X+(dx+1)*sw/dw
			x1 = max(x1, x0+1)
			var r, g, bl, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					cr, cg, cb, _ := src.At(x, y).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			dst.SetRGBA(dx, dy, color.RGBA{R: uint8((r / n) >> 8), G: uint8((g / n) >> 8), B: uint8((bl / n) >> 8), A: 0xff})
		}
	}
	var buf bytes.Buffer
	if jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 70}) != nil {
		return nil
	}
	return buf.Bytes()
}
