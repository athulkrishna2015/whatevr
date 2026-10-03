//go:build whatevr_core

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"whatevrd/internal/conn"
	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/status"
)

// historyStall is how long a history sync type may go without a new
// notification or download before it counts as stalled.
const historyStall = 5 * time.Minute

// mediaFails is how many fetches in a row must fail, with none working in
// between, before media counts as failing rather than one message's bad luck.
const mediaFails = 3

// mediaWatch puts media on the board from how fetches end.
type mediaWatch struct {
	board *status.Board
	log   zerolog.Logger
	now   func() time.Time

	mu    sync.Mutex
	fails int
	since time.Time
}

func (m *mediaWatch) result(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		if m.fails >= mediaFails {
			m.report(m.board.Clear("media", status.MediaFailing))
		}
		m.fails = 0
		return
	}
	if m.fails == 0 {
		m.since = m.now()
	}
	m.fails++
	if m.fails >= mediaFails {
		m.report(m.board.Set("media", status.Problem{Kind: status.MediaFailing, Since: m.since,
			Detail: fmt.Sprintf("%d downloads failed in a row, last: %v", m.fails, err)}))
	}
}

func (m *mediaWatch) report(err error) {
	if err != nil {
		m.log.Error().Err(err).Msg("status: media")
	}
}

// connProblems is the machine's state on the board. ok false leaves the board
// as it is: a retry with no news keeps whatever went wrong before it.
func connProblems(s conn.Status) (ps []status.Problem, ok bool) {
	p := status.Problem{Since: s.Since, Detail: s.Detail, Next: s.Next}
	switch s.Kind {
	case conn.Starting, conn.Online, conn.NeedLogin:
		return nil, true
	case conn.NoNetwork:
		p.Kind = status.NoNetwork
	case conn.LoggedOut:
		p.Kind = status.LoggedOut
	case conn.Banned:
		p.Kind = status.Banned
	case conn.Outdated:
		p.Kind = status.Outdated
	case conn.Replaced:
		p.Kind = status.Replaced
	case conn.Connecting, conn.Waiting:
		switch s.Cause {
		case conn.CauseUnreachable:
			p.Kind = status.NoNetwork
		case conn.CauseNoAnswer:
			p.Kind = status.StuckLogin
		case conn.CauseRefused:
			p.Kind = status.Refused
		case conn.CauseSilent:
			p.Kind = status.KeepaliveLost
		default:
			return nil, false
		}
	default:
		return nil, false
	}
	return []status.Problem{p}, true
}

func syncProblems(db *core.DB) status.Provider {
	return func(ctx context.Context) ([]status.Problem, error) {
		r := model.NewReader(db.Read())
		c, err := r.Completeness(ctx)
		if err != nil {
			return nil, err
		}
		var ps []status.Problem
		var bad []string
		var since int64
		for _, d := range c.AppState {
			if d.State != model.Unavailable && d.Recovery == "" {
				continue
			}
			what := d.Domain + ": " + d.Error
			if d.Recovery != "" {
				what += " (recovering: " + d.Recovery + ")"
			}
			bad = append(bad, what)
			if since == 0 || d.Since < since {
				since = d.Since
			}
		}
		if len(bad) > 0 {
			ps = append(ps, status.Problem{Kind: status.AppStateOutOfSync, Since: time.UnixMilli(since), Detail: strings.Join(bad, "; ")})
		}
		now := time.Now()
		var stalled []string
		since = 0
		for _, h := range c.History {
			last := time.UnixMilli(h.Last)
			switch {
			case h.State == model.Unavailable:
				stalled = append(stalled, fmt.Sprintf("%s: %d downloads failed", h.SyncType, h.Failed))
			case h.State == model.Syncing && now.Sub(last) > historyStall:
				stalled = append(stalled, fmt.Sprintf("%s: at %d%%, nothing new for %s", h.SyncType, h.Progress, now.Sub(last).Round(time.Minute)))
			default:
				continue
			}
			if since == 0 || h.Last < since {
				since = h.Last
			}
		}
		if len(stalled) > 0 {
			ps = append(ps, status.Problem{Kind: status.HistoryStalled, Since: time.UnixMilli(since), Detail: strings.Join(stalled, "; ")})
		}
		n, oldest, err := r.Waiting(ctx)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			ps = append(ps, status.Problem{Kind: status.WaitingOnPhone, Since: time.UnixMilli(oldest), Detail: fmt.Sprintf("%d messages the phone has to send again", n)})
		}
		return ps, nil
	}
}

func outboxProblems(db *core.DB) status.Provider {
	return func(ctx context.Context) ([]status.Problem, error) {
		out, err := model.NewReader(db.Read()).Unsent(ctx)
		if err != nil {
			return nil, err
		}
		var failing []model.Outgoing
		for _, o := range out {
			if o.Attempts > 0 {
				failing = append(failing, o)
			}
		}
		if len(failing) == 0 {
			return nil, nil
		}
		last := failing[len(failing)-1]
		return []status.Problem{{Kind: status.OutboxFailing, Since: time.UnixMilli(failing[0].T),
			Detail: fmt.Sprintf("%d sends failing, last: %s", len(failing), last.Error)}}, nil
	}
}

func coreProblems(db *core.DB) status.Provider {
	return func(context.Context) ([]status.Problem, error) {
		h := db.Health()
		var ps []status.Problem
		for _, f := range []struct {
			what string
			f    *core.Failure
		}{{"logging new input", h.Append}, {"folding", h.Fold}} {
			if f.f != nil {
				ps = append(ps, status.Problem{Kind: status.StoreFailing, Since: f.f.At, Detail: f.what + ": " + f.f.Error})
			}
		}
		// two failures are one kind: keep the older, name both
		if len(ps) == 2 {
			ps[0].Detail += "; " + ps[1].Detail
			ps = ps[:1]
		}
		return ps, nil
	}
}

// watchBoard logs every problem as it starts and ends. nothing outside the
// daemon reads the board yet: v1 has only the connection view, the full list
// waits for protocol 2.
func watchBoard(ctx context.Context, log zerolog.Logger, b *status.Board, poke <-chan struct{}, every time.Duration) {
	seen := map[status.Kind]status.Problem{}
	check := func() {
		now := map[status.Kind]status.Problem{}
		for _, p := range b.List(ctx) {
			now[p.Kind] = p
		}
		var kinds []status.Kind
		for k := range seen {
			if _, ok := now[k]; !ok {
				kinds = append(kinds, k)
			}
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
		for _, k := range kinds {
			log.Info().Str("problem", string(k)).Dur("lasted", time.Since(seen[k].Since)).Msg("status: problem over")
		}
		for k, p := range now {
			if old, ok := seen[k]; ok && old.Detail == p.Detail {
				continue
			}
			log.Warn().Str("problem", string(k)).Str("owner", status.Owner(k)).Str("detail", p.Detail).Time("since", p.Since).Msg("status: problem")
		}
		seen = now
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		case <-poke:
			check()
		}
	}
}
