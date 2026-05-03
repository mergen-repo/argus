// Package radius — binding_precheck.go
//
// BindingGate is the single combined interface the RADIUS Server uses to
// run the STORY-096 IMEI/SIM binding pre-check. It is defined here so
// server.go does NOT import internal/policy/binding directly; the concrete
// adapter (a thin wrapper around binding.Enforcer + binding.Orchestrator)
// is built in cmd/argus/main.go Task 7 and injected via SetBindingGate.
//
// Design note: a single combined interface (Evaluate + Apply) was chosen
// over two separate fields because it keeps handler code simple and the
// failure modes are coupled — a gate that can Evaluate but not Apply is
// not useful at the call site. The adapter in Task 7 composes both.
package radius

import (
	"context"

	"github.com/btopcu/argus/internal/policy/binding"
)

// BindingGate is the narrow interface the RADIUS handler needs from the
// STORY-096 enforcer + orchestrator pair.
//
//   - Evaluate computes the verdict for this session + SIM combination.
//   - Apply executes audit / notification / history sinks per the verdict
//     and handles SIM bound_imei / grace-window updates.
//
// Both methods must be implemented by the same concrete type so a single
// nil-check in the handler guards both calls.
type BindingGate interface {
	// Evaluate returns the enforcement verdict for the given session and SIM.
	// Failures are treated as fail-open at the call site (log + proceed).
	Evaluate(ctx context.Context, session binding.SessionContext, sim binding.SIMView) (binding.Verdict, error)

	// Apply executes the side effects (audit, notification, history, SIM
	// update) implied by the verdict. protocol must be "radius".
	// Failures are treated as fail-open at the call site (log + proceed).
	Apply(ctx context.Context, v binding.Verdict, session binding.SessionContext, sim binding.SIMView, protocol string) error
}
