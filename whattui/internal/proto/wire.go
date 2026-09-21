// Package proto speaks the whatevr protocol: newline-delimited JSON over a
// unix socket. See PROTOCOL.md, which is the contract this implements.
//
// The package knows about framing, correlation and subscriptions. It knows
// nothing about what any view means; decoding an item is the caller's job,
// which is what keeps rule 7 (no view is tailored to a frontend) true on this
// side of the socket too.
package proto

import (
	"encoding/json"
	"fmt"
)

// Error is a daemon error response. Code is stable and machine-readable,
// Message is for humans.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// HasCode reports whether the error carries this code. Nil-safe, so a caller
// tests a result without first testing for nil.
func (e *Error) HasCode(code string) bool { return e != nil && e.Code == code }

// The core error codes. Methods may document more; a frontend that does not
// recognise a code shows Message and carries on.
const (
	ErrInvalidRequest = "invalid_request"
	ErrUnknownMethod  = "unknown_method"
	ErrInvalidParams  = "invalid_params"
	ErrNotFound       = "not_found"
	ErrNotLoggedIn    = "not_logged_in"
	ErrNotConnected   = "not_connected"
	ErrAlreadyExists  = "already_exists"
	ErrExpired        = "expired"
	ErrRejected       = "rejected"
	ErrIO             = "io"
	ErrInternal       = "internal"
)

// Params is what a request carries. A nil Params marshals to {}.
type Params map[string]any

// request is one line out.
type request struct {
	ID     uint64 `json:"id"`
	Method string `json:"method"`
	Params Params `json:"params"`
}

// frame is one line in: the routing fields the three wire shapes share. A
// response has ID, an event has Event. Payloads that only some events carry
// are decoded from the raw line by whoever wants them, because `error` is an
// object on a response and a string on media_stream_update and one struct
// cannot hold both.
type frame struct {
	ID     *uint64         `json:"id"`
	Result json.RawMessage `json:"result"`
	Err    *Error          `json:"error"`

	Event     string          `json:"event"`
	Sub       *uint64         `json:"sub"`
	Sort      string          `json:"sort"`
	Item      json.RawMessage `json:"item"`
	Exhausted *bool           `json:"exhausted"`
}

// removeFrame reads the `id` of a remove event. It is its own type because on
// a response `id` is the request id and here it is the item id.
type removeFrame struct {
	ItemID string `json:"id"`
}

// OpenChat asks the frontend to surface a chat: a notification click, or a
// whatevr://chat/... url.
type OpenChat struct {
	ChatID string `json:"chat_id"`
}

// MediaStreamUpdate reports a late failure of a media.stream this connection
// requested. State is "local" (use Path instead) or "failed".
type MediaStreamUpdate struct {
	StreamID  string `json:"stream_id"`
	MessageID string `json:"message_id"`
	State     string `json:"state"`
	Path      string `json:"path"`
	Error     string `json:"error"`
}

// ServerInfo is what hello answered with.
type ServerInfo struct {
	Daemon   string `json:"daemon"`
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
	State    string `json:"state"`
	DataDir  string `json:"data_dir"`
	CacheDir string `json:"cache_dir"`
}

// ProtocolVersion is the version this client speaks. There is exactly one and
// it is an integer.
const ProtocolVersion = 1
