//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Fault is a way the network breaks, set and cleared on the control socket:
//
//	refuse     dials fail at once, as with nothing listening
//	blackhole  dials hang until the dialer gives up, as with a dead route
//	stall      the socket and noise handshake work, then the server says
//	           nothing, success included
//	silent     live sessions go quiet both ways without closing, as a nat
//	           that dropped its mapping
//	drop       live sessions are cut without a close frame
//	clear      new connections work again; a silent session stays dead
//	clock_jump the wall clock the daemon reads moves by ms
//	appstate_corrupt the next incremental app state answer has a broken
//	           patch mac; a full sync still works
//
// refuse, blackhole and stall hold until clear. silent and drop act on the
// sessions alive at that moment.
type Fault string

const (
	FaultRefuse    Fault = "refuse"
	FaultBlackhole Fault = "blackhole"
	FaultStall     Fault = "stall"
	FaultSilent    Fault = "silent"
	FaultDrop      Fault = "drop"
	FaultClear     Fault = "clear"
	FaultClockJump Fault = "clock_jump"
	FaultAppState  Fault = "appstate_corrupt"
)

// blackholeWait is how long a dial into the black hole takes to fail when the
// dialer sets no deadline of its own, about what a kernel gives a syn.
const blackholeWait = 2 * time.Minute

type faults struct {
	mu        sync.Mutex
	refuse    bool
	blackhole bool
	stall     bool
}

// wallOffset is added to the daemon's wall clock, so a clock jump can be
// played without touching the machine's. process wide, like the transport.
var wallOffset atomic.Int64

// Wall is the wall clock the daemon reads under the mock.
func Wall() time.Time { return time.Now().Add(time.Duration(wallOffset.Load())) }

// replayedAt is the recorded time of the item a replay last played, unix
// nanoseconds, 0 outside a replay.
var replayedAt atomic.Int64

// Stamp is the time the daemon's core stamps on what arrives: the recording's
// own time during a replay, so a capture folds the same whenever it is
// played, and Wall otherwise.
func Stamp() time.Time {
	if at := replayedAt.Load(); at != 0 {
		return time.Unix(0, at)
	}
	return Wall()
}

// Fault breaks the network the way f says, see Fault.
func (s *Server) Fault(f Fault, ms int) error {
	s.faults.mu.Lock()
	switch f {
	case FaultRefuse:
		s.faults.refuse = true
	case FaultBlackhole:
		s.faults.blackhole = true
	case FaultStall:
		s.faults.stall = true
	case FaultClear:
		s.faults.refuse, s.faults.blackhole, s.faults.stall = false, false, false
	case FaultClockJump:
		wallOffset.Add(int64(time.Duration(ms) * time.Millisecond))
	case FaultAppState:
		s.appState.corruptNext()
	case FaultSilent, FaultDrop:
	default:
		s.faults.mu.Unlock()
		return fmt.Errorf("unknown fault %q", f)
	}
	s.faults.mu.Unlock()
	if f != FaultSilent && f != FaultDrop {
		s.log.Info().Str("fault", string(f)).Int("ms", ms).Msg("network fault")
		return nil
	}
	s.mu.Lock()
	live := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		live = append(live, sess)
	}
	s.mu.Unlock()
	for _, sess := range live {
		if f == FaultSilent {
			sess.silent.Store(true)
		} else {
			sess.drop()
		}
	}
	s.log.Info().Str("fault", string(f)).Int("sessions", len(live)).Msg("network fault")
	return nil
}

func (s *Server) stalled() bool {
	s.faults.mu.Lock()
	defer s.faults.mu.Unlock()
	return s.faults.stall
}

// dialFault is what a dial meets before it reaches the mock. nil lets it
// through.
func (s *Server) dialFault(ctx context.Context, address string) error {
	s.faults.mu.Lock()
	refuse, blackhole := s.faults.refuse, s.faults.blackhole
	s.faults.mu.Unlock()
	switch {
	case refuse:
		return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	case blackhole:
		t := time.NewTimer(blackholeWait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return &net.OpError{Op: "dial", Net: "tcp", Err: ctx.Err()}
		case <-t.C:
			return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ETIMEDOUT)}
		}
	}
	return nil
}

// drop cuts the connection under the websocket, no close frame, as a reset.
func (s *session) drop() {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.conn.CloseNow()
	})
}
