// package capture writes and reads captures: everything one account saw and
// did on the wire, segment by segment, enough for wamock to play it back. a
// capture holds the plaintext of every message, it stays on the machine it
// was taken on and never goes in the repo.
package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codelif/whatevr/platform"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
)

const (
	Format = 1

	metaFile    = "capture.json"
	segmentsDir = "segments"
	blobsDir    = "blobs"
)

const (
	KindStart     = "start"
	KindAccount   = "account"
	KindRecv      = "recv"
	KindSend      = "send"
	KindDecrypted = "decrypted"
	KindEncrypted = "encrypted"
	KindMedia     = "media"
	KindHTTP      = "http"
	KindFrontend  = "frontend"
	KindConn      = "conn"
	KindEnd       = "end"
)

// FrameJSON is how a frontend frame is kept: protocol 2 as json, with the
// schema's field names.
var FrameJSON = protojson.MarshalOptions{UseProtoNames: true}

// frontend line directions
const (
	FrontendOpen  = "open"
	FrontendReq   = "req"
	FrontendResp  = "resp"
	FrontendClose = "close"
)

type Meta struct {
	Format  int       `json:"format"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

// Record is one line of a segment. exactly one of the pointers is set, the
// one Kind names.
type Record struct {
	Seq      uint64    `json:"seq"`
	T        time.Time `json:"t"`
	Kind     string    `json:"kind"`
	Start    *Start    `json:"start,omitempty"`
	Account  *Account  `json:"account,omitempty"`
	Frame    *Frame    `json:"frame,omitempty"`
	Payload  *Payload  `json:"payload,omitempty"`
	Media    *Media    `json:"media,omitempty"`
	HTTP     *HTTP     `json:"http,omitempty"`
	Frontend *Frontend `json:"frontend,omitempty"`
	Conn     *Conn     `json:"conn,omitempty"`
}

type Start struct {
	Version string `json:"version"`
	// Run is the logx run id of the same daemon run.
	Run string `json:"run,omitempty"`
	// Mock is the scenario when the run was against wamock, empty on the real server.
	Mock  string `json:"mock,omitempty"`
	Guard bool   `json:"guard"`
	PID   int    `json:"pid"`
}

type Account struct {
	// PN and LID carry the device, 91...:12@s.whatsapp.net
	PN       string `json:"pn"`
	LID      string `json:"lid,omitempty"`
	PushName string `json:"push_name,omitempty"`
	Platform string `json:"platform,omitempty"`
	Business string `json:"business,omitempty"`
}

// Frame is one stanza as binary xmpp, after noise and zlib, without the flag
// byte in front.
type Frame struct {
	Tag  string `json:"tag"`
	ID   string `json:"id,omitempty"`
	Data []byte `json:"data"`
}

// Payload is a decrypted or an encrypted <enc>.
type Payload struct {
	// decrypted: Ref is the recv record holding the stanza (0 when unknown),
	// Child the <enc>'s index among all children, Addr the signal address it
	// opened under.
	Ref   uint64 `json:"ref,omitempty"`
	Child int    `json:"child"`
	Addr  string `json:"addr,omitempty"`
	// encrypted: To is the device, or the group for skmsg.
	To           string `json:"to,omitempty"`
	CipherSHA256 []byte `json:"cipher_sha256,omitempty"`

	Enc  string `json:"enc"`
	Data []byte `json:"data"`
}

type Media struct {
	DirectPath string `json:"direct_path"`
	Type       string `json:"type"`
	FileSHA256 []byte `json:"file_sha256,omitempty"`
	Size       int64  `json:"size"`
	// Blob is empty when the file was over BlobMax.
	Blob string `json:"blob,omitempty"`
}

type HTTP struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Range   string            `json:"range,omitempty"`
	ReqSize int64             `json:"req_size,omitempty"`
	Status  int               `json:"status,omitempty"`
	Header  map[string]string `json:"header,omitempty"`
	Size    int64             `json:"size"`
	Blob    string            `json:"blob,omitempty"`
	// Partial is a body the caller stopped reading early.
	Partial bool   `json:"partial,omitempty"`
	Err     string `json:"err,omitempty"`
}

type Frontend struct {
	Conn int64           `json:"conn"`
	Dir  string          `json:"dir"`
	Line json.RawMessage `json:"line,omitempty"`
}

type Conn struct {
	Event  string `json:"event"`
	Detail string `json:"detail,omitempty"`
}

// BlobMax is the biggest body kept, anything larger keeps size and hash only.
var BlobMax int64 = 256 << 20

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func ValidName(name string) bool { return nameRE.MatchString(name) }

// Root is where named captures live.
func Root(stateHome string) string {
	return platform.CaptureRoot(stateHome)
}

// Resolve takes a name (under Root) or a path (anything with a slash).
func Resolve(arg, stateHome string) (string, error) {
	if strings.ContainsRune(arg, os.PathSeparator) {
		return filepath.Abs(arg)
	}
	if !ValidName(arg) {
		return "", fmt.Errorf("capture name %q: use letters, digits, dot, dash or underscore", arg)
	}
	return filepath.Join(Root(stateHome), arg), nil
}

func readMeta(dir string) (Meta, error) {
	var m Meta
	raw, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("%s: %w", metaFile, err)
	}
	if m.Format != Format {
		return m, fmt.Errorf("capture format %d, this build reads %d", m.Format, Format)
	}
	return m, nil
}

var errNotCapture = errors.New("not a capture (no capture.json)")

func segmentPath(dir string, n int) string {
	return filepath.Join(dir, segmentsDir, fmt.Sprintf("%04d.jsonl", n))
}
