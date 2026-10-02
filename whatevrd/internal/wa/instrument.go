package wa

import (
	"net/http"
	"time"

	"go.mau.fi/whatsmeow"
)

// Instrument is what a capture run hangs on each whatsmeow client and on the
// daemon's own http clients. the zero value changes nothing.
type Instrument struct {
	// Client runs on every new whatsmeow client, before it connects.
	Client func(*whatsmeow.Client)
	// Transport carries the daemon's own http traffic (avatars, stickers,
	// maps, media streams, the version fetch). nil is http.DefaultTransport.
	Transport http.RoundTripper
}

// httpTransport is set once in New, before anything makes a client.
var httpTransport http.RoundTripper

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: httpTransport}
}
