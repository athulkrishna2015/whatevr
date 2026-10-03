package wa

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

// Instrument is what a capture run hangs on each whatsmeow client and on the
// daemon's own http clients. the zero value changes nothing.
type Instrument struct {
	// Client runs on every new whatsmeow client, before it connects.
	Client func(*whatsmeow.Client)
	// Transport carries the daemon's own http traffic (avatars, stickers,
	// maps, media streams, the version fetch). nil is http.DefaultTransport.
	Transport http.RoundTripper
	// KeepHistoryMedia leaves history blobs on the server: under --core new
	// the new core downloads each one too, and must not find it gone.
	KeepHistoryMedia bool
	// Connection takes connecting over: no supervisor runs here and nothing
	// here says what state the connection is in, the login flow aside.
	Connection Connection
	// Wipe runs with the account wipe of a logout, for state kept elsewhere.
	Wipe func(ctx context.Context) error
	// Outbox decides what may go out: only a send it was told of in Queued
	// does.
	Outbox Outbox
	// Guard is the daemon's half of the send guard for message sends that
	// skip the queue (reactions, edits, revokes, votes, pins). nil lets all
	// through.
	Guard func(ctx context.Context, chat string) error
	// Media hears how each media fetch ended: nil when it worked, the error
	// when it failed for a reason that is not about that one message.
	Media func(err error)
}

// Outbox is the record of sends this daemon queued, kept outside this
// package. MaySend returns ErrAlreadySent for one that went out.
type Outbox interface {
	Queued(ctx context.Context, chat, id string, body *waE2E.Message, file string) error
	MaySend(ctx context.Context, chat, id string) error
	Attempted(ctx context.Context, chat, id string, err error, final bool)
}

// ErrAlreadySent is a queued send the outbox has seen go out.
var ErrAlreadySent = errors.New("already sent")

// Connection is whoever owns the socket when it is not this package.
type Connection interface {
	// Reconnect drops the socket and connects again, asked for by a person.
	Reconnect()
	// WantOnline is a send waiting on the socket.
	WantOnline()
}

// External says the socket is someone else's.
func (c *Client) External() bool { return c.instrument.Connection != nil }

// httpTransport is set once in New, before anything makes a client.
var httpTransport http.RoundTripper

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: httpTransport}
}
