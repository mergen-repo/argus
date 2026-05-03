package binding

// gate.go — STORY-096 Task 7 combined Enforcer + Orchestrator facade.
//
// Each AAA protocol handler (radius, diameter, sba) defines its own narrow
// BindingGate interface (Evaluate + Apply) so the handler packages avoid a
// compile-time dependency on the concrete enforcer / orchestrator types.
// All three interfaces have identical shape — a single concrete struct
// satisfies all of them simultaneously, which is exactly what Gate is.
//
// Constructed once at process boot in cmd/argus/main.go, threaded into
// every AAA protocol handler via SetBindingGate (radius / sba) or
// ServerDeps.BindingGate (diameter). PAT-017 — single point of wiring.

import (
	"context"
)

// Gate combines an Enforcer and an Orchestrator into a single object that
// satisfies the BindingGate interface defined in each AAA protocol handler
// package (radius, diameter, sba). The handler interfaces are structurally
// identical so one concrete type works for all three.
//
// Construct with NewGate — both arguments are required. A nil Gate is a
// valid value at the wire layer (the handler interface is interface-typed
// and call sites check for nil before invoking), but Evaluate/Apply on a
// nil receiver will panic by Go convention; the wire layer guards this.
type Gate struct {
	enforcer     *Enforcer
	orchestrator *Orchestrator
}

// NewGate wires the enforcer + orchestrator into a single BindingGate
// implementation. Either argument may be nil — Evaluate / Apply degrade
// gracefully (Evaluate returns the zero verdict, Apply is a no-op) so a
// half-wired gate is always safer than a panic at the AAA hot path.
func NewGate(enforcer *Enforcer, orchestrator *Orchestrator) *Gate {
	return &Gate{enforcer: enforcer, orchestrator: orchestrator}
}

// Evaluate forwards to the enforcer. Nil enforcer returns the zero verdict
// (Allow with no side effects), matching the AC-17 zero-regression contract.
func (g *Gate) Evaluate(ctx context.Context, session SessionContext, sim SIMView) (Verdict, error) {
	if g == nil || g.enforcer == nil {
		return Verdict{Kind: VerdictAllow}, nil
	}
	return g.enforcer.Evaluate(ctx, session, sim)
}

// Apply forwards to the orchestrator. Nil orchestrator is a no-op (the
// verdict's side effects are dropped silently). The protocol argument is
// one of "radius" / "diameter_s6a" / "5g_sba" and is plumbed into the
// audit + notification + history payloads.
func (g *Gate) Apply(ctx context.Context, v Verdict, session SessionContext, sim SIMView, protocol string) error {
	if g == nil || g.orchestrator == nil {
		return nil
	}
	return g.orchestrator.Apply(ctx, v, session, sim, protocol)
}
