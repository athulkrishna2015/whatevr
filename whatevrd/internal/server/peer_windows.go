//go:build windows

package server

import "net"

// Named pipes are ACL-protected by the runtime; there is no Unix peer uid.
func samePeer(net.Conn) error { return nil }
