package sim

import (
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
)

// busSubjectSIMUpdated returns the NATS subject for sim.updated events.
// Wrapped in a package-local helper for testability (override in tests).
func busSubjectSIMUpdated() string {
	return bus.SubjectSIMUpdated
}

// busNewSimUpdatedEnvelope constructs the sim.updated envelope (FIX-212).
// Severity is always "info" — SIM state changes are informational; alerting
// on them is the notification subscriber's decision.
func busNewSimUpdatedEnvelope(sim *store.SIM, oldState, newState, displayName string) *bus.Envelope {
	title := "SIM state changed"
	if oldState != "" && newState != "" {
		title = "SIM " + oldState + " → " + newState
	}
	env := bus.NewEnvelope("sim.state_changed", sim.TenantID.String(), severity.Info).
		WithSource("sim").
		WithTitle(title).
		SetEntity("sim", sim.ID.String(), displayName).
		WithMeta("old_state", oldState).
		WithMeta("new_state", newState).
		WithMeta("operator_id", sim.OperatorID.String())
	if sim.APNID != nil {
		env.WithMeta("apn_id", sim.APNID.String())
	}
	if sim.PolicyVersionID != nil {
		env.WithMeta("policy_version_id", sim.PolicyVersionID.String())
	}
	return env
}
