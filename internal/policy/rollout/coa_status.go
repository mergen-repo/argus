package rollout

// CoA status enum values for policy_assignments.coa_status.
// Order matches the AC-2 canonical list — used by metric label seeding + tests.
// Backed by DB CHECK constraint chk_coa_status (migrations/20260430000001).
const (
	CoAStatusPending   = "pending"    // just-inserted, not-yet-processed (transient)
	CoAStatusQueued    = "queued"     // dispatch in-flight
	CoAStatusAcked     = "acked"      // CoA delivered + ack received
	CoAStatusFailed    = "failed"     // dispatch attempted but failed (after retries)
	CoAStatusNoSession = "no_session" // no active session to push to (idle SIM)
	CoAStatusSkipped   = "skipped"    // policy rule indicated skip (e.g., low-priority change)
)

// CoAStatusAll is the canonical list in display order. Used for metric label seeding
// (Prometheus gauge requires all label values pre-registered for stable cardinality)
// and tests.
var CoAStatusAll = []string{
	CoAStatusPending,
	CoAStatusQueued,
	CoAStatusAcked,
	CoAStatusFailed,
	CoAStatusNoSession,
	CoAStatusSkipped,
}
