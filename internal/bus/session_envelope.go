package bus

import (
	"github.com/btopcu/argus/internal/severity"
)

// NewSessionEnvelope builds a bus.Envelope for session.started /
// session.updated / session.ended subjects. FIX-212 D2 hot-path rules:
//   - ICCID is pre-resolved by the AAA publisher from the already-loaded
//     SIM context; empty iccid yields empty display_name (FE fallback).
//   - operator_id / apn_id travel in meta but NOT resolved to names —
//     that would require DB/Redis on the hot path. Resolver-subscriber
//     enrichment is deferred to a follow-up story.
func NewSessionEnvelope(eventType, tenantID, simID, iccid, title string) *Envelope {
	env := NewEnvelope(eventType, tenantID, severity.Info).
		WithSource("aaa").
		WithTitle(title)
	if simID != "" {
		display := ""
		if iccid != "" {
			display = "ICCID " + iccid
		}
		env.SetEntity("sim", simID, display)
	}
	return env
}
