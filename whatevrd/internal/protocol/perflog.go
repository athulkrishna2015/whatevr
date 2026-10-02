package protocol

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// slowOpenThreshold mirrors store.slowOpThreshold: an Open that blocks this
// long is a latency bug regardless of who asked for it.
const slowOpenThreshold = 100 * time.Millisecond

func logViewOpen(ctx context.Context, start time.Time) {
	d := time.Since(start)
	evt := zerolog.Ctx(ctx).Info()
	if d >= slowOpenThreshold {
		evt = zerolog.Ctx(ctx).Warn()
	}
	evt.Dur("dur", d).Msg("view open")
}

// logRecompute is the per-view diff: counts at info, the ids at debug.
func logRecompute(ctx context.Context, items int, upserted, removed []string, start time.Time) {
	l := zerolog.Ctx(ctx)
	l.Info().Int("items", items).Int("upserts", len(upserted)).Int("removes", len(removed)).
		Dur("dur", time.Since(start)).Msg("recompute")
	if len(upserted)+len(removed) > 0 {
		l.Debug().Strs("upserted", upserted).Strs("removed", removed).Msg("recompute diff")
	}
}
