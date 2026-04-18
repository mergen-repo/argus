# Post-Story Review: STORY-090 — Multi-protocol operator adapter refactor

> Date: 2026-04-18

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-089 | STORY-090 settled the nested-JSONB `adapter_config.{radius,diameter,sba,http,mock}` shape. STORY-089's "seed update so operator rows point to simulator endpoints" (ROUTEMAP line 257) was written as a singular operation. Post-STORY-090 each operator has up to 5 independent protocol slots; the seed task must populate per-protocol sub-keys (e.g. `adapter_config.radius.host`, `adapter_config.http.base_url`). The per-protocol test endpoint (API-307) and `enabled_protocols[]` field are the stable surface STORY-089 should exercise to verify the simulator receives correct protocol config. D-039 (pre-existing AUSF/UDM/NRF api/_index.md gap) is also STORY-089's target and is unblocked by STORY-090 (SBA adapter now indexed as a first-class protocol). | NEEDS_PLAN_REVISION — Amil to dispatch STORY-089 plan update: seed task expanded to per-protocol sub-keys; D-039 SBA api/_index.md re-sweep is in scope. |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/architecture/ERROR_CODES.md | Added `PROTOCOL_NOT_CONFIGURED` (422) row to Operator Errors table; added `CodeProtocolNotConfigured` to Go constants block | UPDATED |
| docs/architecture/api/_index.md | Added API-306 (`GET /api/v1/operators/:id` detail) and API-307 (`POST /api/v1/operators/:id/test/:protocol`); updated section header 8 → 10 endpoints; amended API-024 description to reference per-protocol variant | UPDATED |
| docs/architecture/db/operator.md | Dropped `adapter_type` row from TBL-05 column table; updated `adapter_config` description to reflect nested per-protocol shape + AES-GCM envelope note | UPDATED |
| docs/PRODUCT.md | Operator entity field list: replaced `adapter_type` with `adapter_config (nested per-protocol)` + drop-migration note | UPDATED |
| docs/ARCHITECTURE.md | `internal/operator/` project structure tree: updated adapter/ comment; added `adapterschema/` package entry | UPDATED |
| docs/screens/SCR-041-operator-detail.md | Added `[Protocols]` to tab bar mockup; added API-306 and API-307 to API References | UPDATED |
| docs/USERTEST.md | Appended `## STORY-090` section — 7 walkthrough scenarios: Protocols tab first render, header chip derivation, per-protocol Test Connection, PROTOCOL_NOT_CONFIGURED 422, DB column absence, secret masking, sentinel sweep | UPDATED |
| docs/brainstorming/bug-patterns.md | Added PAT-003 (metric label set not expanded before write-fanout), PAT-004 (N×M goroutine per tick), PAT-005 (masked-secret PATCH round-trip) | UPDATED |
| docs/ROUTEMAP.md | STORY-090 row Status `[~] IN PROGRESS / Review / —` → `[x] DONE / — / 2026-04-18`; header counter `1/3 → 2/3`; Change Log entry added | UPDATED |
| docs/CLAUDE.md | Active Session: Phase counter `1/3 → 2/3`; Story `STORY-090 → STORY-089`; Step `Review → Plan` | UPDATED |
| docs/brainstorming/decisions.md | VAL-028..VAL-032 already appended by gate — NO_CHANGE needed by reviewer | NO_CHANGE |
| docs/GLOSSARY.md | No new domain terms introduced (all adapter/protocol concepts already implicit in STORY-018/021 glossary scope) | NO_CHANGE |
| docs/FRONTEND.md | ProtocolsPanel uses only existing design tokens (verified by `grep -E '#[0-9a-fA-F]|text-\[|bg-\[' ProtocolsPanel.tsx` → 0 hits); no new component class names requiring FRONTEND.md update | NO_CHANGE |
| docs/FUTURE.md | No STORY-090 future-opportunity implications; no stale references found | NO_CHANGE |
| Makefile | No new targets or env vars introduced by STORY-090 | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0 remaining after fixes
- Pre-fix: `adapter_type` column present in `db/operator.md`, `PRODUCT.md`, and SCR-041 tabs omitting Protocols — all three fixed.
- `supported_protocols` appeared in `docs/USERTEST.md` as curl request payloads in pre-090 STORY sections — those are historical curl examples, acceptable as immutable historical records. Not removed.
- VAL-028..VAL-032 present in decisions.md; Grafana dashboard + Prometheus alert rule updated in-story per VAL-028. ARCHITECTURE.md did not enumerate metric names so no update needed there.
- STORY-009-plan.md / STORY-018-review.md / STORY-026-plan.md reference `adapter_type` — these are immutable historical story files. Per reviewer protocol: no edits to past story files.

## Decision Tracing

- Decisions checked: 4 planner decisions (D1-A, D2-B-user-override, D3-B, D4-A) + 5 gate validation entries (VAL-028..VAL-032)
- D1-A (lazy upconvert flat→nested in Go, not SQL): Reflected in `internal/operator/adapterschema/upconvert.go` + `handler.go` Create/Update normalizer path. PASS.
- D2-B user override (drop `adapter_type` column): Reflected in `migrations/20260418120000_drop_operators_adapter_type.{up,down}.sql`; `internal/store/operator.go` reader sweep confirms zero non-test/non-migration references (AC-13 grep guard). PASS.
- D3-B (per-protocol health checker fan-out, single ticker per operator): Reflected in `internal/operator/health.go` `startOperatorLoop` + F-A5 collapse (VAL-031). PASS.
- D4-A (router resolves by protocol arg): Reflected in `internal/operator/router.go` + `failover.go` + all test files. PASS.
- VAL-028..VAL-032: verified in-code at gate + confirmed by test names cited in gate report. PASS.
- Orphaned decisions: 0

## USERTEST Completeness

- Entry exists: YES (added by this review)
- Type: UI scenarios (Protocols tab, header chip, per-protocol Test Connection, PROTOCOL_NOT_CONFIGURED 422, DB column absence, secret masking) + backend sentinel sweep

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting STORY-090 in Tech Debt table: 0 (no pre-existing D-NNN row targeted this story)
- D-029 (seed CI guard, POST-GA): still PENDING — unrelated to STORY-090 scope, target unchanged
- D-039 (AUSF/UDM/NRF api/_index.md gap, target STORY-089): still PENDING — STORY-090 did not absorb SBA mock; scope discipline maintained
- Reviewer-resolved (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

## Mock Status (Frontend-First projects only)

- N/A — Argus is not a Frontend-First project. `src/mocks/` does not exist.
- Mock adapter sweep: `NewAdapter()` function in `registry.go` retains a `default: NewMockAdapter(...)` fallback, but this function is unused in production paths (only defined; all production callers go through `GetOrCreate` → `CreateAdapter` which returns an error on unknown types). Seeds intentionally contain `mock.enabled=true` sub-blobs per VAL-032 (dev/simulator path). Zero production routing defaults to mock when no protocol configured (AC-13 guard clean). PASS.

## Evidence Screenshot Status

The 6 PNGs in `docs/stories/test-infra/STORY-090-evidence/` were captured during Wave 3 BEFORE the F-A2 fix landed. Pre-F-A2 behaviour: Protocols tab showed "all disabled on first render" because `useOperator` fetched the slim list-response (no `adapter_config`). Post-F-A2: `useOperator` hits `GET /api/v1/operators/:id` detail endpoint, `ProtocolsPanel` useEffect syncs state on server-state arrival. Screenshots are functionally stale for AC-4 visual certification. Covered by `TestOperatorResponse_AdapterConfigSerialization` + 4 mask/restore tests. Re-capture recommended on next operator-UI touch.

## Issues

> Every issue MUST have a Resolution. NEVER write an issue without one.

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | ERROR_CODES.md Operator Errors table missing `PROTOCOL_NOT_CONFIGURED` | NON-BLOCKING | FIXED | Added row (422) to operator error table and `CodeProtocolNotConfigured` to Go constants block. Added STORY-090 Gate F-A3 (VAL-030) reference. |
| 2 | api/_index.md missing GET /operators/:id detail endpoint (API-306) and POST /operators/:id/test/:protocol (API-307) | NON-BLOCKING | FIXED | Added both rows; section header updated 8→10; API-024 description updated to point readers to per-protocol variant. |
| 3 | db/operator.md TBL-05 still lists `adapter_type VARCHAR(30)` column (dropped in STORY-090 D2-B) | NON-BLOCKING | FIXED | Removed `adapter_type` row; updated `adapter_config` description to nested per-protocol shape + AES-GCM envelope note. |
| 4 | PRODUCT.md Operator entity field list still references `adapter_type` | NON-BLOCKING | FIXED | Replaced `adapter_type` with `adapter_config (nested per-protocol)`; added migration-drop note. |
| 5 | ARCHITECTURE.md project structure tree: `internal/operator/` missing `adapterschema/` package and updated `adapter/` comment | NON-BLOCKING | FIXED | Added `adapterschema/` entry; updated `adapter/` comment to reference 5-protocol registry and STORY-090. |
| 6 | SCR-041 operator-detail screen mockup missing Protocols tab from tab bar + missing API-306/API-307 references | NON-BLOCKING | FIXED | Added `[Protocols]` to tab bar; added API-306 + API-307 to API References section. |
| 7 | USERTEST.md has no STORY-090 section (UI story → UI scenarios required) | NON-BLOCKING | FIXED | Appended 7-scenario STORY-090 section covering Protocols tab, header chip, per-protocol Test Connection, PROTOCOL_NOT_CONFIGURED, DB column absence, secret masking, sentinel sweep. |
| 8 | bug-patterns.md missing 3 new architectural patterns from STORY-090 Gate (F-A1 metric fan-out, F-A5 goroutine cardinality, F-A2 masked-secret PATCH round-trip) | NON-BLOCKING | FIXED | Added PAT-003 (metric label not expanded before fan-out), PAT-004 (N×M goroutine per tick), PAT-005 (masked-secret PATCH round-trip sentinel persistence). |
| 9 | ROUTEMAP.md STORY-090 row still `[~] IN PROGRESS / Review / —` after gate PASS | NON-BLOCKING | FIXED | Updated row to `[x] DONE / — / 2026-04-18`; counter `1/3 → 2/3`; Change Log STORY-090 DONE entry added. |
| 10 | CLAUDE.md Active Session still points to STORY-090 / Review step | NON-BLOCKING | FIXED | Advanced to STORY-089 / Plan; phase counter `1/3 → 2/3`. |
| 11 | Evidence PNGs in STORY-090-evidence/ stale (pre-F-A2: Protocols tab shows "all disabled" before server state arrives) | NON-BLOCKING | ACKNOWLEDGED | Noted in Evidence Screenshot Status section. Functional coverage by `TestOperatorResponse_AdapterConfigSerialization` + 4 mask/restore tests is complete. Re-capture scheduled on next operator-UI story touch. No D-item created (not an architectural gap). |
| 12 | STORY-089 planner needs to know adapter_config is now per-protocol (seed task expansion) | NON-BLOCKING | REPORT ONLY | Impact table entry above. Amil to dispatch STORY-089 plan update. |

## Project Health

- Stories completed in Runtime Alignment: 2/3 (STORY-092 DONE 2026-04-18; STORY-090 DONE 2026-04-18; STORY-089 PENDING)
- Current phase: Runtime Alignment [IN PROGRESS]
- Next story: STORY-089 (Operator SoR Simulator)
- Blockers: None. STORY-089 can begin Plan step. D-039 (AUSF/UDM/NRF api/_index.md gap) is in STORY-089 scope per prior decision.
