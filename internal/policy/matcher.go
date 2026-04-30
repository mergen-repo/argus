package policy

import (
	"context"
	"encoding/json"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Matcher listens for session.started events and re-evaluates policy
// assignment for the SIM. If the SIM's current policy differs from the
// best match (active policy whose DSL references the SIM's APN), it
// updates the assignment.
type Matcher struct {
	policyStore *store.PolicyStore
	simStore    *store.SIMStore
	apnStore    *store.APNStore
	logger      zerolog.Logger
}

func NewMatcher(policyStore *store.PolicyStore, simStore *store.SIMStore, apnStore *store.APNStore, logger zerolog.Logger) *Matcher {
	return &Matcher{
		policyStore: policyStore,
		simStore:    simStore,
		apnStore:    apnStore,
		logger:      logger.With().Str("component", "policy_matcher").Logger(),
	}
}

// Register subscribes to session.started and sim.updated events.
// Queue group ensures single-consumer semantics in a cluster.
func (m *Matcher) Register(eb *bus.EventBus) error {
	if _, err := eb.QueueSubscribeCtx(bus.SubjectSessionStarted, "policy-matcher", m.handleSessionStarted); err != nil {
		return err
	}
	if _, err := eb.QueueSubscribeCtx(bus.SubjectSIMUpdated, "policy-matcher", m.handleSIMUpdated); err != nil {
		return err
	}
	m.logger.Info().Msg("policy matcher registered")
	return nil
}

func (m *Matcher) handleSessionStarted(ctx context.Context, _ string, data []byte) {
	tenantID, simID := extractTenantAndSIM(data)
	if tenantID == "" || simID == "" {
		m.logger.Debug().Msg("session.started payload missing tenant_id/sim_id; skip")
		return
	}
	m.evaluate(ctx, tenantID, simID)
}

func (m *Matcher) handleSIMUpdated(ctx context.Context, _ string, data []byte) {
	tenantID, simID := extractTenantAndSIM(data)
	if tenantID == "" || simID == "" {
		return
	}
	m.evaluate(ctx, tenantID, simID)
}

// extractTenantAndSIM first tries the FIX-212 envelope shape (entity.id + tenant_id
// fields) then falls back to the legacy sim_id/tenant_id top-level shape for
// in-flight events during the 1-release grace window (D-078).
func extractTenantAndSIM(data []byte) (tenantID, simID string) {
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

func (m *Matcher) evaluate(ctx context.Context, tenantIDStr, simIDStr string) {
	if tenantIDStr == "" || simIDStr == "" {
		return
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return
	}
	simID, err := uuid.Parse(simIDStr)
	if err != nil {
		return
	}

	ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)

	sim, err := m.simStore.GetByID(ctx, tenantID, simID)
	if err != nil {
		return
	}
	if sim.APNID == nil || sim.State != "active" {
		return
	}

	apn, err := m.apnStore.GetByID(ctx, tenantID, *sim.APNID)
	if err != nil {
		return
	}

	policies, _, err := m.policyStore.ListReferencingAPN(ctx, tenantID, apn.Name, 10, "")
	if err != nil || len(policies) == 0 {
		return
	}

	var best *store.Policy
	for i := range policies {
		p := &policies[i]
		if p.State == "active" && p.CurrentVersionID != nil {
			best = p
			break
		}
	}
	if best == nil {
		return
	}

	if sim.PolicyVersionID != nil && *sim.PolicyVersionID == *best.CurrentVersionID {
		return
	}

	if err := m.simStore.SetIPAndPolicy(ctx, simID, sim.IPAddressID, best.CurrentVersionID); err != nil {
		m.logger.Warn().Err(err).Str("sim_id", simIDStr).Msg("set policy on sim")
		return
	}
	m.logger.Info().
		Str("sim_id", simIDStr).
		Str("policy_id", best.ID.String()).
		Str("version_id", best.CurrentVersionID.String()).
		Msg("policy auto-assigned via matcher")
}
