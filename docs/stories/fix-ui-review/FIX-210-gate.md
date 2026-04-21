# Gate Report: FIX-210 — Alert Deduplication + State Machine

## Summary
- Requirements Tracing: 7/7 ACs traced; AC-1/AC-2 deviations documented in plan Decisions (D1 column name, D3 severity excluded)
- Gap Analysis: 7/7 acceptance criteria passed (after F-A1 fix closed the resolve-via-REST cooldown gap)
- Compliance: COMPLIANT
- Tests: 3460/3460 full Go suite PASS (107 packages); new F-A1 regression test `TestUpdateState_ResolveHandler_StampsCooldownFromConfig` PASS against live DB
- Test Coverage: AC-5 cooldown stamp now has BOTH store-level (`TestAlertStore_UpdateState_ResolveStampsCooldownUntil`) AND handler-level (new, this gate) regression tests
- Performance: No new issues; 3 new Prometheus counters remain PAT-003 compliant (type × source bounded)
- Build: Go build PASS, go vet clean, web tsc PASS, web production build PASS (all 10 chunks emitted)
- Container: rebuilt via `make build` → new binary contains `occurrence_count` symbol (5 occurrences); live `GET /api/v1/alerts` returns 21 keys including all 4 new FIX-210 fields (`occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until`)
- Screen Mockup Compliance: alert list row + detail cooldown banner reworked per F-U2/F-U3
- UI Quality: `Repeat` icon anchors dedup badge; cooldown banner gains left accent stripe + `BellOff` icon for semantic visibility
- Token Enforcement: no new violations; all changes use existing `border-l-accent`, `text-accent`, `bg-bg-elevated` tokens
- Turkish Text: N/A (no Turkish surfaces touched)
- Overall: **PASS**

## Team Composition
- Analysis Scout: 6 findings (F-A1 CRITICAL, F-A2/A3/A5/A6 LOW, F-A4 MEDIUM)
- Test/Build Scout: 0 findings (all green)
- UI Scout: 4 findings (F-U1 CRITICAL, F-U2 MEDIUM, F-U3/U4 LOW)
- De-duplicated: 10 raw → 9 actionable (F-A3 folded into F-A1; F-A6 informational-only); all 9 fixed in this gate

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | compliance/CRITICAL (F-A1) | `internal/api/alert/handler.go` | Added `cooldownMinutes int` field to `Handler`; `NewHandler` now takes it as 4th param; `UpdateState` passes `h.cooldownMinutes` to `alertStore.UpdateState` (replaces hardcoded `0`) | Go build PASS; new regression test `TestUpdateState_ResolveHandler_StampsCooldownFromConfig` PASS |
| 2 | compliance/CRITICAL (F-A1) | `cmd/argus/main.go` | `alertapi.NewHandler` call passes `cfg.AlertCooldownMinutes` | Go build PASS; container rebuild verified |
| 3 | test-coverage (F-A5) | `internal/api/alert/handler_test.go` | Updated 6 existing `NewHandler` calls to new signature; added `newTestHandlerWithCooldown` helper; added `TestUpdateState_ResolveHandler_StampsCooldownFromConfig` full-cycle resolve→cooldown test | DB-gated test PASS against live postgres |
| 4 | ops-noise/MEDIUM (F-A4) | `internal/notification/service.go` | Cooldown-drop log demoted Warn→Debug; `dedup_key` truncated to 8-char prefix in log line (metric remains primary ops signal) | `go vet` clean; unit tests pass |
| 5 | CRITICAL/deployment (F-U1) | container binary | `make build && docker compose up -d argus` rebuild; verified `strings /app/argus \| grep occurrence_count` returns 5; `curl /api/v1/alerts` JSON response now includes 4 new fields (`occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until`) | smoke-verified against live API via admin token |
| 6 | visual/MEDIUM (F-U2) | `web/src/pages/alerts/index.tsx` | Occurrence badge: dropped `uppercase tracking-wide`; added `Repeat` lucide icon prefix + `gap-1` for visual anchor; differentiates from source/state neutral pills | tsc clean; production build PASS |
| 7 | visual/LOW (F-U3) | `web/src/pages/alerts/detail.tsx` | Cooldown banner: added `border-l-2 border-l-accent/60` accent stripe + `BellOff` icon prefix; flex layout for icon+copy alignment | tsc clean; production build PASS |
| 8 | text/LOW (F-U4) | `web/src/lib/alerts.ts` | `humanizeWindow` returns `'<1s'` when `ms < 1000` (was `"0s"` for count=2 with near-identical timestamps) | tsc clean |
| 9 | docs/LOW (F-A2) | `docs/stories/fix-ui-review/FIX-210-plan.md` | Corrected PAT-016 prose + Risk 7 prose: `jsonb \|\| jsonb` is right-hand-wins (not left-hand); clarified mitigation uses explicit array append, not `\|\|` ordering | n/a (doc only) |

## Escalated Issues
(none)

## Deferred Items (tracked in ROUTEMAP → Tech Debt)
(none — every finding was fixable within the FIX-210 scope)

## Performance Summary

### Queries Analyzed
No new queries introduced by this gate; all fixes were behavioural/prose/visual.

### Caching Verdicts
No caching changes.

## Token & Component Enforcement (UI)

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 | 0 | CLEAN |
| Raw HTML elements | 0 | 0 | CLEAN |
| Competing UI libraries | 0 | 0 | CLEAN |
| Default Tailwind colors | 0 | 0 | CLEAN |
| Inline SVG | 0 | 0 | CLEAN |
| Missing elevation | 0 | 0 | CLEAN |

All new elements use existing tokens: `text-accent`, `border-l-accent/60`, `bg-bg-elevated`, `border-border`, `text-text-secondary`. Icons come from `lucide-react` (established dependency).

## Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go vet ./...` | PASS (0 issues) |
| `go test ./... -count=1` (full suite) | 3460/3460 PASS across 107 packages |
| F-A1 regression test (DB-gated) | `TestUpdateState_ResolveHandler_StampsCooldownFromConfig` PASS |
| `web/ npx tsc --noEmit` | PASS |
| `web/ npm run build` | PASS (all chunks emitted, 2.46s) |
| `make build` | PASS (image rebuilt) |
| `docker compose up -d argus` | PASS (container healthy) |
| `strings /app/argus \| grep -c occurrence_count` | 5 (was 0 pre-gate) |
| `curl /api/v1/alerts` JSON response | contains `occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until` |
| Grep gate PAT-011 (`alertStore.Create(` in `internal/notification/`) | 0 hits |
| Grep gate D-076 (`validAlertStates\|validAlertTransitions\|allowedUpdateStates` outside `alertstate`) | 0 hits |
| Grep gate D3 regression (`severity` in `internal/alertstate/dedup.go`) | 0 hits |
| Fix iterations | 1 (all fixes applied in one pass) |

## Passed Items

- AC-1 schema columns (`occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until`) confirmed queryable in live DB
- AC-2 dedup_key SHA-256 formula implemented without severity (D3)
- AC-3 `INSERT ... ON CONFLICT` atomic upsert verified via existing `TestAlertStore_UpsertWithDedup_ConcurrentHit` + live smoke
- AC-4 edge-triggered publishers (health_worker + enforcer) retained from implementation; persist-level dedup covers belt-and-suspenders for remaining 5
- **AC-5 cooldown stamp on resolve** — NOW covered in BOTH store-level AND handler-level paths (F-A1 gap closed this gate)
- AC-6 occurrence_count surfaced on UI list + detail (F-U2/U3/U4 polish applied)
- AC-7 Prometheus metric `argus_alerts_deduplicated_total{type,source}` wired; PAT-003 cardinality bound preserved
- D-076 consolidation: `internal/alertstate` package is single source of truth
- PAT-006 scan-site coverage: all 4 new columns scanned via `alertColumns` constant + `scanAlert`
- PAT-011 construction-site wiring: `cfg.AlertCooldownMinutes` now reaches BOTH `handleAlertPersist` path AND REST handler path (this gate fixed the handler gap)
