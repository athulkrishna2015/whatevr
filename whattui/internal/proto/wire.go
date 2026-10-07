// Package proto speaks protocol 2 to whatevrd: size-delimited protobuf frames
// over a local socket. PROTOCOL.md is the contract this implements.
//
// The package knows about framing, correlation, deadlines and subscriptions.
// It never looks inside an item: what a view's rows mean is the caller's.
package proto

import (
	"fmt"
	"strings"

	"github.com/codelif/whatevr/platform"
	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// ProtocolVersion is the protocol this client speaks.
const ProtocolVersion = 2

// Error is a request that failed. Code is the daemon's, or UNSPECIFIED when
// the client answered itself: no daemon, no answer in time, too much queued.
type Error struct {
	Code    v2.ErrorCode
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == v2.ErrorCode_ERROR_CODE_UNSPECIFIED {
		return e.Message
	}
	code := strings.ToLower(strings.TrimPrefix(e.Code.String(), "ERROR_CODE_"))
	if e.Message == "" {
		return code
	}
	return fmt.Sprintf("%s: %s", code, e.Message)
}

// HasCode reports whether the daemon answered with this code. Nil-safe.
func (e *Error) HasCode(code v2.ErrorCode) bool { return e != nil && e.Code == code }

func daemonError(e *v2.Error) *Error { return &Error{Code: e.GetCode(), Message: e.GetMessage()} }

var (
	errOffline = &Error{Message: "whatevrd is not connected"}
	errBusy    = &Error{Message: "too many requests waiting on whatevrd"}
	errTimeout = &Error{Message: "whatevrd did not answer in time"}
	errLost    = &Error{Message: "connection to whatevrd lost"}
)

// DefaultSocketPath is where whatevrd listens: WHATEVR_SOCKET, or the OS's
// place for it.
func DefaultSocketPath() string {
	path, _ := platform.ClientSocketPath()
	return path
}
