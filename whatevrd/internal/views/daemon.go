package views

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/conn"
	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
	"whatevrd/internal/status"
)

// historyStall is how long a sync type goes with nothing new before it
// counts as stalled, the board's own measure
const historyStall = 5 * time.Minute

func (rs *Reads) connectionView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		st := rs.live.Conn()
		row := v2.ConnectionRow_builder{
			State:        connState(st.Kind),
			SinceMs:      ms(st.Since),
			Detail:       st.Detail,
			Cause:        connCause(st.Cause),
			Attempt:      uint32(max(st.Attempt, 0)),
			NextRetryMs:  ms(st.Next),
			CanReconnect: st.Manual,
		}.Build()
		out, err := rs.r.Unsent(ctx)
		if err != nil {
			return nil, err
		}
		row.SetPendingOutgoing(uint32(len(out)))
		it := &v2.Upsert{}
		it.SetConnection(row)
		return one(it), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, live.TouchConn, TouchSends) }
	return w, nil, nil
}

func connState(k conn.Kind) v2.ConnectionState {
	switch k {
	case conn.Starting:
		return v2.ConnectionState_CONNECTION_STATE_STARTING
	case conn.NeedLogin:
		return v2.ConnectionState_CONNECTION_STATE_NEED_LOGIN
	case conn.Connecting:
		return v2.ConnectionState_CONNECTION_STATE_CONNECTING
	case conn.Online:
		return v2.ConnectionState_CONNECTION_STATE_ONLINE
	case conn.Waiting, conn.NoNetwork:
		return v2.ConnectionState_CONNECTION_STATE_WAITING
	case conn.LoggedOut, conn.Banned, conn.Outdated, conn.Replaced:
		return v2.ConnectionState_CONNECTION_STATE_OFFLINE
	}
	return v2.ConnectionState_CONNECTION_STATE_UNSPECIFIED
}

func connCause(c conn.Cause) v2.ConnectionCause {
	switch c {
	case conn.CauseUnreachable:
		return v2.ConnectionCause_CONNECTION_CAUSE_UNREACHABLE
	case conn.CauseNoAnswer:
		return v2.ConnectionCause_CONNECTION_CAUSE_NO_ANSWER
	case conn.CauseRefused:
		return v2.ConnectionCause_CONNECTION_CAUSE_REFUSED
	case conn.CauseSilent:
		return v2.ConnectionCause_CONNECTION_CAUSE_SILENT
	case conn.CauseClosed:
		return v2.ConnectionCause_CONNECTION_CAUSE_CLOSED
	}
	return v2.ConnectionCause_CONNECTION_CAUSE_UNSPECIFIED
}

// ms is t in unix ms, 0 for the zero time.
func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func (rs *Reads) loginView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	if rs.login != nil {
		rs.login()
	}
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		l := rs.live.Login()
		row := v2.LoginRow_builder{State: loginState(l.State), Detail: l.Detail}.Build()
		if l.State == live.ShowingQR {
			row.SetQr(l.QR)
			row.SetQrExpiresMs(ms(l.Expires))
		}
		it := &v2.Upsert{}
		it.SetLogin(row)
		return one(it), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, live.TouchLogin) }
	return w, nil, nil
}

func loginState(s live.LoginState) v2.LoginState {
	switch s {
	case live.LoggedIn:
		return v2.LoginState_LOGIN_STATE_LOGGED_IN
	case live.ShowingQR:
		return v2.LoginState_LOGIN_STATE_QR
	case live.Pairing:
		return v2.LoginState_LOGIN_STATE_PAIRING
	case live.LoginFailed:
		return v2.LoginState_LOGIN_STATE_FAILED
	}
	return v2.LoginState_LOGIN_STATE_UNSPECIFIED
}

func (rs *Reads) syncView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		c, err := rs.r.Completeness(ctx)
		if err != nil {
			return nil, err
		}
		it := &v2.Upsert{}
		it.SetSync(syncRow(c.History, time.Now()))
		return one(it), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "sync", TouchClock) }
	return w, nil, nil
}

// syncRow is the sync type running now, or the one that ran last.
func syncRow(hs []model.HistoryState, now time.Time) *v2.SyncRow {
	var cur *model.HistoryState
	for i := range hs {
		h := &hs[i]
		if cur == nil || (h.State == model.Syncing) != (cur.State == model.Syncing) && h.State == model.Syncing ||
			(h.State == model.Syncing) == (cur.State == model.Syncing) && h.Last > cur.Last {
			cur = h
		}
	}
	row := &v2.SyncRow{}
	if cur == nil {
		row.SetPhase(v2.SyncPhase_SYNC_PHASE_IDLE)
		return row
	}
	row.SetType(syncType(cur.SyncType))
	row.SetPercent(uint32(min(max(cur.Progress, 0), 100)))
	row.SetChunk(uint32(max(cur.Blobs, 0)))
	switch {
	case cur.State == model.Unavailable:
		row.SetPhase(v2.SyncPhase_SYNC_PHASE_STALLED)
	case cur.State == model.Syncing && now.Sub(time.UnixMilli(cur.Last)) > historyStall:
		row.SetPhase(v2.SyncPhase_SYNC_PHASE_STALLED)
	case cur.State == model.Syncing:
		row.SetPhase(v2.SyncPhase_SYNC_PHASE_RUNNING)
	default:
		row.SetPhase(v2.SyncPhase_SYNC_PHASE_DONE)
		row.SetPercent(100)
	}
	return row
}

func syncType(t string) v2.SyncType {
	switch t {
	case "INITIAL_BOOTSTRAP", "INITIAL_STATUS_V3":
		return v2.SyncType_SYNC_TYPE_INITIAL
	case "RECENT":
		return v2.SyncType_SYNC_TYPE_RECENT
	case "FULL":
		return v2.SyncType_SYNC_TYPE_FULL
	case "ON_DEMAND":
		return v2.SyncType_SYNC_TYPE_ON_DEMAND
	case "PUSH_NAME":
		return v2.SyncType_SYNC_TYPE_PUSH_NAMES
	}
	return v2.SyncType_SYNC_TYPE_UNSPECIFIED
}

// severity is the problems view's order, worst first
var severity = []status.Kind{
	status.StoreFailing, status.LoggedOut, status.Banned, status.Outdated, status.Replaced, status.Refused,
	status.StuckLogin, status.NoNetwork, status.KeepaliveLost, status.OutboxFailing, status.AppStateOutOfSync,
	status.HistoryStalled, status.WaitingOnPhone, status.MediaFailing,
}

func (rs *Reads) problemsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		if rs.board == nil {
			return nil, nil
		}
		var out []*v2.Upsert
		for _, p := range rs.board.List(ctx) {
			rank := slices.Index(severity, p.Kind)
			if rank < 0 {
				rank = len(severity)
			}
			it := &v2.Upsert{}
			it.SetId(string(p.Kind))
			it.SetSort(append([]byte{byte(rank)}, p.Kind...))
			it.SetProblem(v2.ProblemRow_builder{Kind: problemKind(p.Kind), SinceMs: ms(p.Since),
				NextRetryMs: ms(p.Next), Text: problemText(p)}.Build())
			out = append(out, it)
		}
		slices.SortFunc(out, func(a, b *v2.Upsert) int { return bytes.Compare(a.GetSort(), b.GetSort()) })
		return limited(out, max), nil
	}
	w.wake = func(c core.Change) bool {
		return touches(c, live.TouchProblems, live.TouchConn, "sync", "appstate", TouchSends, TouchClock) ||
			c.All["message"]
	}
	return w, nil, nil
}

func problemKind(k status.Kind) v2.ProblemKind {
	switch k {
	case status.NoNetwork:
		return v2.ProblemKind_PROBLEM_KIND_OFFLINE
	case status.StuckLogin:
		return v2.ProblemKind_PROBLEM_KIND_STUCK_BEFORE_LOGIN
	case status.Refused:
		return v2.ProblemKind_PROBLEM_KIND_SERVER_REFUSED
	case status.LoggedOut:
		return v2.ProblemKind_PROBLEM_KIND_LOGGED_OUT
	case status.Banned:
		return v2.ProblemKind_PROBLEM_KIND_TEMP_BANNED
	case status.Outdated:
		return v2.ProblemKind_PROBLEM_KIND_CLIENT_OUTDATED
	case status.Replaced:
		return v2.ProblemKind_PROBLEM_KIND_STREAM_REPLACED
	case status.KeepaliveLost:
		return v2.ProblemKind_PROBLEM_KIND_KEEPALIVE_LOST
	case status.AppStateOutOfSync:
		return v2.ProblemKind_PROBLEM_KIND_APP_STATE
	case status.HistoryStalled:
		return v2.ProblemKind_PROBLEM_KIND_HISTORY_STALLED
	case status.WaitingOnPhone:
		return v2.ProblemKind_PROBLEM_KIND_UNDECRYPTED
	case status.OutboxFailing:
		return v2.ProblemKind_PROBLEM_KIND_SEND_FAILING
	case status.MediaFailing:
		return v2.ProblemKind_PROBLEM_KIND_MEDIA_FAILING
	case status.StoreFailing:
		return v2.ProblemKind_PROBLEM_KIND_STORAGE
	}
	return v2.ProblemKind_PROBLEM_KIND_UNSPECIFIED
}

// problemText is p as a sentence for people, its detail after it.
func problemText(p status.Problem) string {
	var s string
	switch p.Kind {
	case status.NoNetwork:
		s = "Can't reach WhatsApp."
	case status.StuckLogin:
		s = "Connected, but WhatsApp never finished logging in."
	case status.Refused:
		s = "WhatsApp turned the login down."
	case status.LoggedOut:
		s = "Logged out. Link this computer again to carry on."
	case status.Banned:
		s = "WhatsApp suspended this account for a while."
		if !p.Next.IsZero() {
			s = fmt.Sprintf("WhatsApp suspended this account until %s.", p.Next.Local().Format("Jan 2 15:04"))
		}
	case status.Outdated:
		s = "WhatsApp wants a newer version of whatevr."
	case status.Replaced:
		s = "WhatsApp was opened on another computer. Reconnect to take it back."
	case status.KeepaliveLost:
		s = "The connection went quiet and is being replaced."
	case status.AppStateOutOfSync:
		s = "Some settings from the phone can't be read."
	case status.HistoryStalled:
		s = "The phone stopped sending history."
	case status.WaitingOnPhone:
		s = "Some messages couldn't be decrypted and are being asked for again."
	case status.OutboxFailing:
		s = "Messages are waiting to send and failing."
	case status.MediaFailing:
		s = "Downloads keep failing."
	case status.StoreFailing:
		s = "The database can't be written."
	default:
		s = "Something is wrong."
	}
	if p.Detail != "" {
		s += " (" + p.Detail + ")"
	}
	return s
}
