package main

import (
	"net/http"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
)

// usesFlag reports whether the command line has -name or any -name-*.
func usesFlag(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == "--" {
			break
		}
		arg = strings.TrimLeft(arg, "-")
		if arg == name || strings.HasPrefix(arg, name+"=") || strings.HasPrefix(arg, name+"-") {
			return true
		}
	}
	return false
}

// mockClocks are the clocks a mock run's daemon reads instead of the
// machine's: wall for timers and backoff, stamp for what the core stamps on
// arrivals. nil outside a mock run.
type mockClocks struct {
	wall, stamp func() time.Time
}

// clientHooks is what a capture or the send guard hangs on every whatsmeow
// client, and the transport the daemon's own http goes through. zero is none.
type clientHooks struct {
	client    func(*whatsmeow.Client)
	transport http.RoundTripper
}

// qrSource is where a mock run's phone reads the QR it scans.
type qrSource interface {
	QRCodes() (<-chan string, func())
}

// tap sees every frame on the socket, for captures.
type tap = func(conn uint64, dir string, frame []byte)
