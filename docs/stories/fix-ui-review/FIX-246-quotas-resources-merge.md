# FIX-246: Quotas + Resources Merge → Unified "Tenant Usage" Dashboard

## Problem Statement
`/admin/quotas` (readonly utilization view) + `/admin/resources` (current-usage view) overlap heavily — both show per-tenant tenant cards with SIMs/sessions/storage values. Two tabs for essentially same concern. User feedback (2026-04-19): merge into single zarif tenant-card dashboard.

Additional gaps:
- No quota breach alerts — tenant approaching 80% session limit has no warning (F-315). Alert Thresholds (F-233) belongs here, not in notification settings
- No quota edit on this page — edit is on Tenants page (correct separation; but link back to editor missing)
- Storage/session quotas M2M-unrealistic (F-316)
- "273.4 B" vs "27,304 bytes" display inconsistency (F-317)

## User Story
As a super_admin monitoring multi-tenant health, I want a single "Tenant Usage" dashboard showing each tenant's quota utilization with visual bars, breach alerts, and quick link to edit limits — so I can proactively manage tenant capacity.

## Architecture Reference
- Frontend: new consolidated page replacing 2 existing
- Backend: `/admin/tenants/quotas` + `/admin/tenants/resources` — unified endpoint OR both used

## Findings Addressed
- F-271 (plan mismatch — fix alongside)
- F-272 (quota utilization % gap)
- F-273 (state column missing)
- F-274 (user_count 0 orphan)
- F-314 (merge decision)
- F-315 (alert trigger)
- F-316 (unrealistic defaults)
- F-317 (unit bug)

## Acceptance Criteria
- [ ] **AC-1:** New page `/admin/tenant-usage` (primary) — legacy `/admin/quotas` + `/admin/resources` redirect here (301).
- [ ] **AC-2:** Per-tenant card design:
  ```
  ┌─ ABC Edaş (Starter) ──────────────── [OK] ─┐
  │ SIMs        108 / 100,000    ▓░░░░░ 0.1%   │
  │ Sessions     0 / 2,000        ░░░░░░ 0%    │
  │ API RPS      0 / 2,000        ░░░░░░ 0%    │
  │ Storage   27.3 KB / 10 GB     ░░░░░░ 0.0%  │
  │                                            │
  │ 📊 Active: 108 SIMs · 0 sessions · 27.3 KB │
  │ State: ACTIVE  [Edit limits →]             │
  └────────────────────────────────────────────┘
  ```
  - Plan chip (from tenant.plan — fix F-271 alignment)
  - 4 quota bars with current/max + utilization % + color gradient (green <50 / yellow 50-80 / red >80 / critical >95)
  - State chip (ACTIVE / SUSPENDED / TRIAL)
  - "Edit limits" link → `/system/tenants/{id}` (existing tenant detail edit)
- [ ] **AC-3:** Default view: Cards grid (4 col desktop, 2 col tablet, 1 col mobile). Toggle to Table view (dense).
- [ ] **AC-4:** Toolbar:
  - Search: tenant name/slug
  - Sort: utilization desc / name / plan
  - Filter: State (all/active/suspended/trial), Plan (all/starter/standard/enterprise), Breach threshold (show only >50% / >80% / >95%)
- [ ] **AC-5:** **Alert integration (F-233, F-315):**
  - Each quota has threshold: 80% → warning alert, 95% → critical alert (Info/Alerts subsystem FIX-209)
  - UI card shows pulsing ring when approaching breach
  - Alert thresholds **CONFIGURABLE** per-plan — admin sets "Starter: 80/95, Enterprise: 90/98"
  - Configuration in new "Quota Alert Settings" tab (future) — for now hardcoded 80/95 defaults
- [ ] **AC-6:** **Realistic defaults (F-316):** Seed quota update:
  - Starter plan: 10K SIMs, 2K sessions, 100 GB storage, 2K API RPS
  - Standard plan: 100K SIMs, 20K sessions, 1 TB storage, 5K API RPS
  - Enterprise plan: 1M SIMs, 200K sessions, 10 TB storage, 20K API RPS
  - Migration updates existing tenant quotas to plan defaults (conservative — don't shrink existing)
- [ ] **AC-7:** **Unit consistency (F-317):** Storage displayed with humanized units (B/KB/MB/GB/TB) — new helper `formatBytes(n)` ensures single convention. Raw bytes only in tooltip/export.
- [ ] **AC-8:** **Plan enum consistency (F-271):** `tenant.plan` canonical values: `starter | standard | enterprise`. List + detail render same. Migration to normalize existing records.
- [ ] **AC-9:** **User_count orphan fix (F-274):** ABC Edaş user_count 0 — migration seeds tenant admin user per tenant OR adjust `user_count` query to match reality. Verify via SQL + seed check.
- [ ] **AC-10:** **Refresh + sort:** Page polls every 30s or WS subscribes `tenant.usage_updated`. Sort preserved on refresh.
- [ ] **AC-11:** **Per-tenant click expansion:** Card click → SlidePanel with deeper stats: 7-day utilization trend sparkline per quota, top consuming resources, recent breach events.
- [ ] **AC-12:** **Breach history:** "Past 30 days breaches" section at page top shows tenants that breached — with severity, date, resolution.

## Files to Touch
- `web/src/pages/admin/tenant-usage.tsx` (NEW — replaces 2 pages)
- Delete `web/src/pages/admin/quotas.tsx`, `web/src/pages/admin/resources.tsx`
- `web/src/router.tsx` — redirect old routes
- `web/src/components/layout/sidebar.tsx` — single ADMIN item "Tenant Usage"
- `web/src/hooks/use-tenant-usage.ts` (NEW or extend)
- `web/src/lib/format.ts::formatBytes` (helper)
- Backend: verify unified endpoint `/admin/tenants/usage` or compose from existing
- `migrations/YYYYMMDDHHMMSS_tenant_quota_defaults.up.sql` — plan defaults migration
- `docs/architecture/ADMIN.md` — updated page reference

## Risks & Regression
- **Risk 1 — Existing tenant quotas shrink:** AC-6 conservative migration — only raise, never lower.
- **Risk 2 — Alert threshold false positives:** AC-5 configurable; ops can silence.
- **Risk 3 — User opt-in tenant lost on merge:** Data migration preserves all tenant rows; only UI reorganizes.

## Test Plan
- Unit: formatBytes consistent output
- Integration: alert fires when quota > 80% (seed breach scenario)
- Browser: tenant cards render, filter/sort work, SlidePanel expansion
- Regression: 2 old routes redirect to new

## Plan Reference
Priority: P2 · Effort: M · Wave: 10 · Depends: FIX-209 (alerts table — for breach alert), FIX-211 (severity taxonomy)
