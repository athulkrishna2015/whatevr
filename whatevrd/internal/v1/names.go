package v1

import (
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/model"
	"whatevrd/internal/whatsapp"
)

// names is the World as the decoder asks it.
type names struct{ w *model.World }

func (n names) Norm(j types.JID) types.JID {
	if j.Server != types.HiddenUserServer {
		return j
	}
	if pn := n.w.PN(n.w.Now(j.ToNonAD().String())); pn != "" {
		return jid(pn)
	}
	return j
}

// Name falls back to the bare user of anything but a lid, a bot's number
// say, the way the old core did.
func (n names) Name(j types.JID) string {
	if name, _ := n.w.Name(n.w.Now(j.ToNonAD().String())); name != "" || j.Server == types.HiddenUserServer {
		return name
	}
	return j.User
}

func (n names) Own(j types.JID) bool { return !j.IsEmpty() && n.w.IsSelf(j.ToNonAD().String()) }

func (a *Adapter) decoder(w *model.World) *whatsapp.Decoder {
	return whatsapp.NewDecoder(names{w}, a.mediaDir)
}
