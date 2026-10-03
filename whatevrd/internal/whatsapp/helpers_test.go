package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

// testNames knows nobody: what the old d said with no session
type testNames struct{}

func (testNames) Norm(j types.JID) types.JID { return j }
func (testNames) Own(types.JID) bool         { return false }
func (testNames) Name(j types.JID) string {
	return firstNonEmpty(formatPhoneDisplayName(j), j.User)
}

func newTestDecoder(t *testing.T) *Decoder {
	t.Helper()
	return NewDecoder(testNames{}, t.TempDir())
}
