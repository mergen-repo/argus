# Review Report: STORY-070 — Frontend Real-Data Wiring

> Reviewer: post-gate review agent
> Date: 2026-04-13
> Gate result going in: PASS (2576 tests, 3 gate fixes, 14/14 ACs)

---

## Check 1 — Next-Story Impact (REPORT-ONLY)

**STORY-071 (Roaming Agreement Management):** No impact from STORY-070. Roaming store layer is independent; no shared tables modified. New `useSearchParams` URL-filter pattern is a positive precedent to follow.

**STORY-072 (Enterprise Observability Screens):** `useAPNTraffic`, `useOperatorMetrics`, `useOperatorHealthHistory` hooks introduced here are building blocks for STORY-072 observability panels. No conflicts. `useCapacity` will likely be extended in STORY-072.

**STORY-073 (Multi-Tenant Admin & Compliance Screens):** Violation acknowledgment migration (`20260413000003`) and `ErrViolationNotFound` sentinel will be reused by STORY-073's compliance audit views without modification.

**STORY-075 (Cross-Entity Context):** `ws-indicator.tsx` and the `wsClient.getStatus/onStatus/reconnectNow` public API are foundational for STORY-075 status surfaces. No conflict.

**DEV-201 constraint preserved:** `emptyReportProvider` stub intact; `GET /reports/definitions` returns hardcoded definitions and does NOT replace the report engine stub. STORY-070 did not violate the DEV-201 constraint.

---

## Check 2 — Architecture Doc (UPDATED)

**Finding:** `docs/ARCHITECTURE.md` scale line reads `166 APIs`; STORY-070 adds 6 new endpoints → correct count is 172.

**Resolution:** Updated `docs/ARCHITECTURE.md` line 4: `Scale: Large (166 APIs, …)` → `Scale: Large (172 APIs, …)`.

---

## Check 3 — API Index (UPDATED)

**Finding:** `docs/architecture/api/_index.md` — 6 new endpoints missing (operators health-history, operators metrics, APNs traffic, violations acknowledge, reports definitions, system capacity). Footer reads 166.

**Resolution:** Added "Operator Detail Analytics (2 endpoints) — STORY-070", "APN Detail Analytics (1 endpoint) — STORY-070", "Violation Remediation (1 endpoint) — STORY-070", "Report Definitions (1 endpoint) — STORY-070", "System Capacity (1 endpoint) — STORY-070" sections (API-224..229). Footer updated to 172.

---

## Check 4 — DB / Migration Doc (CLEAN)

Migration `20260413000003_violation_acknowledgment` adds `acknowledged_at`, `acknowledged_by`, `acknowledged_note` columns and a partial index on `policy_violations`. `docs/architecture/db/_index.md` lists `policy_violations` as TBL-28 (added in STORY-064). The migration is additive and reversible; no new table — no new TBL-NNN entry needed. CLEAN.

---

## Check 5 — Config Doc (UPDATED)

**Finding:** `docs/architecture/CONFIG.md` — 4 new capacity env vars (`ARGUS_CAPACITY_SIM`, `ARGUS_CAPACITY_SESSION`, `ARGUS_CAPACITY_AUTH`, `ARGUS_CAPACITY_GROWTH_SIMS_MONTHLY`) present in `internal/config/config.go` (lines 211-214) but absent from CONFIG.md.

**Resolution:** Added "Capacity Targets (SVC-01)" section to CONFIG.md.

**Finding:** `.env.example` — same 4 vars missing.

**Resolution:** Added ARGUS_CAPACITY_* block to `.env.example`.

---

## Check 6 — GLOSSARY (UPDATED)

**Finding:** Two new domain concepts introduced without glossary entries:
1. **Traffic Heatmap** — 7×24 CDR bucketing into hour/weekday heatmap grid, introduced in STORY-070 dashboard and APN detail panels.
2. **Violation Acknowledgment** — operator acknowledging a policy violation with optional note, closing the investigation loop.

**Resolution:** Added both terms to `docs/GLOSSARY.md` (Analytics & Reporting section and Policy Engine section respectively).

---

## Check 7 — SCREENS.md (CLEAN)

STORY-070 is a "rewire" story — no new screens added, existing screens enriched with real data. SCREENS.md header count unchanged. CLEAN.

---

## Check 8 — PRODUCT.md (CLEAN)

No new feature flags referenced. All changed features (APN traffic charts, violation acknowledgment, capacity targets) are already covered under existing feature IDs (F-024 analytics, F-009 policy engine). CLEAN.

---

## Check 9 — USERTEST.md (UPDATED)

**Finding:** STORY-070 section absent from `docs/USERTEST.md`. UI story with 14 ACs requires full user test scenarios.

**Resolution:** Added STORY-070 section with 19 test scenarios (backend verification + frontend walkthroughs + operations).

---

## Check 10 — Story File (REPORT-ONLY)

Story file `docs/stories/phase-10/STORY-070-frontend-real-data.md` is accurate. Step-log and gate report are complete. No updates needed to the story file itself.

---

## Check 11 — Decision Tracing (UPDATED)

Three non-obvious implementation choices lack decision records:

1. **DEV-206:** Operators page excluded from URL filter persistence (AC-11) — no filter UI on operators list page; only search-by-name which is not a filterable dimension requiring URL persistence. Documented.

2. **DEV-207:** Topology page uses `sim_count` as traffic approximation — real CDR traffic per-APN not available at topology render time without an additional aggregation query; `sim_count` from the enriched APN list is a proportional proxy and matches the existing FlowLine traffic prop type. Documented.

3. **DEV-208:** `GET /reports/definitions` returns hardcoded list in handler — consistent with DEV-201 (`emptyReportProvider`); report metadata is static config, not DB-driven, while the report engine evolves. Documented.

**Resolution:** Added DEV-206, DEV-207, DEV-208 to `docs/brainstorming/decisions.md`.

---

## Check 12 — FUTURE.md (CLEAN)

No new future opportunities or existing FTR invalidations from STORY-070. The `emptyReportProvider` (DEV-201) was already documented as a follow-up. CLEAN.

---

## Check 13 — Tech Debt Pickup (CLEAN)

Reviewed open tech debt items:
- D-001/D-002: `ip-pool-detail.tsx` and `apns/index.tsx` raw elements → targeting STORY-077. Not in STORY-070 scope. UNAFFECTED.
- D-003: Stale SCR IDs in story files → targeting STORY-062. UNAFFECTED.
- D-004/D-005: Resolved in STORY-059 close-out. CLOSED.

No tech debt items targeted STORY-070. CLEAN.

---

## Check 14 — Mock Sweep (CLEAN)

`web/src/mocks/` directory does not exist. Grep confirms:
- `Math.random` in `web/src`: 0 matches
- `mockUsageData`/`mockTimeline`/`mockAuthData`/`mockSimCount`/`mockTrafficMB`/`mockPoolUsed`/`mockPoolTotal`/`generateMockTraffic`/`generateMockFrequency`/`REPORT_DEFINITIONS`/`SCHEDULED_REPORTS`: 0 matches
- `placeholder.tsx`: deleted (AC-14)

CLEAN.

---

## Findings Summary

| # | Check | Type | Finding | Resolution |
|---|-------|------|---------|-----------|
| F-1 | Architecture Doc | UPDATED | ARCHITECTURE.md scale line 166→172 APIs | Fixed |
| F-2 | API Index | UPDATED | 6 new endpoints API-224..229 missing; footer 166→172 | Fixed |
| F-3 | Config Doc | UPDATED | 4 ARGUS_CAPACITY_* vars absent from CONFIG.md | Fixed |
| F-4 | Config Doc | UPDATED | 4 ARGUS_CAPACITY_* vars absent from .env.example | Fixed |
| F-5 | Glossary | UPDATED | "Traffic Heatmap" term missing | Fixed |
| F-6 | Glossary | UPDATED | "Violation Acknowledgment" term missing | Fixed |
| F-7 | User Tests | UPDATED | STORY-070 section absent from USERTEST.md | Fixed |
| F-8 | Decisions | UPDATED | DEV-206: operators URL filter exclusion undocumented | Fixed |
| F-9 | Decisions | UPDATED | DEV-207: topology sim_count approximation undocumented | Fixed |
| F-10 | Decisions | UPDATED | DEV-208: report definitions hardcoded in handler | Fixed |
| F-11 | ROUTEMAP | UPDATED | STORY-070 not marked DONE; counter 13→14/22 | Fixed |

**Total findings: 11 — All resolved (zero-deferral). No escalations.**

---

## Compliance Checklist

- [x] All new API endpoints: standard envelope, tenant scoping, RBAC middleware order
- [x] Violation acknowledgment: audit log via `audit.Emit`, sentinel error pattern
- [x] URL filter persistence: `useSearchParams` on 7 pages (sims, apns, sessions, jobs, audit, violations, esim)
- [x] WS indicator: `ws-indicator.tsx` with semantic tokens, mounted in topbar, `getStatus/onStatus/reconnectNow` exposed
- [x] Dead code removal: `placeholder.tsx` deleted, `Math.random` = 0
- [x] shadcn/ui: all new components use shadcn Dialog/DropdownMenu/Badge/Tooltip/Alert
- [x] Design tokens: 0 hex, 0 arbitrary px, 0 `bg-gray-*`/`text-gray-*` in story files
- [x] Migration: additive, reversible (up + down files present)
- [x] Performance: no N+1 (3 GROUP BY queries for APN enrichment), CDR aggregations reuse materialized views

---

## Build Verification (carried forward from Gate)

| Check | Result |
|-------|--------|
| `go test ./...` | 2576 / 0 |
| `tsc --noEmit` | PASS |
| `npm run build` | PASS (3.71s) |

---

## Overall: PASS
