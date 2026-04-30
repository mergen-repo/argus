# Codex DONE Stories Audit — Round 2

- Scope: second-pass sweep of `ROUTEMAP.md` stories marked `[x] DONE`
- Status: `fail`
- Stories With Findings: `2`
- Findings: `3`
- Method: placeholder/no-op/mock-data/doc-code drift sweep, then mapped back to DONE story ACs

## Findings

### 1. [MEDIUM] STORY-044 usage tab still omits the required CDR table
- Expected:
  STORY-044 defines the SIM detail usage tab as `usage chart (30-day trend), CDR table`.
- Actual:
  The current `UsageTab` now renders a real chart and summary totals, but there is still no CDR list/table in the tab at all. The component ends after the chart card and summary card.
- Evidence:
  - `docs/ROUTEMAP.md:192`
  - `docs/stories/phase-8/STORY-044-frontend-sim.md:18`
  - `docs/stories/phase-8/STORY-044-frontend-sim.md:34`
  - `web/src/pages/sims/detail.tsx:311`
  - `web/src/pages/sims/detail.tsx:331`
  - `web/src/pages/sims/detail.tsx:402`
  - `web/src/pages/sims/detail.tsx:429`

### 2. [HIGH] STORY-073 tenant resource trend sparklines are still hardcoded zero arrays
- Expected:
  STORY-073 AC-1 requires per-tenant cards with spark trend per metric.
- Actual:
  The backend still emits `Spark: make([]int, 7)` for every tenant and comments that real time-series integration is for a future story. The frontend renders that array directly through `SparkBar`, so the delivered “trend” is still a placeholder instead of real tenant movement.
- Evidence:
  - `docs/ROUTEMAP.md:192`
  - `docs/stories/phase-10/STORY-073-admin-compliance-screens.md:31`
  - `docs/stories/phase-10/STORY-073-gate.md:5`
  - `internal/api/admin/tenant_resources.go:22`
  - `internal/api/admin/tenant_resources.go:55`
  - `internal/api/admin/tenant_resources.go:57`
  - `web/src/pages/admin/tenant-resources.tsx:18`
  - `web/src/pages/admin/tenant-resources.tsx:138`
  - `web/src/pages/admin/tenant-resources.tsx:170`

### 3. [HIGH] STORY-073 delivery status board is only partially real: email/telegram are stubbed and the failed-delivery workflow is absent
- Expected:
  STORY-073 AC-12 requires per-channel success/failure/retry depth/latency health plus a failed delivery list with retry button.
- Actual:
  The backend hardcodes email and telegram to `success_rate=1.0` and marks telegram as “not instrumented yet”. The frontend page renders only summary cards; there is no failed-delivery list and no retry action anywhere on the screen.
- Evidence:
  - `docs/ROUTEMAP.md:192`
  - `docs/stories/phase-10/STORY-073-admin-compliance-screens.md:40`
  - `docs/stories/phase-10/STORY-073-gate.md:5`
  - `internal/api/admin/delivery_status.go:67`
  - `internal/api/admin/delivery_status.go:70`
  - `web/src/pages/admin/delivery.tsx:38`
  - `web/src/pages/admin/delivery.tsx:50`
  - `web/src/pages/admin/delivery.tsx:84`
  - `web/src/pages/admin/delivery.tsx:149`
  - `web/src/pages/admin/delivery.tsx:157`

## Recommendations

1. Re-open `STORY-073` for two concrete follow-ups:
   `AC-1` tenant trend sparklines must read real tenant history rather than zero arrays.
   `AC-12` must either add real email/telegram instrumentation plus failed-delivery drilldown/retry, or be downgraded from DONE/PASS.
2. Re-open `STORY-044` usage-tab scope to add the missing CDR table, since the chart-only implementation does not satisfy the story contract.
3. After fixes, update gate/review docs so they stop claiming full PASS on acceptance criteria that are still delivered as placeholders or reduced surfaces.
