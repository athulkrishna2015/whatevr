package capture

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"weak"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Recorder is the glue between the fork's hooks and a Writer.
type Recorder struct {
	w   *Writer
	log zerolog.Logger

	// message stanzas waiting for their decrypted payloads, so a payload can
	// point at the recv record it came in. weak, so a dropped stanza is not
	// kept alive by the capture.
	mu    sync.Mutex
	nodes map[weak.Pointer[waBinary.Node]]uint64

	errOnce sync.Once
}

func NewRecorder(w *Writer, log zerolog.Logger) *Recorder {
	return &Recorder{w: w, log: log, nodes: map[weak.Pointer[waBinary.Node]]uint64{}}
}

func (r *Recorder) Writer() *Writer { return r.w }

func (r *Recorder) write(rec Record) uint64 {
	seq := r.w.Write(rec)
	if err := r.w.Err(); err != nil {
		r.errOnce.Do(func() { r.log.Error().Err(err).Msg("capture write failed, the capture is incomplete from here") })
	}
	return seq
}

// Hook puts every capture hook on cli, call it for each new client.
func (r *Recorder) Hook(cli *whatsmeow.Client) {
	cli.RawNodeHandler = r.rawNode
	cli.SentNodeHandler = r.sentNode
	cli.DecryptedPayloadHandler = r.decrypted
	cli.EncryptedPayloadHandler = r.encrypted
	cli.DecryptedMediaHandler = r.media
	// the same base whatsmeow clones, which is the mock's under --mock
	base := http.DefaultTransport.(*http.Transport).Clone()
	cli.SetMediaHTTPClient(&http.Client{Transport: r.Transport(base)})
	cli.AddEventHandler(func(evt any) { r.event(cli, evt) })
}

func frameOf(node *waBinary.Node, data []byte) *Frame {
	id, _ := node.Attrs["id"].(string)
	return &Frame{Tag: node.Tag, ID: id, Data: append([]byte(nil), data...)}
}

func (r *Recorder) rawNode(_ context.Context, raw whatsmeow.RawNode) (*waBinary.Node, bool) {
	seq := r.write(Record{Kind: KindRecv, Frame: frameOf(raw.Node, raw.Frame)})
	if raw.Node.Tag == "message" {
		r.mu.Lock()
		if len(r.nodes) >= 4096 {
			for k := range r.nodes {
				if k.Value() == nil {
					delete(r.nodes, k)
				}
			}
		}
		r.nodes[weak.Make(raw.Node)] = seq
		r.mu.Unlock()
	}
	return nil, false
}

func (r *Recorder) sentNode(_ context.Context, sent whatsmeow.SentNode) {
	r.write(Record{Kind: KindSend, Frame: frameOf(sent.Node, sent.Frame)})
}

// encAddress is the address whatsmeow opened the payload under, the same
// pick decryptMessages makes.
func encAddress(info *types.MessageInfo) types.JID {
	if info.Sender.Server == types.DefaultUserServer && !info.Sender.IsBot() && info.SenderAlt.Server == types.HiddenUserServer {
		return info.SenderAlt
	}
	return info.Sender
}

func (r *Recorder) decrypted(_ context.Context, p whatsmeow.DecryptedPayload) {
	r.mu.Lock()
	ref := r.nodes[weak.Make(p.Node)]
	r.mu.Unlock()
	r.write(Record{Kind: KindDecrypted, Payload: &Payload{
		Ref:   ref,
		Child: p.ChildIndex,
		Addr:  encAddress(p.Info).String(),
		Enc:   p.EncType,
		Data:  append([]byte(nil), p.Plaintext...),
	}})
}

func (r *Recorder) encrypted(_ context.Context, p whatsmeow.EncryptedPayload) {
	sum := sha256.Sum256(p.Ciphertext)
	r.write(Record{Kind: KindEncrypted, Payload: &Payload{
		To:           p.To.String(),
		CipherSHA256: sum[:],
		Enc:          p.EncType,
		Data:         append([]byte(nil), p.Plaintext...),
	}})
}

func (r *Recorder) media(_ context.Context, m whatsmeow.DecryptedMedia) {
	rec := &Media{DirectPath: m.DirectPath, Type: string(m.MediaType), FileSHA256: m.FileSHA256}
	b, err := r.w.NewBlob()
	if err != nil {
		r.log.Warn().Err(err).Msg("capture media blob")
		return
	}
	if m.File != nil {
		_, err = io.Copy(b, io.NewSectionReader(m.File, 0, m.Size))
	} else {
		_, err = b.Write(m.Data)
	}
	if err != nil {
		b.Abort()
		r.log.Warn().Err(err).Msg("capture media blob")
		return
	}
	rec.Size = b.Size()
	if rec.Blob, _, err = b.Finish(); err != nil {
		r.log.Warn().Err(err).Msg("capture media blob")
	}
	r.write(Record{Kind: KindMedia, Media: rec})
}

func (r *Recorder) event(cli *whatsmeow.Client, evt any) {
	switch e := evt.(type) {
	case *events.PairSuccess:
		r.write(Record{Kind: KindAccount, Account: &Account{PN: e.ID.String(), LID: e.LID.String(), Platform: e.Platform, Business: e.BusinessName}})
	case *events.Connected:
		if id := cli.Store.GetJID(); !id.IsEmpty() {
			r.write(Record{Kind: KindAccount, Account: &Account{
				PN: id.String(), LID: cli.Store.GetLID().String(), PushName: cli.Store.PushName, Platform: cli.Store.Platform, Business: cli.Store.BusinessName,
			}})
		}
		r.write(Record{Kind: KindConn, Conn: &Conn{Event: "connected"}})
	case *events.Disconnected:
		r.write(Record{Kind: KindConn, Conn: &Conn{Event: "disconnected"}})
	case *events.StreamReplaced:
		r.write(Record{Kind: KindConn, Conn: &Conn{Event: "stream_replaced"}})
	case *events.LoggedOut:
		r.write(Record{Kind: KindConn, Conn: &Conn{Event: "logged_out", Detail: e.Reason.String()}})
	case *events.ConnectFailure:
		r.write(Record{Kind: KindConn, Conn: &Conn{Event: "connect_failure", Detail: e.Reason.String()}})
	case *events.StreamError:
		r.write(Record{Kind: KindConn, Conn: &Conn{Event: "stream_error", Detail: e.Code}})
	case *events.KeepAliveTimeout:
		r.write(Record{Kind: KindConn, Conn: &Conn{Event: "keepalive_timeout", Detail: fmt.Sprint(e.ErrorCount)}})
	}
}

// Frontend takes one line to or from a frontend connection, dir is one of
// the Frontend* constants. open and close carry no line.
func (r *Recorder) Frontend(conn int64, dir string, line []byte) {
	rec := &Frontend{Conn: conn, Dir: dir}
	if len(line) > 0 {
		if json.Valid(line) {
			rec.Line = append(json.RawMessage(nil), line...)
		} else {
			rec.Line, _ = json.Marshal(string(line))
		}
	}
	r.write(Record{Kind: KindFrontend, Frontend: rec})
}

// Transport records every exchange that goes through base. nil is whatever
// http.DefaultTransport is at the time of the request, which under --mock is
// only the mock's once it started.
func (r *Recorder) Transport(base http.RoundTripper) http.RoundTripper {
	return &recordingTransport{r: r, base: base}
}

type recordingTransport struct {
	r    *Recorder
	base http.RoundTripper
}

// keptHeaders are what a replay needs to answer the same way.
var keptHeaders = []string{"Content-Type", "Content-Range", "Accept-Ranges", "Location", "Etag", "Last-Modified"}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := &HTTP{Method: req.Method, URL: req.URL.String(), Range: req.Header.Get("Range"), ReqSize: req.ContentLength}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		rec.Err = err.Error()
		t.r.write(Record{Kind: KindHTTP, HTTP: rec})
		return nil, err
	}
	rec.Status = resp.StatusCode
	for _, h := range keptHeaders {
		if v := resp.Header.Get(h); v != "" {
			if rec.Header == nil {
				rec.Header = map[string]string{}
			}
			rec.Header[h] = v
		}
	}
	b, err := t.r.w.NewBlob()
	if err != nil {
		t.r.log.Warn().Err(err).Msg("capture http blob")
		rec.Err = "not recorded: " + err.Error()
		t.r.write(Record{Kind: KindHTTP, HTTP: rec})
		return resp, nil
	}
	resp.Body = &teeBody{ReadCloser: resp.Body, t: t, blob: b, rec: rec}
	return resp, nil
}

type teeBody struct {
	io.ReadCloser
	t    *recordingTransport
	blob *BlobWriter
	rec  *HTTP
	once sync.Once
}

func (b *teeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.blob.Write(p[:n])
	}
	if err == io.EOF {
		b.finish(false)
	} else if err != nil {
		b.rec.Err = err.Error()
		b.finish(true)
	}
	return n, err
}

func (b *teeBody) Close() error {
	b.finish(true)
	return b.ReadCloser.Close()
}

func (b *teeBody) finish(partial bool) {
	b.once.Do(func() {
		b.rec.Partial = partial
		b.rec.Size = b.blob.Size()
		var err error
		if b.rec.Blob, _, err = b.blob.Finish(); err != nil {
			b.t.r.log.Warn().Err(err).Msg("capture http blob")
		}
		b.t.r.write(Record{Kind: KindHTTP, HTTP: b.rec})
	})
}
