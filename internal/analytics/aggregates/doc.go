// Package aggregates provides the canonical read-path facade over cross-surface
// aggregate metrics (SIM counts by operator/APN/policy/state, session stats,
// traffic). All UI handlers MUST consume metrics via this interface; raw store
// calls in read paths are banned (enforced by Gate grep — see FIX-208 Gate).
//
// # Canonical Sources
//
//   - SIMCountByPolicy: sims.policy_version_id (live FK updated by rollout.Apply)
//     NOT policy_assignments — that table is CoA history.
//     Decision recorded in decisions.md DEV-NNN (FIX-208).
//
// # Caching
//
// The default constructor (`aggregates.New`) wraps dbAggregates in a Redis
// decorator with 60s TTL. Invalidation is driven by NATS events; see
// invalidator.go for the subject→key map.
//
// # Contract
//
// All methods are tenant-scoped. Passing uuid.Nil as tenantID returns
// ErrInvalidTenant. Admin-scope lookups keep calling the store directly.
package aggregates
