# Implementation Plan: FIX-246 — Quotas + Resources Merge → Unified "Tenant Usage" Dashboard

## Goal
Merge `/admin/quotas` (utilization) + `/admin/resources` (current usage) into a single `/admin/tenant-usage` dashboard with per-tenant quota cards, M2M-realistic defaults, plan enum normalization, breach alerts via FIX-209, and SlidePanel deep-stats — closing F-271..F-274 + F-314..F-317.

## Story Context

- Story: `docs/stories/fix-ui-review/FIX-246-quotas-resources-merge.md`
- Effort: M  · Priority: P2  · Wave: 10
- Findings: F-271 (plan enum mismatch), F-272 (no util %), F-273 (no state column), F-274 (user_count 0 orphan), F-314 (merge), F-315 (alert trigger gap), F-316 (M2M-unrealistic defaults), F-317 (unit display bug)
- Depends on: FIX-209 (alerts table — DONE), FIX-211 (severity taxonomy — DONE), FIX-216 (SlidePanel pattern), FIX-240 (`useHashTab`, `lib/rbac.ts`)

## Architecture Touch

### Frontend (web/src/)
- **NEW page:** `pages/admin/tenant-usage.tsx` — replaces 2 existing pages
- **DELETE:** `pages/admin/quotas.tsx`, `pages/admin/tenant-resources.tsx` (note: file is `tenant-resources.tsx`, not `resources.tsx` — spec was wrong)
- **MODIFY:** `router.tsx` — `/admin/quotas` + `/admin/resources` → `<Navigate to="/admin/tenant-usage" replace />`
- **MODIFY:** `components/layout/sidebar.tsx` — collapse 2 sidebar items into single "Tenant Usage"
- **NEW hook:** `hooks/use-tenant-usage.ts` — single React Query feeding the unified dashboard (30s polling)
- **MODIFY:** `types/admin.ts` — extend `TenantUsageItem` (new shape uniting QuotaMetric + current usage + plan + state)
- **NEW component:** `components/admin/tenant-usage-card.tsx` (per-tenant card)
- **NEW component:** `components/admin/tenant-usage-detail-panel.tsx` (SlidePanel deep stats — AC-11)
- **REUSE:** `lib/format.ts::formatBytes` (already exists, AC-7) + `components/ui/slide-panel.tsx`

### Backend (internal/)
- **NEW endpoint:** `GET /api/v1/admin/tenants/usage` — unified payload (decision: backend-compose, see Decision D-1 below)
- **NEW handler file:** `internal/api/admin/tenant_usage.go` — combines quota + resource data + plan + state + breach history snippet (replaces `tenant_resources.go` long-term but kept additively this round)
- **MODIFY:** `internal/store/tenant.go` — add `Plan string` to `Tenant` struct + scan it; update `ListWithCounts`/`Get`/`Update` SELECTs to include the new column
- **NEW periodic job:** `internal/job/quota_breach_checker.go` — hourly cron, fires `quota.breach` alerts via `alertStore.UpsertWithDedup` at 80%/95% thresholds (mirror `coa_failure_alerter.go` pattern)
- **MODIFY:** `internal/gateway/router.go` — register new route + retire old routes after FE switch (legacy routes stay for one release for compat — see Risk 4)

### Database
- **NEW migration up/down:** `migrations/20260502000001_tenant_plan_and_quota_defaults.{up,down}.sql`
  - Add `tenants.plan VARCHAR(20) NOT NULL DEFAULT 'standard'` with CHECK enum `('starter','standard','enterprise')`
  - Add `tenants.max_sessions INTEGER NOT NULL DEFAULT 2000` (was implicit `MaxUsers`)
  - Add `tenants.max_api_rps INTEGER NOT NULL DEFAULT 2000`
  - Add `tenants.max_storage_bytes BIGINT NOT NULL DEFAULT 107374182400` (100 GB)
  - Conservative `UPDATE` using `GREATEST(current, plan_default)` — never lowers (AC-6)
  - Backfill `plan` based on `max_sims` heuristic: `<=20K → starter`, `<=200K → standard`, `>200K → enterprise` (AC-8)
- **NEW seed migration:** `migrations/seed/009_tenant_admin_orphan_fix.sql` — for any tenant missing a `tenant_admin` user, insert one (AC-9 — root-cause fix in seed, not query patch — see Decision D-3)

## Decisions

### D-1: Backend strategy = unified endpoint, NOT FE-compose
- Existing shapes diverge (quota = current+max+pct+status objects; resource = flat numbers) — composing two requests on FE means double round-trips and duplicated tenant list.
- New `GET /admin/tenants/usage` returns one consolidated array. Old `/quotas` and `/resources` endpoints stay registered for one release as legacy adapters (FE never calls them after this story; D-168 tracks deletion).

### D-2: Polling, not WebSocket
- No `tenant.usage_updated` event publisher exists. Adding one requires injection into 5+ write paths (sim create/update, session start/stop, storage tick). Spec instruction #6: "prefer polling unless WS is trivially achievable."
- Use 30s React Query `refetchInterval` (existing pattern in `useTenantResources`). Sort/filter state held in `useState` survives refetch (AC-10).

### D-3: user_count fix = seed root cause, not query patch
- Audit: `ListWithCounts` query already filters `state != 'terminated'` correctly (line 325 of `internal/store/tenant.go`). The query is fine.
- ABC Edaş `user_count = 0` because seed `005_multi_operator_seed.sql` does NOT create a `tenant_admin` user for that tenant — only the platform-level admin exists in `tenants[0]`.
- Fix: new seed migration `009_tenant_admin_orphan_fix.sql` inserts a `tenant_admin` user for every tenant that has zero non-terminated users. Idempotent (`ON CONFLICT DO NOTHING`).

### D-4: Plan enum source of truth = NEW `tenants.plan` column
- Audit: NO `plan` column exists today. FE reads `tenant.plan ?? 'standard'` (purely a fallback in `pages/system/tenants.tsx`). Backend never returns `plan`.
- Existing canonical values: none in DB. Migration introduces the column with CHECK `('starter','standard','enterprise')`. No "trial" value found anywhere — no 4th plan needed.
- Update `Tenant` struct + Update handler param + tenant edit form to write the new column.

### D-5: Breach alert = NEW alert `type='quota.breach'` on existing `alerts` table (NO new table)
- `chk_alerts_source` enum already includes `'system'` — quota breach uses `source='system'`.
- `dedup_key` format: `quota:{tenant_id}:{metric}:{threshold}` (e.g. `quota:abc-123:sims:80`).
- Severity mapping: 80% → `medium` (warning), 95% → `critical`. State `open`/`resolved` follow existing FIX-210 state machine.
- Periodic job `quota_breach_checker.go` runs hourly; mirrors `coa_failure_alerter.go` pattern (existing reference).

### D-6: Pulsing ring = subtle Tailwind animation, no new design tokens
- Use existing `animate-pulse` utility on a `ring-2 ring-warning/40` (>=80%) or `ring-danger/50` (>=95%) outer ring on the card. No new keyframes, no new tokens.

### D-7: 7-day trend in SlidePanel = reuse existing CDR sparklines
- `cdrStore.GetDailyKPISparklines(ctx, tenantID, 7)` already returns `total_sims` series (used in current resources endpoint). Extend to also return `active_sessions`, `cdr_bytes`. No new aggregation table.

## Wire Contract

### `GET /api/v1/admin/tenants/usage` (super_admin)
Response: `{ status: "success", data: TenantUsageItem[] }`
```ts
interface QuotaMetric { current: number; max: number; pct: number; status: 'ok'|'warning'|'critical' }
interface TenantUsageItem {
  tenant_id: string
  tenant_name: string
  plan: 'starter' | 'standard' | 'enterprise'
  state: 'active' | 'suspended' | 'trial'
  sims:          QuotaMetric          // current = sim_count, max = max_sims
  sessions:      QuotaMetric          // current = active_sessions, max = max_sessions
  api_rps:       QuotaMetric          // current = api_rps (5-min rolling), max = max_api_rps
  storage_bytes: QuotaMetric          // current = storage_bytes, max = max_storage_bytes
  user_count:    number               // for footer summary
  cdr_bytes_30d: number               // for footer summary
  open_breach_count: number           // # of open quota.breach alerts (drives pulsing ring)
}
```

NOTE: status enum changed `'danger' → 'critical'` to align with FIX-211 severity taxonomy.

### `GET /api/v1/admin/tenants/{id}/usage/trend?days=7` (super_admin) — for SlidePanel
```ts
interface UsageTrendPoint { date: string; sims: number; sessions: number; cdr_bytes: number }
// Returns 7 points (one per day)
```

### `GET /api/v1/alerts?type=quota.breach&fired_at>=NOW-30d` — existing endpoint, no new code
Used directly by FE for breach history section (AC-12) — query param reuse.

## Migration

### `20260502000001_tenant_plan_and_quota_defaults.up.sql`
```sql
-- Source: NEW (no migration exists for these columns)
ALTER TABLE tenants
  ADD COLUMN plan VARCHAR(20) NOT NULL DEFAULT 'standard'
    CHECK (plan IN ('starter','standard','enterprise')),
  ADD COLUMN max_sessions     INTEGER NOT NULL DEFAULT 2000,
  ADD COLUMN max_api_rps      INTEGER NOT NULL DEFAULT 2000,
  ADD COLUMN max_storage_bytes BIGINT NOT NULL DEFAULT 107374182400; -- 100 GB

-- Plan backfill (AC-8) — heuristic on existing max_sims
UPDATE tenants SET plan = CASE
  WHEN max_sims <=  20000 THEN 'starter'
  WHEN max_sims <= 200000 THEN 'standard'
  ELSE 'enterprise'
END;

-- Conservative quota raise (AC-6) — GREATEST never lowers existing values
UPDATE tenants SET
  max_sims          = GREATEST(max_sims,           CASE plan WHEN 'starter' THEN 10000   WHEN 'standard' THEN 100000  ELSE 1000000  END),
  max_sessions      = GREATEST(max_sessions,       CASE plan WHEN 'starter' THEN 2000    WHEN 'standard' THEN 20000   ELSE 200000   END),
  max_api_rps       = GREATEST(max_api_rps,        CASE plan WHEN 'starter' THEN 2000    WHEN 'standard' THEN 5000    ELSE 20000    END),
  max_storage_bytes = GREATEST(max_storage_bytes,  CASE plan WHEN 'starter' THEN 107374182400  WHEN 'standard' THEN 1099511627776  ELSE 10995116277760 END);

CREATE INDEX idx_tenants_plan ON tenants (plan);
```

### `seed/009_tenant_admin_orphan_fix.sql` (AC-9)
```sql
-- For every tenant with zero active users, insert a tenant_admin.
-- Idempotent: ON CONFLICT DO NOTHING on the (tenant_id, email) unique pair.
INSERT INTO users (tenant_id, email, password_hash, name, role, state, password_change_required)
SELECT t.id,
       'admin+' || lower(regexp_replace(t.name, '[^a-zA-Z0-9]', '', 'g')) || '@argus.local',
       '$2a$10$placeholderHashForceResetOnFirstLogin',
       t.name || ' Admin',
       'tenant_admin',
       'active',
       true
FROM tenants t
WHERE NOT EXISTS (
  SELECT 1 FROM users u
  WHERE u.tenant_id = t.id AND u.state != 'terminated'
)
ON CONFLICT (email) DO NOTHING;
```

## Screen / Card Mockup

```
┌─ Past 30 days breaches ─────────────────────────────────────────┐
│ Apr 25 · ABC Edaş · sims 95% (critical)  · acknowledged         │
│ Apr 22 · Bosphorus · sessions 82% (warning) · resolved          │
└─────────────────────────────────────────────────────────────────┘

[Search ◀tenant▶]  [Plan: All ▾]  [State: All ▾]  [Threshold: All ▾]   [Cards|Table]  [↻]

┌─ ABC Edaş ─[STARTER]──────────────────── [OK] ─┐  ┌─ Bosphorus IoT (STANDARD) ──── [WARN]🔴 ─┐
│ SIMs        108 / 100,000   ▓░░░░░░  0.1%      │  │ Sessions   1,640 / 2,000  ▓▓▓▓░  82% ⚠   │
│ Sessions      0 / 2,000     ░░░░░░░  0.0%      │  │ ...                                       │
│ API RPS       0 / 2,000     ░░░░░░░  0.0%      │  └──────────────────────────────────────────┘
│ Storage  27.3 KB / 100 GB   ░░░░░░░  0.0%      │
│ State: ACTIVE          [Edit limits →]         │
└────────────────────────────────────────────────┘
   ↑ click → SlidePanel with 7-day trend per quota + recent breach events
```

## Design Token Map (from FRONTEND.md)

| Usage | Token | Never Use |
|-------|-------|-----------|
| Card bg | `bg-bg-surface` | `bg-white` |
| Card border | `border-border` | `border-gray-200` |
| Plan chip (starter) | `Badge variant="default"` | hex |
| Plan chip (standard) | `Badge variant="success"` | hex |
| Plan chip (enterprise) | `Badge variant="warning"` | hex |
| State chip ACTIVE | `Badge variant="success"` | — |
| State chip SUSPENDED | `Badge variant="danger"` | — |
| State chip TRIAL | `Badge variant="warning"` | — |
| Bar (ok) | `bg-accent-primary` | `bg-blue-500` |
| Bar (warning ≥50) | `bg-warning` | `bg-yellow-500` |
| Bar (danger ≥80) | `bg-danger` | `bg-red-500` |
| Pulsing ring (≥80) | `ring-2 ring-warning/40 animate-pulse` | new keyframe |
| Pulsing ring (≥95) | `ring-2 ring-danger/50 animate-pulse` | new keyframe |
| Page title | `text-xl font-semibold text-text-primary` | `text-[24px]` |
| Subtitle | `text-sm text-text-secondary` | `text-gray-500` |
| Bar track | `h-2 rounded-full bg-bg-muted` | hex |

### Components to REUSE
| Component | Path | Use |
|-----------|------|-----|
| `<Card>` family | `web/src/components/ui/card.tsx` | tenant cards |
| `<Badge>` | `web/src/components/ui/badge.tsx` | plan + state chips |
| `<Button>` | `web/src/components/ui/button.tsx` | view toggle, refresh |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | loading state |
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | deep stats (AC-11) |
| `<EntityLink>` | `web/src/components/shared/entity-link.tsx` | tenant link |
| `<Table>` family | `web/src/components/ui/table.tsx` | table view (AC-3) |
| `formatBytes()` | `web/src/lib/format.ts` | storage display (AC-7) |
| `formatNumber()` | `web/src/lib/format.ts` | counts |
| `hasMinRole()` | `web/src/lib/rbac.ts` | super_admin guard |
| Pattern: existing `pages/admin/quotas.tsx` `QuotaBar` | inline today | extract to shared `<QuotaBar>` molecule |

## File-by-File Touch List

| File | Action | Notes |
|------|--------|-------|
| `migrations/20260502000001_tenant_plan_and_quota_defaults.up.sql` | NEW | plan col + 3 quota cols + GREATEST backfill |
| `migrations/20260502000001_tenant_plan_and_quota_defaults.down.sql` | NEW | drop columns + index |
| `migrations/seed/009_tenant_admin_orphan_fix.sql` | NEW | idempotent admin user backfill |
| `internal/store/tenant.go` | MODIFY | add Plan/MaxSessions/MaxAPIRPS/MaxStorageBytes fields + scans + Update SET clauses |
| `internal/api/admin/tenant_usage.go` | NEW | `ListTenantUsage` handler + `GetTenantUsageTrend` handler |
| `internal/api/admin/tenant_resources.go` | MODIFY (light) | keep legacy endpoints for 1 release; route registration unchanged |
| `internal/job/quota_breach_checker.go` | NEW | hourly job; `UpsertWithDedup` pattern from `coa_failure_alerter.go` |
| `internal/job/quota_breach_checker_test.go` | NEW | 4 cases: <80 noop, 80-94 warning, 95+ critical, dedup hit |
| `internal/gateway/router.go` | MODIFY | register `/admin/tenants/usage` + `/admin/tenants/{id}/usage/trend` |
| `cmd/argus/main.go` | MODIFY | wire QuotaBreachChecker in scheduler list |
| `web/src/types/admin.ts` | MODIFY | add `TenantUsageItem`, `UsageTrendPoint`; rename status `'danger' → 'critical'` |
| `web/src/hooks/use-tenant-usage.ts` | NEW | `useTenantUsage()` (30s poll) + `useTenantUsageTrend(id)` |
| `web/src/hooks/use-admin.ts` | MODIFY | mark `useTenantResources` + `useTenantQuotas` `@deprecated` |
| `web/src/pages/admin/tenant-usage.tsx` | NEW | top-level page |
| `web/src/components/admin/tenant-usage-card.tsx` | NEW | per-tenant card with bars + chips |
| `web/src/components/admin/tenant-usage-detail-panel.tsx` | NEW | SlidePanel deep stats |
| `web/src/components/admin/quota-bar.tsx` | NEW | shared bar molecule (extract from old quotas.tsx) |
| `web/src/pages/admin/quotas.tsx` | DELETE | |
| `web/src/pages/admin/tenant-resources.tsx` | DELETE | |
| `web/src/router.tsx` | MODIFY | add `/admin/tenant-usage` lazy import + 2x `<Navigate replace>` |
| `web/src/components/layout/sidebar.tsx` | MODIFY | replace 2 admin items with 1 "Tenant Usage" |
| `web/src/components/admin/__tests__/tenant-usage-card.test.tsx` | NEW | unit: pct rendering, color tier, pulsing-ring threshold |
| `web/src/__tests__/format.test.ts` | MODIFY | add formatBytes assertions for AC-7 (already partially covered) |
| `docs/architecture/services/_index.md` | MODIFY (light) | mention new admin endpoint |

## Tasks (parallelizable in 5 waves)

### Wave 1 — Foundation (parallel-safe; pure DB + type contracts)
- **Task 1** [low] · Migration up/down + seed 009 · Files: 3 migration files · Pattern ref: `migrations/20260423000001_alerts_dedup_statemachine.up.sql` · Verify: `make db-migrate && make db-seed` clean
- **Task 2** [low] · `web/src/types/admin.ts` extension · Pattern ref: existing `TenantQuota` interface · Verify: `tsc --noEmit` clean

### Wave 2 — Backend (depends on Wave 1; parallel-safe between 3 + 4 + 5)
- **Task 3** [medium] · `internal/store/tenant.go` Plan/MaxSessions/MaxAPIRPS/MaxStorageBytes fields + scans · Verify: `go build ./...` + tenant CRUD tests pass
- **Task 4** [medium] · `internal/api/admin/tenant_usage.go` handler + router wiring · Pattern ref: `internal/api/admin/tenant_resources.go::ListTenantQuotas` · Verify: `curl /admin/tenants/usage` returns 200 with shape
- **Task 5** [high] · `internal/job/quota_breach_checker.go` hourly cron + dedup + main.go wire · Pattern ref: `internal/job/coa_failure_alerter.go` (closest analog — periodic job, alertStore.UpsertWithDedup, dedup_key composition) · Verify: unit test fires 80/95 alerts, dedup blocks 2nd run

### Wave 3 — Frontend page + hook (depends on Wave 2 endpoint)
- **Task 6** [medium] · `web/src/hooks/use-tenant-usage.ts` + `quota-bar.tsx` shared molecule · Pattern ref: existing `useTenantResources` + old `QuotaBar` in quotas.tsx · Verify: hook returns typed data; bar renders 4 color tiers
- **Task 7** [medium] · `web/src/components/admin/tenant-usage-card.tsx` + `tenant-usage-detail-panel.tsx` · Pattern ref: `pages/admin/tenant-resources.tsx` card layout + `components/policy/rollout-expanded-slide-panel.tsx` SlidePanel structure · Tokens: ZERO hex/px — only Design Token Map · Verify: Storybook smoke OR manual

### Wave 4 — Page assembly + routing (depends on Waves 2, 3)
- **Task 8** [medium] · `web/src/pages/admin/tenant-usage.tsx` (toolbar/search/sort/filter/cards|table/breach-history) · Pattern ref: `pages/admin/tenant-resources.tsx` (view toggle), FIX-240's settings page (toolbar pattern) · Tokens: only Design Token Map · Verify: `grep -r '#[0-9a-fA-F]\{3,\}' web/src/pages/admin/tenant-usage.tsx web/src/components/admin/` returns ZERO matches
- **Task 9** [low] · Router redirects + sidebar collapse + DELETE 2 old pages · Verify: visit `/admin/quotas` → 301-style redirect lands on `/admin/tenant-usage`; sidebar shows single item; `grep -r "admin/quotas\|admin/resources\|tenant-resources\|pages/admin/quotas" web/src` returns only the new redirect lines

### Wave 5 — Tests + docs (depends on Waves 2, 3, 4)
- **Task 10** [medium] · Unit tests · `web/src/components/admin/__tests__/tenant-usage-card.test.tsx` (4-tier color, pulsing ring at 80/95) + extend `web/src/__tests__/format.test.ts` (AC-7) + `internal/job/quota_breach_checker_test.go` (already in Task 5 — verify here) · Verify: `npm test`, `go test ./internal/job/...`
- **Task 11** [low] · `docs/architecture/services/_index.md` mention + manual smoke (cards render; click → SlidePanel; trigger 80% breach in seed; verify alert appears in `/alerts` page filtered by `type=quota.breach`)

## Acceptance Criteria Mapping

| AC | Requirement | Implemented in | Verified by |
|----|-------------|----------------|-------------|
| AC-1 | New `/admin/tenant-usage` + 2 redirects | Task 9 (router), Task 8 (page) | manual smoke |
| AC-2 | Card design w/ 4 quotas + plan + state + edit link | Tasks 6, 7 | Task 10 unit |
| AC-3 | Cards/Table view toggle | Task 8 | manual smoke |
| AC-4 | Toolbar search/sort/filter | Task 8 | manual smoke |
| AC-5 | Alert integration 80%/95% + pulsing ring | Task 5 (BE alert), Task 7 (FE ring) | Task 5 BE test, Task 10 FE test |
| AC-6 | M2M defaults via GREATEST | Task 1 (migration) | `make db-migrate` + SQL diff smoke (no row's max_* shrank) |
| AC-7 | `formatBytes()` for unit consistency | Task 6 (reuse existing) | Task 10 format test |
| AC-8 | Plan enum normalization | Task 1 (migration backfill), Task 3 (struct field) | manual SQL `SELECT plan, COUNT(*) FROM tenants GROUP BY plan` |
| AC-9 | user_count orphan fix | Task 1 (seed 009) | `SELECT t.name, COUNT(u.id) FROM tenants t LEFT JOIN users u ON u.tenant_id=t.id GROUP BY t.name` — every tenant ≥1 user |
| AC-10 | 30s poll, sort preserved | Task 6 (hook `refetchInterval`), Task 8 (sort state in `useState`) | manual: change sort → wait 30s → sort still applied |
| AC-11 | Card click → SlidePanel deep stats | Task 7 | manual smoke |
| AC-12 | Breach history (past 30 days) | Task 8 (uses existing `/alerts?type=quota.breach`) | manual: trigger breach in seed → see history row |

## Bug Pattern Warnings (from `docs/brainstorming/bug-patterns.md`)

- **PAT-006 (RECURRENCE #3 from FIX-251):** React Query cache invalidation drift. The new `useTenantUsage` mutates nothing, but if a future story adds a "Quick edit limits" inline mutation, MUST invalidate ALL related keys via a single `invalidateTenantUsage(qc)` helper (mirror FIX-244's `invalidateViolations`). Routed for prevention.
- **PAT-018 (token discipline):** ZERO hex / `text-gray-*` / `bg-blue-*` permitted in Tasks 6/7/8. Verify command: `grep -rE "#[0-9a-fA-F]{3,8}|text-(gray|blue|red|green|yellow)-|bg-(gray|blue|red|green|yellow)-" web/src/pages/admin/tenant-usage.tsx web/src/components/admin/` returns ZERO matches.
- **PAT-023 (zero-code schema drift):** When adding `tenants.plan` column, ensure ALL `SELECT id, name, ...` and `INSERT INTO tenants` and `RETURNING ...` lists in `internal/store/tenant.go` are updated atomically (10+ sites). Search command before commit: `grep -n "FROM tenants\|INTO tenants\|RETURNING " internal/store/tenant.go`.
- **PAT-001 (counter double-write):** N/A — no metrics added by this story. Quota breach is alert-emitting only; no Prometheus counter writes.

## Tech Debt

- **D-168 (NEW):** Legacy `/admin/tenants/quotas` and `/admin/tenants/resources` endpoints + their handlers stay registered for one release as compat. Delete after FIX-246 ships and FE confirmed switched. Owner: next maintenance wave.
- **D-169 (NEW):** Spec said file was `resources.tsx`; actual file is `tenant-resources.tsx`. Updated in plan but spec doc has stale name — flag for spec author to correct in story doc cleanup pass.
- **D-170 (NEW):** Quota alert thresholds hardcoded 80/95 in `quota_breach_checker.go`. AC-5 mentions "configurable per-plan future". Leave a TODO + a `quotaThresholds()` function returning a hardcoded map keyed by plan; future story extracts to DB table.

## Story-Specific Compliance Rules

- **API:** `/admin/tenants/usage` MUST use standard envelope `{ status, data, meta? }` (`apierr.WriteSuccess` helper). Super_admin role guard via existing chi middleware.
- **DB:** Both up + down migration required. Down drops the index + columns. Tested via `make db-migrate && make db-down && make db-migrate`.
- **UI:** All bars + chips MUST use Design Token Map. `frontend-design` skill SHOULD be invoked for Task 7 (cards + SlidePanel are the visual spine of this page).
- **RLS:** New endpoint reads from `tenants` table — no RLS policy needed (super_admin only).
- **ADR-002 / FIX-211:** Severity values `medium`, `critical` — NEVER `warning`/`error`/`danger` at the alert layer. (FE chip color naming `danger` is presentational only.)

## Risks & Mitigations

| # | Risk | Likelihood | Mitigation |
|---|------|------------|-----------|
| 1 | Migration `GREATEST` accidentally lowers a custom-tuned tenant quota | Low | `GREATEST` semantically cannot lower; verified by Task 1 verify-step running `SELECT id, max_sims AS new, max_sims_old FROM tenants_backup` (snapshot pre-migration in down file) |
| 2 | Alert false positives (storage_bytes calculation is `pg_column_size` which underestimates) | Medium | Storage threshold uses 100 GB default — well above current ~27 KB; even 10x error doesn't breach. Acknowledged in Task 5; D-170 routes per-plan tunable. |
| 3 | User opt-in tenant lost on merge | Very Low | No data migration touches tenant rows beyond ALTER TABLE ADD; no DELETE/MERGE. Down file restores original column shape. |
| 4 | Legacy quotas/resources endpoints break if FE deployment lags BE | Low | D-168: keep them registered for 1 release. New FE switches via `useTenantUsage`; old hooks marked `@deprecated` but still functional. |
| 5 | Backfill heuristic mis-classifies plan | Low | Heuristic uses existing `max_sims`. Admin can edit plan in `/system/tenants/{id}` after migration. Document in CHANGELOG. |
| 6 | Quota cron job storms NATS during 80%+ multi-tenant breach | Low | `UpsertWithDedup` honors cooldown (FIX-210 state machine). Hourly cadence — bounded. |
| 7 | Seed `009_tenant_admin_orphan_fix.sql` collides with future tenant create flow | Very Low | Idempotent (`ON CONFLICT (email) DO NOTHING`); only fires when 0 active users exist. |
| 8 | FE `'danger'` → `'critical'` rename breaks legacy callers of `useTenantQuotas` | Low | Old hook left in place but unused after Task 9. Type rename only in NEW types — old `QuotaMetric.status='danger'` shape unchanged for back-compat. |

## Self-Validation Checklist (Quality Gate)

- [x] All 12 ACs mapped to concrete files + tasks (table above)
- [x] Migration audit performed: NO `plan` column exists today (D-4); NO `max_sessions`/`max_api_rps`/`max_storage_bytes` columns exist (added in migration); user_count orphan root-caused to seed (D-3)
- [x] FIX-209 alert reuse documented — `quota.breach` is a new alert TYPE (not table); `source='system'` (existing enum value)
- [x] Backend strategy decided — unified endpoint (D-1) with rationale
- [x] WS vs poll decided — poll (D-2) with rationale
- [x] `formatBytes` location confirmed: `web/src/lib/format.ts` (already exists, KB/MB/GB/TB w/ 1024 base)
- [x] Wave breakdown enables parallel dispatch (Wave 2 has 3 parallel tasks, Wave 5 covers tests in one task)
- [x] Risks 1/2/3 from spec addressed (mitigations 1, 2, 3); 5 new risks (4-8) added from audit
- [x] No scope creep — no new alert table; no new aggregation table; no new design tokens; no WS plumbing
- [x] Token discipline rule embedded in Tasks 7, 8 with verify command
- [x] Pattern refs assigned to every NEW file task
- [x] Self-containment: API contract, DB DDL, mockup, token map all embedded inline

**Quality Gate: PASS**
