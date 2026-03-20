# Deliverable: STORY-007 — Audit Log Service — Tamper-Proof Hash Chain

## Summary
Implemented audit service (SVC-10) with tamper-proof SHA-256 hash chain, NATS-based event consumption, search/filter/export APIs, and KVKK pseudonymization support.

## Files Changed

### New Files
- `internal/audit/audit.go` — Domain types (Entry, AuditEvent, VerifyResult), ComputeHash, ComputeDiff, VerifyChain, Auditor interface
- `internal/audit/service.go` — FullService with NATS consumer, per-tenant mutex serialization, ProcessEntry, PublishAuditEvent, VerifyChain
- `internal/audit/audit_test.go` — 21 unit tests (hash, diff, chain, FullService)
- `internal/store/audit.go` — AuditStore: Create, GetLastHash, List, GetRange, GetByDateRange, Pseudonymize
- `internal/store/audit_test.go` — Store unit tests (pseudonymization, edge cases)
- `internal/api/audit/handler.go` — HTTP handlers: List (API-140), Verify (API-141), Export (API-142)
- `internal/api/audit/handler_test.go` — 9 handler tests (auth, validation, response mapping)

### Modified Files
- `internal/bus/nats.go` — Added SubjectAuditCreate constant
- `internal/gateway/router.go` — Added AuditHandler to RouterDeps, registered 3 audit routes
- `internal/api/tenant/handler.go` — Updated to use Auditor interface, added correlation_id extraction
- `internal/api/user/handler.go` — Updated to use Auditor interface, added correlation_id extraction
- `internal/api/session/handler.go` — Updated to use Auditor interface, added correlation_id extraction
- `cmd/argus/main.go` — Wired FullService + AuditStore + AuditHandler + eventBusSubscriber adapter

## Architecture References Fulfilled
- SVC-10: Audit Service — fully implemented
- API-140: GET /api/v1/audit-logs — list with cursor pagination + 6 filters
- API-141: GET /api/v1/audit-logs/verify — hash chain integrity verification
- API-142: POST /api/v1/audit-logs/export — CSV stream export
- TBL-19: audit_logs — all columns mapped, partitioned table utilized
- ALGORITHMS.md Section 2: Hash chain algorithm implemented per spec

## Key Decisions
- DEV-013: Old Service stub replaced by FullService with Auditor interface
- DEV-014: user_name omitted from API-140 response (user_id only, frontend resolves)
- DEV-015: CSV export returns 200 with stream (not 202 with download_url)
- DEV-016: eventBusSubscriber adapter bridges EventBus to audit.MessageSubscriber

## Test Coverage
- 30 unit tests across 3 packages
- Hash computation: determinism, nil user_id, different prev_hash
- Diff computation: create, update, delete, no-changes, both-nil
- Chain verification: valid chain, tampered entry, broken link, single entry, empty
- FullService: ProcessEntry, chain integrity, publish, verify, CreateEntry
- Pseudonymization: sensitive field replacement, edge cases
- Handlers: auth checks, input validation, response mapping
- Full suite: 24 packages pass, 0 regressions
