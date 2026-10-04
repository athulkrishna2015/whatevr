package model

import (
	"context"
	"strings"

	"whatevrd/internal/core"
)

// syncDomain keeps how each sync domain last ended, so what the daemon has
// of each is known from the log rather than guessed.
var syncDomain = core.Domain{
	Name:    "sync",
	Version: 2,
	Tables:  []string{"sync_domain"},
	Schema: []string{
		`CREATE TABLE sync_domain (
			domain  TEXT PRIMARY KEY,
			version INTEGER NOT NULL DEFAULT 0,
			ok_t    INTEGER NOT NULL DEFAULT 0,
			err_t   INTEGER NOT NULL DEFAULT 0,
			error   TEXT NOT NULL DEFAULT '',
			recovered_t INTEGER NOT NULL DEFAULT 0,
			-- the recovery step, newest by input time; seq is the input it
			-- came from, which breaks a tie
			step   INTEGER NOT NULL DEFAULT 0,
			step_t INTEGER NOT NULL DEFAULT 0,
			seq    INTEGER NOT NULL DEFAULT 0
		)`,
	},
	Folds: map[string]core.FoldFunc{core.KindSyncState: foldSyncDomain},
}

func foldSyncDomain(tx *core.Tx, in core.Input) error {
	h, err := head[core.SyncStateHead](in)
	if err != nil || h.Domain == "" {
		return err
	}
	t := in.At.UnixMilli()
	var ok, bad, rec int64
	switch h.State {
	case core.SyncComplete:
		ok = t
		if h.Recovery {
			rec = t
		}
	case core.SyncError:
		bad = t
	}
	if _, err := tx.Exec(`INSERT INTO sync_domain (domain, version, ok_t, err_t, error, recovered_t) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (domain) DO UPDATE SET
			version = MAX(sync_domain.version, excluded.version),
			ok_t = MAX(sync_domain.ok_t, excluded.ok_t),
			error = CASE WHEN (excluded.err_t, excluded.error) > (sync_domain.err_t, sync_domain.error) THEN excluded.error ELSE sync_domain.error END,
			err_t = MAX(sync_domain.err_t, excluded.err_t),
			recovered_t = MAX(sync_domain.recovered_t, excluded.recovered_t)`,
		h.Domain, h.Version, ok, bad, h.Error, rec); err != nil {
		return err
	}
	if h.Step != nil {
		if _, err := tx.Exec(`UPDATE sync_domain SET step = ?, step_t = ?, seq = ?
			WHERE domain = ? AND (step_t, seq) < (?, ?)`, *h.Step, t, in.Seq, h.Domain, t, in.Seq); err != nil {
			return err
		}
	}
	tx.Touch("sync", h.Domain)
	return nil
}

// what the daemon has of a domain
const (
	// Known is synced: what is here is what the server has.
	Known = "known"
	// Syncing is on its way, nothing says it is all here yet.
	Syncing = "syncing"
	// Unavailable is a sync that failed last; whatever is here may be stale.
	Unavailable = "unavailable"
)

// Collections are the app state collections, each its own domain.
var Collections = []string{"critical_block", "critical_unblock_low", "regular_high", "regular", "regular_low"}

type DomainState struct {
	Domain  string
	State   string
	Version uint64
	// Since is when the domain got to State, 0 for never
	Since int64
	Error string
	// Recovery is how far recovering it got: "", RecoveringFullSync or
	// RecoveringFromPhone
	Recovery string
}

// recovery steps as whatsmeow numbers them
const (
	RecoveringFullSync  = "full_sync"
	RecoveringFromPhone = "asked_phone"
)

func recoveryName(step int) string {
	switch step {
	case 1:
		return RecoveringFullSync
	case 2:
		return RecoveringFromPhone
	}
	return ""
}

// RecoveryStep is how far recovering an app state collection got, as
// whatsmeow numbers it. 0 is healthy.
func (r *Reader) RecoveryStep(ctx context.Context, collection string) (int, error) {
	var step int
	err := r.db.QueryRowContext(ctx, `SELECT step FROM sync_domain WHERE domain = ?`, "app_state:"+collection).Scan(&step)
	if isNoRows(err) {
		return 0, nil
	}
	return step, err
}

type HistoryState struct {
	SyncType string
	State    string
	// Progress is the phone's own number for the type, the highest it said
	Progress int
	Blobs    int
	Pending  int
	Failed   int
	// Last is when the newest notification or download of the type landed,
	// unix ms
	Last int64
	// Newest is the seq of the type's newest notification: the phone's order,
	// which a replay repeats, where Last is the downloads'
	Newest int64
}

type Completeness struct {
	AppState []DomainState
	History  []HistoryState
}

// Completeness is what the daemon has of each sync domain.
func (r *Reader) Completeness(ctx context.Context) (Completeness, error) {
	var c Completeness
	rows, err := r.db.QueryContext(ctx, `SELECT domain, version, ok_t, err_t, error, step FROM sync_domain WHERE domain LIKE 'app_state:%'`)
	if err != nil {
		return c, err
	}
	seen := map[string]DomainState{}
	for rows.Next() {
		var d DomainState
		var ok, bad int64
		var step int
		if err := rows.Scan(&d.Domain, &d.Version, &ok, &bad, &d.Error, &step); err != nil {
			rows.Close()
			return c, err
		}
		d.Domain = strings.TrimPrefix(d.Domain, "app_state:")
		switch {
		case bad > ok:
			d.State, d.Since = Unavailable, bad
		case ok > 0:
			d.State, d.Since, d.Error = Known, ok, ""
		default:
			d.State, d.Error = Syncing, ""
		}
		d.Recovery = recoveryName(step)
		seen[d.Domain] = d
	}
	rows.Close()
	for _, name := range Collections {
		d, ok := seen[name]
		if !ok {
			d = DomainState{Domain: name, State: Syncing}
		}
		c.AppState = append(c.AppState, d)
	}

	rows, err = r.db.QueryContext(ctx, `SELECT n.sync_type, MAX(n.progress), COUNT(*),
		SUM(CASE WHEN b.notification IS NULL THEN 1 ELSE 0 END),
		SUM(CASE WHEN b.error != '' THEN 1 ELSE 0 END),
		MAX(MAX(n.at, COALESCE(b.at, 0))), MAX(n.seq)
		FROM hist_note n LEFT JOIN hist_blob b ON b.notification = n.notification
		GROUP BY n.sync_type ORDER BY n.sync_type`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var h HistoryState
		if err := rows.Scan(&h.SyncType, &h.Progress, &h.Blobs, &h.Pending, &h.Failed, &h.Last, &h.Newest); err != nil {
			return c, err
		}
		switch {
		case h.Failed > 0 && h.Pending == 0:
			h.State = Unavailable
		case h.Pending == 0 && (h.Progress >= 100 || noProgress(h.SyncType)):
			h.State = Known
		default:
			h.State = Syncing
		}
		c.History = append(c.History, h)
	}
	return c, rows.Err()
}

// noProgress is a sync type that carries no progress: one blob is all of it.
func noProgress(t string) bool { return t == "PUSH_NAME" || t == "NON_BLOCKING_DATA" }

// Waiting is messages the phone was asked to send again, still not here:
// how many and since when (unix ms of the oldest).
func (r *Reader) Waiting(ctx context.Context) (n int, oldest int64, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(t), 0) FROM msg_wait w
		WHERE w.unavailable = '' AND NOT EXISTS (SELECT 1 FROM msg m WHERE m.id = w.id)`).Scan(&n, &oldest)
	return n, oldest, err
}
