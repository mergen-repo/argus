package session

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/btopcu/argus/internal/store"
)

// SIMSessionTerminator is the FIX-305 dispatcher: when a SIM transitions to
// a state that requires terminating its active connectivity (Suspend,
// Terminate), the SIM API handler calls TerminateSIMSessions which:
//  1. Lists all active sessions for the SIM (sessions.session_state='active')
//  2. For each session with a NASIP, sends a RADIUS Disconnect-Message (DM)
//     to the NAS so it tears down the actual data path.
//  3. Finalizes the session row in the DB (state → closed,
//     terminate_cause = reason). This step happens regardless of DM
//     success — local DB state must reflect the operator's intent even
//     when the NAS is unreachable.
//
// Use NewSIMSessionTerminator to construct one and pass it to the SIM
// API handler via sim.WithSessionTerminator.
type SIMSessionTerminator struct {
	sessions *store.RadiusSessionStore
	dm       *DMSender
	logger   zerolog.Logger
}

func NewSIMSessionTerminator(sessions *store.RadiusSessionStore, dm *DMSender, logger zerolog.Logger) *SIMSessionTerminator {
	return &SIMSessionTerminator{
		sessions: sessions,
		dm:       dm,
		logger:   logger.With().Str("component", "sim_session_terminator").Logger(),
	}
}

// TerminateSIMSessions sends DM to each active session's NAS (when a NASIP
// is recorded) and finalizes every session in the DB. Returns the number of
// sessions that were finalized. Errors are logged per-session; a single
// session failure does not block the others. Returns a wrapping error only
// if the initial ListActiveBySIM query fails.
func (t *SIMSessionTerminator) TerminateSIMSessions(ctx context.Context, simID uuid.UUID, tenantID uuid.UUID, reason string) (int, error) {
	if t == nil || t.sessions == nil {
		return 0, nil
	}

	sessions, err := t.sessions.ListActiveBySIM(ctx, simID)
	if err != nil {
		return 0, fmt.Errorf("terminator: list active sessions: %w", err)
	}
	if len(sessions) == 0 {
		return 0, nil
	}

	terminated := 0
	for i := range sessions {
		s := sessions[i]

		// Best-effort DM (skip when no NASIP — happens for synthetic / mock
		// sessions or before the NAS has reported). The DB finalize below
		// proceeds either way.
		if t.dm != nil && s.NASIP != nil && *s.NASIP != "" {
			req := DMRequest{
				NASIP:    *s.NASIP,
				TenantID: tenantID,
			}
			if s.AcctSessionID != nil {
				req.AcctSessionID = *s.AcctSessionID
			}
			req.SessionID = s.ID.String()
			if _, dmErr := t.dm.SendDM(ctx, req); dmErr != nil {
				t.logger.Warn().Err(dmErr).
					Str("sim_id", simID.String()).
					Str("session_id", s.ID.String()).
					Str("nas_ip", *s.NASIP).
					Msg("DM send failed; finalizing session locally")
			}
		}

		// Finalize the session row in the DB so subscribers + UI reflect
		// the terminated state immediately. Counters preserved at last-known.
		if err := t.sessions.Finalize(ctx, s.ID, reason, s.BytesIn, s.BytesOut, s.PacketsIn, s.PacketsOut); err != nil {
			t.logger.Warn().Err(err).
				Str("sim_id", simID.String()).
				Str("session_id", s.ID.String()).
				Msg("session finalize failed")
			continue
		}
		terminated++
	}

	return terminated, nil
}
