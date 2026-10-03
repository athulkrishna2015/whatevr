package ingest

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

// recovery keeps whatsmeow's app state recovery step in the log, so a
// restart carries on where the last run got to and the step shows up in
// completeness.
func (g *Ingest) recovery() *whatsmeow.AppStateRecovery {
	return &whatsmeow.AppStateRecovery{LoadStep: g.loadStep, SaveStep: g.saveStep}
}

func (g *Ingest) loadStep(ctx context.Context, name appstate.WAPatchName) (whatsmeow.AppStateRecoveryStep, error) {
	if g.db == nil {
		return 0, errors.New("no core to ask")
	}
	if err := g.folded(ctx); err != nil {
		return 0, err
	}
	step, err := model.NewReader(g.db.Read()).RecoveryStep(ctx, string(name))
	return whatsmeow.AppStateRecoveryStep(step), err
}

func (g *Ingest) saveStep(ctx context.Context, name appstate.WAPatchName, step whatsmeow.AppStateRecoveryStep) error {
	n := int(step)
	in, err := input(core.KindSyncState, core.SyncStateHead{Domain: "app_state:" + string(name), Step: &n}, nil)
	if err != nil {
		return err
	}
	_, err = g.log.AppendBatch(ctx, []core.Input{in})
	return err
}

// folded waits until everything appended so far is folded, so a read sees
// what was just written.
func (g *Ingest) folded(ctx context.Context) error {
	_, appended := g.db.Progress()
	if appended == 0 {
		return nil
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return g.db.WaitFolded(wctx, appended)
}
