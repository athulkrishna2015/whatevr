//go:build windows

package whatsapp

import "os"

// Windows file access is enforced by the current user's filesystem ACL.
func ownedByCurrentUser(os.FileInfo) bool { return true }
