package reactive

import (
	"context"
	"sync/atomic"
	"time"
)

type SessionState uint32

const (
	StateIdle SessionState = iota
	StateAuthenticating
	StateAuthenticated
	StateActive
	StateBackingOff
	StateTerminating
	StateSuspended
)

func (s SessionState) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateAuthenticating:
		return "Authenticating"
	case StateAuthenticated:
		return "Authenticated"
	case StateActive:
		return "Active"
	case StateBackingOff:
		return "BackingOff"
	case StateTerminating:
		return "Terminating"
	case StateSuspended:
		return "Suspended"
	default:
		return "Unknown"
	}
}

// DisconnectCause is set by the Listener when it cancels a session.
// Engine reads it to classify the termination metric (PAT-001 single writer).
type DisconnectCause uint32

const (
	CauseNone     DisconnectCause = iota
	CauseDM                       // Disconnect-Message from Argus
	CauseCoADeadline              // CoA updated deadline expired
)

type Session struct {
	ID              string
	OperatorCode    string
	AcctSessionID   string
	State           atomic.Uint32
	Deadline        atomic.Int64
	DisconnectCause atomic.Uint32
	CancelFn        context.CancelFunc
}

// Transition CAS-swaps State from->to. Returns false if another transition
// raced. Callers log at warn and proceed; state is advisory for metrics.
func (s *Session) Transition(from, to SessionState) bool {
	return s.State.CompareAndSwap(uint32(from), uint32(to))
}

func (s *Session) CurrentState() SessionState {
	return SessionState(s.State.Load())
}

// UpdateDeadline atomically stores a new deadline (unix-nanos).
// Used by CoA-ACK to extend/shorten a session mid-flight.
func (s *Session) UpdateDeadline(t time.Time) {
	s.Deadline.Store(t.UnixNano())
}

func (s *Session) CurrentDeadline() time.Time {
	v := s.Deadline.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// SetDisconnectCause atomically records why Cancel was called.
func (s *Session) SetDisconnectCause(c DisconnectCause) {
	s.DisconnectCause.Store(uint32(c))
}

func (s *Session) CurrentDisconnectCause() DisconnectCause {
	return DisconnectCause(s.DisconnectCause.Load())
}
