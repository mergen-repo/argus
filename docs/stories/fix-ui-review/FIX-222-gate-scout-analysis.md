# FIX-222 — Scout Analysis Findings

Gate: FIX-222 Operator/APN Detail Polish
Role: Analysis (architecture, data flow, null-safety, hook correctness)

## Scope Covered
- `web/src/hooks/use-tab-url-sync.ts`
- `web/src/components/shared/kpi-card.tsx`
- `web/src/components/ui/info-tooltip.tsx`, `web/src/lib/glossary-tooltips.ts`
- `web/src/components/operators/EsimProfilesTab.tsx`
- `web/src/pages/operators/detail.tsx` (KPI row + tabs + alias redirect)
- `web/src/pages/apns/detail.tsx` (KPI row + tabs + top-operator client derivation)

<SCOUT-ANALYSIS-FINDINGS>

F-A1 | MEDIUM | EsimProfilesTab missing isError handling
- File: web/src/components/operators/EsimProfilesTab.tsx
- Problem: `useESimList` destructures `isLoading/hasNextPage/fetchNextPage/isFetchingNextPage` but NOT `isError`. On query failure, the component falls through to the empty-state row — user cannot distinguish "no profiles" from "request failed".
- Fixable: YES (inline, matches `HealthTimelineTab` pattern at detail.tsx:323-335).
- Fix applied: added `isError`/`refetch` destructure + early-return with AlertCircle + Retry button.

F-A2 | LOW | Alias-chain safety depends on current config
- File: web/src/hooks/use-tab-url-sync.ts
- Observation: The effect self-terminates under the current configs (`{circuit→health, notifications→alerts}`; neither target is a key). If someone later adds `old→circuit`, first redirect lands on `circuit`, second redirect on `health` — works, but not explicitly proven. An iteration guard or an aliases-closure pre-resolution would make this future-proof.
- Fixable: NO need today (no alias chains exist); documented as deferral candidate.
- Status: Noted; defer to D-120.

F-A3 | PASS | KPI null-safety
- Operator: `authRateSparkline` falls back to `[]` when buckets missing; `avgAuthRate` guards on empty array; `uptimePct` returns 0 when history empty; `activeSessionsCount` nullish-coalesces to 0; `simTotal` derived from flat-mapped pages (never NaN).
- APN: `traffic24h` guards on empty series; `topOperator` returns `—` on empty first page; `simTotal` flat-map fallback to 0. No NaN/undefined paths identified.

F-A4 | PASS | Top Operator disclosure subtitle present
- APN KPI Top Operator subtitle: `'Based on first 50 SIMs'` when `hasNextPage` true, else `'By SIM count'`. Satisfies DEV-301.

F-A5 | PASS | useTabUrlSync invalid-tab fallback
- When `?tab=invalid` arrives, `isValid=false` → `activeTab` falls back to `defaultTab`. Effect rewrites URL via `setSearchParams({replace:true})`. No infinite-loop risk (second render: `rawTab` equals canonical `defaultTab`, both guards false).

F-A6 | PASS | EsimProfilesTab state coverage (loading + empty)
- Loading: 5 skeleton rows; Empty: `EmptyState` with Cpu icon + copy; (Error: NOW present after F-A1 fix).

F-A7 | PASS | Glossary coverage
- `GLOSSARY_TOOLTIPS` contains 9 terms (MCC, MNC, EID, MSISDN, APN, IMSI, ICCID, CoA, SLA). InfoTooltip warns in dev when unknown term is used. All 11 call sites use known terms (verified via grep).

</SCOUT-ANALYSIS-FINDINGS>
