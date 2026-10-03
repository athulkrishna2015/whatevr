package model

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// trackers is each test db's tracker, found by feed.
var trackers sync.Map

// tracker collects the persons fold commits touched, so feed can check that
// patching the last world with them gives the world a fresh read does.
type tracker struct {
	mu      sync.Mutex
	persons []string
	all     bool
	world   *World
}

func (tr *tracker) change(c core.Change) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.persons = append(tr.persons, c.Keys["person"]...)
	tr.all = tr.all || c.All["person"]
}

func (tr *tracker) checkPatch(t *testing.T, db *core.DB) {
	t.Helper()
	ctx := context.Background()
	// the writer tells of a fold before it takes the next append: once one
	// is in, every change up to seq has been told
	if _, err := db.Append(ctx, core.Input{Kind: "zz_barrier", V: 1}); err != nil {
		t.Fatal(err)
	}
	r := NewReader(db.Read())
	fresh, err := r.World(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.world != nil && !tr.all {
		patched, err := r.Patch(ctx, tr.world, tr.persons)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(norm(patched), norm(fresh)) {
			t.Fatalf("patched world differs after %v:\n  patched %+v\n  fresh   %+v", tr.persons, *patched, *fresh)
		}
	}
	tr.world, tr.persons, tr.all = fresh, nil, false
}

// norm drops empty maps a patch can leave where a fresh world has none.
func norm(w *World) World {
	n := *w
	for _, m := range []*map[string]map[string]string{&n.names} {
		for k, v := range *m {
			if len(v) == 0 {
				delete(*m, k)
			}
		}
	}
	return n
}

// TestPatchFollowsEveryInput feeds the scenario one input at a time, each
// way round, so feed checks a patched world after every one, with a self
// push name and a contact removed on top.
func TestPatchFollowsEveryInput(t *testing.T) {
	more := []core.Input{
		appState("critical_block", 3, "set", []string{"setting_pushName"}, &waSyncAction.SyncActionValue{PushNameSetting: &waSyncAction.PushNameSetting{Name: proto.String("Me")}}, 110),
		appState("critical_unblock_low", 4, "remove", []string{"contact", boPN}, &waSyncAction.SyncActionValue{}, 111),
		in(core.KindLIDMapping, core.LIDMappingHead{LID: "100000000077@lid", PN: boPN}, nil, at(112)),
	}
	for _, order := range [][]core.Input{append(scenario(), more...), reversed(append(scenario(), more...))} {
		db := openModel(t)
		for _, x := range order {
			feed(t, db, []core.Input{x})
		}
	}
}
