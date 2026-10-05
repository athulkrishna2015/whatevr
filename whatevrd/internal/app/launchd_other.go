//go:build !darwin

package app

import "net"

// LaunchdListener is macOS only.
func LaunchdListener() (net.Listener, error) { return nil, nil }
