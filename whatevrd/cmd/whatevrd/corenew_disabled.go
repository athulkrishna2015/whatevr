//go:build !whatevr_core

package main

import (
	"context"

	"github.com/rs/zerolog"

	"whatevrd/internal/app"
	"whatevrd/internal/protocol"
	"whatevrd/internal/store"
	"whatevrd/internal/wa"
)

type coreFlagSet struct{}

// coreFlags registers nothing in a release build: the new core is not in it
// until it replaces the old one. say so instead of a bare unknown flag.
func coreFlags(log zerolog.Logger) *coreFlagSet {
	if usesFlag("core") {
		log.Fatal().Msg("this whatevrd was built without the new core; rebuild with -tags whatevr_core")
	}
	return nil
}

type newCore struct{}

func coreOpen(context.Context, zerolog.Logger, *coreFlagSet, app.Paths, *app.Daemon, *store.DB, *mockClocks) *newCore {
	return nil
}

func (*newCore) instrument(inst wa.Instrument) wa.Instrument { return inst }

func (*newCore) register(_ context.Context, s *protocol.Server, daemon *app.Daemon, old *store.DB, waClient *wa.Client) {
	protocol.RegisterDaemonViews(s, daemon, old, waClient)
	protocol.RegisterDaemonCommands(s, waClient)
}

func (*newCore) close() {}
