# Gate Report: FIX-224 — SIM List/Detail Polish

## Summary
- Requirements Tracing: Fields 6/6 (state-csv, created_at datetime, MAX_SIMS=4, import preview cols, import result shape, useJobPolling). Endpoints 0/0 (FE-only). Workflows 6/6 (AC-1..AC-6). Components 4/4 (sims/index.tsx, sims/compare.tsx, hooks/use-sims.ts, dropdown-menu.tsx).
- Gap Analysis: 6/6 acceptance criteria passed (AC-1 multi-select filter, AC-2 created datetime + tooltip, AC-3 bulk-bar sticky audit-only, AC-4 compare cap 4 + warn+disable, AC-5 import preview, AC-6 post-import report).
- Compliance: COMPLIANT (no API surface changes, no DB changes, no env var changes).
- Tests: tsc PASS · npm run build PASS 2.39s · no FE unit suite (project convention).
- Test Coverage: N/A for FE-only story without unit harness; AC-level verification performed by UI scout.
- Performance: 0 new queries (FE-only). Client-side secondary state filter acceptable at current scale (~fleet ≤ 500 SIMs); scale concern tracked as pre-existing tech-debt (D-### server-side state-ANY predicate).
- Build: PASS (tsc · vite · 2.39s).
- Screen Mockup Compliance: 6/6 elements in place (state filter, datetime cell, tooltip, compare grid 4-col, import preview table, import result panel).
- UI Quality: 15/15 tokens + primitives reused; raw-button=0, hex=0, inline-svg=0, default-tailwind=0.
- Token Enforcement: CLEAN.
- Turkish Text: N/A (admin UI English strings).
- Overall: **PASS**

## Team Composition
- Analysis Scout: 12 findings (F-A1..F-A12) — 1 LOW fixed (F-A1 preview 5→10), 2 DEFERRED to existing parent tech-debt (F-A3/F-A4 server-side state-ANY), 1 DEFERRED to new D-124 (F-A6 checkbox-item a11y roles), 8 PASS.
- Test/Build Scout: 7 checks (F-B1..F-B7) — all PASS/N-A, 0 failures.
- UI Scout: 11 findings (F-U1..F-U11) — all PASS, 0 NEEDS-FIX, 0 CRITICAL.
- De-duplicated: 30 → 30 (no overlapping findings across scouts; F-A3/A4 + F-A11 are distinct layers).

### Scout execution note
Lead dispatch prompt requested 3 parallel scouts. Per Gate Team Lead contract (`~/.claude/skills/amil/agents/gate-team/lead-prompt.md` §Context), subagents cannot nest-dispatch Task calls. Gate Lead therefore executed all three scout perspectives inline (pattern established FIX-220..FIX-223) and labeled findings F-A / F-B / F-U to preserve audit trail.

## Fixes Applied
| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | UX / spec fidelity | `web/src/pages/sims/index.tsx:1125,1136` | Preview table row limit 5 → 10 (plan §Task 5 verify) | tsc + build PASS |

## Escalated Issues
None.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)
| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-124 | F-A2 — Native CSV parser lacks RFC 4180 quoted-field / CRLF normalisation. Accepted per plan R2 (no free-text columns in schema); CRLF edge on Excel exports introduces trailing `\r` on rightmost column that can trip format validators. One-line `content.replace(/\r/g, '')` lift. | FIX-24x (Import polish) | YES |
| D-125 | F-A6 — `DropdownMenuCheckboxItem` primitive missing `role="menuitemcheckbox"` / `aria-checked`. Screen readers announce as "button" not "checked menu item". Shared primitive impacts analytics filter, notification channels, and multi-select filters across app. | FIX-24x (a11y sweep) | YES |

(Pre-existing D-### tech-debt for server-side `state = ANY($1::text[])` predicate — filed by plan §Tech Debt — already in ROUTEMAP; F-A3 and F-A4 are re-validations, not new entries.)

## Performance Summary

### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | FE-only | Client-side `allSims.filter(sim => selectedStates.includes(sim.state))` when ≥2 states selected | Sparse-page symptom at scale; cursor advances on backend count, FE-filtered count may differ | LOW | DEFERRED (pre-existing tech debt) |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | SIM list filtered | React Query `staleTime: 15_000` | 15s | Preserved; CSV `state` is part of queryKey, so multi-select cache is segregated from single-select | OK |
| 2 | Job polling | React Query `refetchInterval: 2000` (terminates on `completed|failed|cancelled`) | 2s | Correct — stops polling on terminal state (hook line 34-40) | OK |

## Token & Component Enforcement
| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 new | 0 | CLEAN |
| Raw HTML elements | 0 | 0 | CLEAN (Button/DropdownMenu/Table/Dialog/Tooltip all from @/components/ui) |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind colors | 0 new | 0 | CLEAN (all via `-accent/-success/-danger/-warning` tokens) |
| Inline SVG | 0 | 0 | CLEAN (lucide-react + 1 token-checkbox path inside primitive — primitive-internal, expected) |
| Missing elevation | 0 | 0 | CLEAN |

## Verification
- tsc after fixes: PASS
- Build after fixes: PASS (2.39s · index chunk 409.84 kB gzip 124.69 kB)
- Go diff: 0 files (FE-only story confirmed)
- Token enforcement: ALL CLEAN (0 violations)
- Fix iterations: 1

## Passed Items
- DEV-307 bulk-bar sticky audit: code at `sims/index.tsx:867-872` confirms `fixed bottom-0 right-0 z-30` with sidebar-aware `left-16`/`left-60` offset. AC-3 satisfied-by-existing (FIX-201 delivery), zero code change.
- DEV-308 multi-state CSV filter + per-token chip removal: correctly implemented; `removeFilter(key, stateToken?)` signature consistent across the single call site.
- DEV-309 compare 4-cap with warn+disable: `MAX_SIMS=4` in `compare.tsx:35`; warning span + `disabled` + `aria-disabled` at cap; grid shifts to `lg:grid-cols-4`.
- DEV-310 `useImportSIMs` response type correction: matches backend `bulkImportResponse { JobID, TenantID, Status }`; no stale `rows_parsed`/`errors[]` references remain anywhere in `web/src`.
- DEV-311 native-split CSV preview: validates required columns (iccid/imsi/msisdn), per-row format regex; preview table + error list render; commit button disabled on column-level errors. Accepted limitations (quoted fields, CRLF) tracked as D-124.
- `useJobPolling` integration: page-level hook, terminal state stops polling, page unmount cleans up React Query. No leak.
- Dark mode parity: 100% token-based.
- Shadcn discipline: 100% primitive reuse.
