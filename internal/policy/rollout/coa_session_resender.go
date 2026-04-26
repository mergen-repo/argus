package rollout

import (
	"context"
	"encoding/json"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const dedupResendWindow = 60 * time.Second

// coaStoreReader is the narrow read seam needed by Resender.
// Satisfied by *store.PolicyStore — defined as interface for test injection.
type coaStoreReader interface {
	GetAssignmentBySIMForResend(ctx context.Context, simID uuid.UUID) (string, *time.Time, error)
}

// coaResendDispatcher is the narrow dispatch seam needed by Resender.
// Satisfied by *Service — defined as interface for test injection.
type coaResendDispatcher interface {
	ResendCoA(ctx context.Context, simID uuid.UUID) error
}

// Resender subscribes to session.started events and re-fires CoA for any
// SIM whose policy_assignments row is currently marked no_session (the SIM
// was idle when its policy was last pushed). A 60-second dedup window
// prevents tight re-fire loops when multiple session.started events arrive
// in quick succession for the same SIM.
type Resender struct {
	policyStore coaStoreReader
	rolloutSvc  coaResendDispatcher
	logger      zerolog.Logger
}

// NewResender constructs a Resender. ps and svc must be non-nil in production;
// both are interfaces so tests can inject fakes without a real DB or Service.
func NewResender(ps coaStoreReader, svc coaResendDispatcher, logger zerolog.Logger) *Resender {
	return &Resender{
		policyStore: ps,
		rolloutSvc:  svc,
		logger:      logger.With().Str("component", "coa_session_resender").Logger(),
	}
}

// Register subscribes to session.started. Queue group "rollout-coa-resend"
// ensures single-consumer semantics in a multi-instance deployment.
func (r *Resender) Register(eb *bus.EventBus) error {
	if _, err := eb.QueueSubscribeCtx(bus.SubjectSessionStarted, "rollout-coa-resend", r.handle); err != nil {
		return err
	}
	r.logger.Info().Msg("coa session resender registered")
	return nil
}

// handle is the core event handler. Mirrors matcher.go::handleSessionStarted
// in style: void return, internal logging for soft errors.
func (r *Resender) handle(ctx context.Context, _ string, data []byte) {
	tenantID, simIDStr := extractTenantAndSIMForResender(data)
	if tenantID == "" || simIDStr == "" {
		r.logger.Warn().Msg("coa_session_resender: malformed envelope; skipping")
		return
	}

	simID, err := uuid.Parse(simIDStr)
	if err != nil {
		r.logger.Warn().Err(err).Str("sim_id", simIDStr).Msg("coa_session_resender: invalid sim_id uuid; skipping")
		return
	}

	coaStatus, coaSentAt, err := r.policyStore.GetAssignmentBySIMForResend(ctx, simID)
	if err != nil {
		r.logger.Warn().Err(err).Str("sim_id", simIDStr).Msg("coa_session_resender: read assignment; skipping")
		return
	}

	if coaStatus != CoAStatusNoSession {
		return
	}

	if coaSentAt != nil && time.Since(*coaSentAt) < dedupResendWindow {
		r.logger.Debug().Str("sim_id", simIDStr).Msg("coa_session_resender: within dedup window; skipping")
		return
	}

	if err := r.rolloutSvc.ResendCoA(ctx, simID); err != nil {
		r.logger.Warn().Err(err).Str("sim_id", simIDStr).Msg("coa_session_resender: ResendCoA failed")
	}
}

// extractTenantAndSIMForResender is a local copy of the pattern from
// internal/policy/matcher.go::extractTenantAndSIM (D-078). It supports
// both the FIX-212 envelope shape and the legacy top-level sim_id/tenant_id
// fields for in-flight backward compatibility during the 1-release grace window.
func extractTenantAndSIMForResender(data []byte) (tenantID, simID string) {
	var env bus.Envelope
	if err := json.Unmarshal(data, &env); err == nil && env.EventVersion == bus.CurrentEventVersion {
		tenantID = env.TenantID
		if env.Entity != nil && env.Entity.Type == "sim" {
			simID = env.Entity.ID
		}
		if simID == "" {
			if s, ok := env.Meta["sim_id"].(string); ok {
				simID = s
			}
		}
		return
	}
	var legacy struct {
		SIMID    string `json:"sim_id"`
		TenantID string `json:"tenant_id"`
	}
	_ = json.Unmarshal(data, &legacy)
	return legacy.TenantID, legacy.SIMID
}
