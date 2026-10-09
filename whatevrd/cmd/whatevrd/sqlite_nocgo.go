//go:build !cgo

package main

// SQLite memory-status configuration is unavailable without cgo.
func memstatusOff() int { return 0 }
