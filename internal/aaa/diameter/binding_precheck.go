package diameter

import (
	"context"

	"github.com/btopcu/argus/internal/policy/binding"
)

// BindingGate is the narrow interface the S6a handler uses for the
// IMEI/SIM binding pre-check (STORY-096 / Task 4). It mirrors the
// Enforcer + Orchestrator surface exposed by internal/policy/binding
// but is defined locally so the diameter package has no compile-time
// dependency on the concrete types (PAT-017 — handler struct gains one
// new interface-typed field; Task 7 supplies the concrete gate).
type BindingGate interface {
	Evaluate(ctx context.Context, session binding.SessionContext, sim binding.SIMView) (binding.Verdict, error)
	Apply(ctx context.Context, v binding.Verdict, session binding.SessionContext, sim binding.SIMView, protocol string) error
}
