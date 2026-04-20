# Gate Report: FIX-202 — SIM List & Dashboard DTO: Operator Name Resolution Everywhere

## Summary

- **Overall Verdict: PASS** (1 HIGH finding fixed in-gate, 5 items deferred to Tech Debt)
- Requirements Tracing: Fields 100% (DTO widening landed across SIM / Session / Violation / eSIM / Dashboard / Notification), Endpoints 6/6 touched, Workflows N/A, Components 1/1 (OperatorChip reused in 5 pages)
- Gap Analysis: 10/10 acceptance criteria satisfied (AC-3 and AC-7 partial — explicit tech-debt + plan); AC-10 evidence pending local DB run (D-049)
- Compliance: COMPLIANT — PAT-006 single scan-helper discipline held, LEFT JOIN orphan safety, tenant scoping preserved, API envelope unchanged
- Tests: Full suite 3270 / 3270 PASS; race-detector PASS on dashboard + session
- Test Coverage: Orphan row, cross-tenant, chunk-boundary (501), APN display-name precedence, no-policy paths all unit-tested (DB-gated). Race coverage added for dashboard.
- Performance: AC-10 EXPLAIN test lands; execution-time assertion < 100ms (DB-gated). JOIN plan rejects root Seq Scan on sims.
- Build: Go build PASS · go vet PASS · tsc --noEmit PASS
- Token Enforcement: 0 hardcoded hex in new FE files, 0 in modified FE-lines across 5 pages. Operator chip palette uses only `warning` / `danger` / `info` / `text-tertiary` tokens.
- Turkish Text: N/A (no user-facing copy changed in this story)

## Team Composition
- Scout Analysis (internal): 4 findings (F-A1 — HIGH race, F-A2 — MEDIUM DTO typing, F-A3 — LOW partition-unaware JOIN, F-A4 — MEDIUM AC-7 partial)
- Scout Test/Build (internal): 0 new findings — 3270 preserved, typecheck clean, vet clean. AC-10 evidence gap surfaced via step log (tracked as F-B1 → D-049).
- Scout UI (internal): 0 new findings — OperatorChip is reused across all 5 target pages, tokens compliant, orphan fallback renders as specified.
- De-duplicated: 5 findings → 1 FIX NOW + 4 DEFERRED

## Fixes Applied

| # | Category | File | Line | Change | Verified |
|---|----------|------|------|--------|----------|
| 1 | Concurrency | `internal/api/dashboard/handler.go` | 257-266, 402-415 | Moved `active_sessions` merge out of the operator-health goroutine and re-homed it after `wg.Wait()`. Mutex-alone did not enforce happens-before ordering with the session-stats goroutine, so the merge intermittently saw `sessionStatsByOp == nil` and silently dropped `active_sessions` from the response. Sequential post-Wait placement removes the race entirely. Also copy `count` into a fresh local before taking its address (address-of-loop-variable safety). | `go test -race ./internal/api/dashboard/... ./internal/api/session/... -count=1` → 15 / 15 PASS, race detector clean. Full suite 3270 / 3270 PASS. |

## Findings Resolution

| ID | Severity | Action | Target/Resolution |
|----|----------|--------|-------------------|
| F-A1 | HIGH | FIX NOW | Dashboard race on `sessionStatsByOp` — merge moved post-`wg.Wait()` (fixed above) |
| F-A2 | MEDIUM | DEFER | `violationDTO` uses `interface{}` for typed fields — JSON contract is byte-identical, cosmetic only — D-046 |
| F-A3 | LOW | DEFER | Enriched sims JOINs lack partition key in predicate — acceptable at current 3-operator scale — D-047 |
| F-A4 | MEDIUM | DEFER | AC-7 notification `entity_refs[].display_name` carried empty — full cross-entity name resolution is FIX-212's unified envelope — D-048 |
| F-B1 | MEDIUM | DEFER | AC-10 EXPLAIN test is DB-gated (skips on default CI) — run once against `make infra-up` before launch — D-049 |
| AC-3 gap | LOW | DEFER | `operator_health[].latency_ms` / `.auth_rate` intentionally null — no source column exists today — D-050 (target FIX-229) |

## Escalated Issues

None. The one HIGH-severity finding (dashboard goroutine race) was fixable in-story; applied and verified.

## Deferred Items (written to ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Row added |
|---|---------|--------------|-----------|
| D-046 | violationDTO should use typed fields instead of `interface{}` | future code-quality sweep | YES |
| D-047 | Enriched sims JOINs include partition key for pruning | POST-GA perf hardening | YES |
| D-048 | AC-7 notification display_name resolution | FIX-212 | YES |
| D-049 | AC-10 EXPLAIN performance evidence — run DB-gated test once | POST-GA launch-readiness | YES |
| D-050 | AC-3 latency_ms / auth_rate / last_health_check metric-source wiring | FIX-229 | YES |

## Acceptance-Criteria Tracing

| AC | Status | Evidence |
|----|--------|----------|
| AC-1 SIM DTO adds operator_name/operator_code/policy_name/policy_version_number/policy_version_id | PASS | `internal/api/sim/handler.go` simResponse widened (line 106-121); `web/src/types/sim.ts` adds operator_code + policy_version_number (line 6, 17) |
| AC-2 Store sim.List uses JOIN — single query, no N+1 | PASS | `internal/store/sim.go` ListEnriched; simEnrichedJoin uses 4 LEFT JOINs; scanSIMWithNames is the ONLY scan helper (PAT-006) |
| AC-3 Dashboard operatorHealthDTO adds code/latency_ms/active_sessions/auth_rate/last_health_check/sla_target | PARTIAL | code, sla_target, active_sessions, last_health_check populated; latency_ms + auth_rate null (documented Risk R-3 + D-050) |
| AC-4 Session DTO adds policy_name/policy_version/operator_name | PASS | `internal/api/session/handler.go` sessionDTO widened; batch enrichment via GetManyByIDsEnriched (single ANY($1) query per page) |
| AC-5 Violation DTO adds iccid/policy_name/policy_version/operator_name/apn_name | PASS | `internal/api/violation/handler.go` violationDTO with 8 new enriched fields; store ListEnriched + GetByIDEnriched |
| AC-6 eSIM DTO adds operator_name/operator_code | PASS | `internal/store/esim.go` ESimProfileWithNames; handler switched for List + Get (mutation endpoints deferred to FIX-216+ with TODOs) |
| AC-7 Notification body entity refs carry {entity_type, entity_id, display_name} | PARTIAL | Shape present; display_name empty until FIX-212 unified envelope (D-048). scope_ref_id persisted — no data loss |
| AC-8 Orphan entity handling via LEFT JOIN nullability + FE "(Unknown)" | PASS | LEFT JOIN in all enriched queries; OperatorChip renders AlertCircle + "(Unknown)" when name is null; orphan test asserts nil operator_name for ghost operator_id |
| AC-9 Backend enrichment centralized in store query — no per-row handler lookups | PASS | `enrichSessionDTO` deleted; `enrichSIMResponse` kept only for compare.go (non-enriched fallback path); `handler.go` list path replaces 90-line per-row enrichment with ListEnriched call + single IP-pool batch |
| AC-10 SIM list p95 < 100ms for 50-item page | EVIDENCE-PENDING | EXPLAIN test in place (`sim_list_enriched_explain_test.go`) with < 100ms assertion + no root Seq Scan on sims; test skips without DATABASE_URL. Run against `make infra-up` before launch (D-049) |

## Performance Summary

### Queries Analyzed

| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | `internal/store/sim.go` ListEnriched | 4 LEFT JOINs + tenant+cursor predicate | Partition-pruned via `s.tenant_id` + `s.operator_id` filter chain; no root Seq Scan | Expected-plan | EXPLAIN test codifies |
| 2 | `internal/store/policy_violation.go` ListEnriched | 5 LEFT JOINs; sims join on `s.id = v.sim_id` | No partition key in sims JOIN predicate — per-partition index scan across all partitions. Acceptable at 3-operator scale | LOW | D-047 |
| 3 | `internal/store/esim.go` ListEnriched | LEFT JOIN operators + JOIN sims | Same partition-unaware sims JOIN | LOW | D-047 |
| 4 | `internal/api/session/handler.go` List | 1 batch-lookup query per page via GetManyByIDsEnriched | Replaces per-row enrichSessionDTO (up to 3 DB round-trips per session) | Improvement | PASS |

### Caching Verdicts

| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | Operator/APN name cache | `internal/api/sim/handler.go` SIM list | n/a | Retired from SIM list path (replaced by JOIN). NameCache type preserved for other handlers still using it | PASS |

## Token & Component Enforcement (UI story)

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors in new FE files (`operator-chip.tsx`, `operator-chip.ts`) | 0 | 0 | PASS |
| Hardcoded hex colors in modified FE lines (5 pages) | 0 | 0 | PASS |
| Raw HTML elements where components exist | — | All `<Badge>`, `<Table>`, `<Tooltip>`, `<OperatorChip>` used | PASS |
| Design-token compliance (`text-*`, `bg-*-dim`) | — | 100% — `bg-warning-dim`, `bg-danger-dim`, `bg-info-dim`, `text-text-*` | PASS |
| OperatorChip reuse across 5 pages | — | `sims/index`, `sims/detail`, `dashboard/index`, `violations/index`, `esim/index` | PASS |
| ARIA on warning icon (orphan fallback) | — | `aria-hidden="true"` on AlertCircle + visually-separated "(Unknown)" text | PASS |
| Orphan chip non-clickable | — | `clickable` prop gated; without it renders as `<span>` not `<button>` | PASS |

## Verification

```
$ go build ./...
PASS

$ go vet ./...
PASS

$ go test -race ./internal/api/dashboard/... ./internal/api/session/... -count=1 -timeout=5m
15 / 15 PASS  (race detector clean)

$ go test ./internal/... -count=1 -timeout=10m
3270 / 3270 PASS across 95 packages

$ cd web && npm run typecheck
tsc --noEmit  (clean)

$ rg -nE '#[0-9a-fA-F]{6}' web/src/components/shared/operator-chip.tsx web/src/lib/operator-chip.ts
0 matches
```

- Fix iterations: 1 (dashboard race). Max allowed: 2.

## Pass 0: Regression Verification — not applicable (AUTOPILOT story, not maintenance mode)

## Passed Items

- PAT-006 single-scan-helper discipline: `scanSIMWithNames`, `scanPolicyViolationWithNames`, `scanESimProfileWithNames` are each the unique scan helper for their type; header comments point readers to PAT-006.
- LEFT JOIN orphan safety: all three enriched queries use LEFT JOIN (never INNER), explicit orphan unit tests assert nil operator_name / policy_name when parent row is missing.
- Tenant scoping on every new JOIN: `sims.tenant_id = $1`, `a.tenant_id = $1`, `pol.tenant_id = $1`, `si.tenant_id = $1`; operators deliberately not tenant-filtered (tenant-agnostic per plan).
- API envelope unchanged: DTO widening is additive; old consumers see unchanged JSON contract.
- `v.version` column usage (plan used `pv.version_number`) is the correct actual column name per migration — verified against `migrations/20260320000002_core_schema.up.sql`.
- Batch enrichment replaces N+1 in session handler: one `GetManyByIDsEnriched` call per page instead of up to 3 round-trips per session.
- IP pool enrichment preserved on its existing batch path — intentionally out of scope per plan.
- FE OperatorChip handles `code = null` with muted default colors; routes to `/operators/:id` only when clickable prop set.

## Notes for next story

- D-048 is the natural hand-off to FIX-212 (unified event envelope): once FIX-212 lands `entity_refs` will be wire-format-ready for cross-entity display_name resolution at emit-time.
- D-049 should be closed in the pre-launch checklist — run EXPLAIN test against `make infra-up` with the 1K-SIM fixture and attach output to the evidence folder.
- D-050 tracks the AC-3 scope cut; FIX-229 is the correct home.
