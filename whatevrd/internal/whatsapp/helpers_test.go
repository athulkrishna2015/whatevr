package whatsapp

import (
	"testing"

	"github.com/nyaruka/phonenumbers"
	"go.mau.fi/whatsmeow/types"
)

// testNames knows nobody: what the old d said with no session
type testNames struct{}

func (testNames) Norm(j types.JID) types.JID { return j }
func (testNames) Own(types.JID) bool         { return false }
func (testNames) Name(j types.JID) string {
	if j.Server != types.DefaultUserServer || j.User == "" {
		return j.User
	}
	n, err := phonenumbers.Parse("+"+j.User, "ZZ")
	if err != nil || !phonenumbers.IsValidNumber(n) {
		return "+" + j.User
	}
	return phonenumbers.Format(n, phonenumbers.INTERNATIONAL)
}

func newTestDecoder(t *testing.T) *Decoder {
	t.Helper()
	return NewDecoder(testNames{}, t.TempDir())
}
