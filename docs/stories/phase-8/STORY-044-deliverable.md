# Deliverable: STORY-044 — Frontend SIM List & Detail

## Summary

Full SIM management frontend: data table with segment dropdown, filter bar, combo search, infinite scroll, bulk actions, multi-select. SIM detail with 5 tabs: overview (state/operator/APN/IP/policy), sessions, usage (chart), diagnostics (step-by-step wizard), and history (state timeline). Group-first UX with segments as primary navigation.

## Files Changed
| File | Purpose |
|------|---------|
| `web/src/types/sim.ts` | TypeScript types for SIM, segments, filters |
| `web/src/hooks/use-sims.ts` | TanStack Query hooks for all SIM APIs |
| `web/src/pages/sims/index.tsx` | Full SIM list page (SCR-020) |
| `web/src/pages/sims/detail.tsx` | Full SIM detail page (SCR-021, 5 tabs) |

## Key Features
- Data table: ICCID, IMSI, MSISDN, State badge, SIM type, RAT chip
- Segment dropdown: filter by saved segments
- Filter bar: state/operator/APN/RAT dropdowns + free-text combo search
- Combo search: auto-detect ICCID/IMSI/MSISDN by pattern
- Infinite scroll: IntersectionObserver-based cursor pagination
- Bulk actions: suspend/resume/terminate with confirmation dialog
- SIM detail 5 tabs: Overview, Sessions, Usage, Diagnostics, History
- State actions: context-dependent buttons (activate/suspend/resume/terminate)
- Diagnostics: run button + step-by-step results (pass/warn/fail)
- History: timeline with colored state badges, reason, who/when
- Empty states with CTA throughout

## Test Coverage
- TypeScript strict, npm run build clean
- 15 ACs verified (13 full, 2 partial: virtual scroll uses infinite scroll, usage tab has mock data)
