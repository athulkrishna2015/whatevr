package ingest

import (
	"context"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/core"
)

// lids sits in front of whatsmeow's lid map, so every pairing whatsmeow
// learns, from whatever corner of the protocol, is in the log first.
type lids struct {
	store.LIDStore
	g *Ingest
}

// wrapLIDs puts the wrapper in front of cli's lid map, again if need be:
// saving the device at pairing hands it the container's own map, and every
// pair learned after that would skip the log.
func (g *Ingest) wrapLIDs(cli *whatsmeow.Client) {
	if _, ok := cli.Store.LIDs.(*lids); !ok {
		cli.Store.LIDs = &lids{LIDStore: cli.Store.LIDs, g: g}
	}
}

func (l *lids) PutLIDMapping(ctx context.Context, lid, pn types.JID) error {
	return l.PutManyLIDMappings(ctx, []store.LIDMapping{{LID: lid, PN: pn}})
}

func (l *lids) PutManyLIDMappings(ctx context.Context, mappings []store.LIDMapping) error {
	var ins []core.Input
	for _, m := range mappings {
		if m.LID.IsEmpty() || m.PN.IsEmpty() {
			continue
		}
		// whatsmeow hands the same pair over with every message; only what
		// is new goes in the log
		if known, err := l.LIDStore.GetPNForLID(ctx, m.LID); err == nil && known.ToNonAD() == m.PN.ToNonAD() {
			continue
		}
		in, err := input(core.KindLIDMapping, core.LIDMappingHead{LID: jid(m.LID.ToNonAD()), PN: jid(m.PN.ToNonAD())}, nil)
		if err != nil {
			return err
		}
		ins = append(ins, in)
	}
	if len(ins) > 0 {
		if _, err := l.g.log.AppendBatch(l.g.ctx, ins); err != nil {
			return err
		}
	}
	return l.LIDStore.PutManyLIDMappings(ctx, mappings)
}

// selfInput is this account's own pair, logged on every login: pairing may
// have moved it, and a log rebuilt elsewhere needs to know whose it is.
func selfInput(dev *store.Device) (core.Input, bool) {
	pn, lid := dev.GetJID(), dev.GetLID()
	if pn.IsEmpty() {
		return core.Input{}, false
	}
	in, err := input(core.KindLIDMapping, core.LIDMappingHead{LID: jid(lid.ToNonAD()), PN: jid(pn.ToNonAD()), Self: true}, nil)
	return in, err == nil
}
