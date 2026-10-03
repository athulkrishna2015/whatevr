package model

import (
	"context"
	"testing"

	"whatevrd/internal/core"
)

func TestCompletenessIsReadOffTheLog(t *testing.T) {
	ctx := context.Background()
	db := openModel(t)
	sync := func(domain, state, msg string, version uint64, at_ int) core.Input {
		return in(core.KindSyncState, core.SyncStateHead{Domain: domain, State: state, Error: msg, Version: version}, nil, at(at_))
	}
	feed(t, db, []core.Input{
		sync("app_state:regular_low", core.SyncComplete, "", 5, 10),
		sync("app_state:regular_high", core.SyncComplete, "", 3, 10),
		sync("app_state:regular_high", core.SyncError, "mismatching LTHash", 0, 20),
		sync("app_state:regular", core.SyncError, "timed out", 0, 10),
		sync("app_state:regular", core.SyncComplete, "", 9, 30),
		in(core.KindHistoryNotification, core.HistoryNotificationHead{ID: "N1", SyncType: "RECENT", ChunkOrder: 1, Progress: 100}, nil, at(40)),
		in(core.KindHistoryNotification, core.HistoryNotificationHead{ID: "N2", SyncType: "FULL", ChunkOrder: 1, Progress: 40}, nil, at(41)),
		in(core.KindHistoryExtra, core.HistoryExtraHead{Notification: "N1", SyncType: "RECENT", ChunkOrder: 1, Progress: 100}, nil, at(42)),
	})
	c, err := NewReader(db.Read()).Completeness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range c.AppState {
		got[d.Domain] = d.State
	}
	want := map[string]string{"regular_low": Known, "regular_high": Unavailable, "regular": Known, "critical_block": Syncing, "critical_unblock_low": Syncing}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s is %s, want %s", k, got[k], v)
		}
	}
	hist := map[string]HistoryState{}
	for _, h := range c.History {
		hist[h.SyncType] = h
	}
	if h := hist["RECENT"]; h.State != Known || h.Progress != 100 {
		t.Errorf("RECENT %+v", h)
	}
	if h := hist["FULL"]; h.State != Syncing || h.Pending != 1 || h.Progress != 40 || h.Last != at(41).UnixMilli() {
		t.Errorf("FULL %+v", h)
	}
	if h := hist["RECENT"]; h.Last != at(42).UnixMilli() {
		t.Errorf("RECENT last %d, want the download at %d", h.Last, at(42).UnixMilli())
	}
}

func TestWaitingCountsOnlyWhatThePhoneCanStillSend(t *testing.T) {
	db := openModel(t)
	und := func(id, unavailable string, sec int) core.Input {
		return in(core.KindUndecryptable, core.UndecryptableHead{Source: core.Source{Chat: ashaPN, Sender: ashaPN}, ID: id,
			T: at(sec).Unix(), UnavailableType: unavailable}, nil, at(sec))
	}
	feed(t, db, []core.Input{und("U1", "", 10), und("U2", "view_once", 5), und("U3", "", 20)})
	n, oldest, err := NewReader(db.Read()).Waiting(context.Background())
	if err != nil || n != 2 || oldest != at(10).UnixMilli() {
		t.Fatalf("waiting %d since %d (%v), want 2 since %d", n, oldest, err, at(10).UnixMilli())
	}
}

func TestRecoveryStepIsTheNewestInAnyOrder(t *testing.T) {
	ctx := context.Background()
	step := func(n, sec int) core.Input {
		return in(core.KindSyncState, core.SyncStateHead{Domain: "app_state:regular_low", Step: &n}, nil, at(sec))
	}
	errAt := in(core.KindSyncState, core.SyncStateHead{Domain: "app_state:regular_low", State: core.SyncError, Error: "mismatching patch MAC"}, nil, at(9))
	for _, order := range [][]core.Input{
		{errAt, step(1, 10), step(2, 20)},
		{step(2, 20), step(1, 10), errAt},
	} {
		db := openModel(t)
		feed(t, db, order)
		r := NewReader(db.Read())
		if n, err := r.RecoveryStep(ctx, "regular_low"); err != nil || n != 2 {
			t.Fatalf("step %d %v, want 2", n, err)
		}
		c, err := r.Completeness(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range c.AppState {
			if d.Domain == "regular_low" && (d.State != Unavailable || d.Recovery != RecoveringFromPhone) {
				t.Fatalf("regular_low %+v", d)
			}
		}
		feed(t, db, []core.Input{step(0, 30)})
		if n, _ := r.RecoveryStep(ctx, "regular_low"); n != 0 {
			t.Fatalf("step %d after healthy", n)
		}
	}
}
