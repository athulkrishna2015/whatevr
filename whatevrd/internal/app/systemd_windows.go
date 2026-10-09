//go:build windows

package app

import "net"

// Windows does not use systemd socket activation.
func SystemdListener() (net.Listener, error) { return nil, nil }
