# Gate Report: STORY-062 — Performance & Doc Drift Cleanup

## Summary
- Status: **PASS**
- Passes: 5/6 completed (Pass 6 UI functional/visual skipped per brief — backend-heavy story; Pass 6.4 token enforcement run on 2 FE files)
- Findings: 2 minor (both fixed — ROUTEMAP counter inconsistency, gate-step bump)
- Build: Go ✓ (exit 0) | Web tsc ✓ | Web build ✓ (4.28s)
- Tests: 2738 passing across 86 Go packages

---

## Pass 1: Requirements Tracing

| AC/Debt | Task | Status | Evidence |
|---------|------|--------|----------|
| AC-1 Dashboard cache 30s + NATS invalidation | T7 | ✓ PASS | `internal/api/dashboard/handler.go:354` TTL 30s; new `invalidator.go` subscribes sim.updated, session.started/ended, operator.health; `cmd/argus/main.go` wired. Note: `cdr.recorded` subject does not exist in `internal/bus/nats.go` so invalidator omits it (functionally complete). |
| AC-2 MSISDN batch INSERT | T8 | ✓ PASS | `internal/store/msisdn.go` chunks of 500, `INSERT ... VALUES ... ON CONFLICT DO NOTHING RETURNING msisdn`, per-chunk TX, duplicate diff captured. Test `msisdn_test.go` added. |
| AC-3 CDR export cursor + ALGORITHMS doc | T11 | ✓ PASS | `docs/architecture/ALGORITHMS.md` §5a "CDR Export Streaming" added with flow diagram; existing `internal/api/cdr/export.go` verified as 500-row cursor stream with flusher. |
| AC-4 Active sessions Redis counter | T9 | ✓ PASS | New `internal/aaa/session/counter.go` — INCR on session.started, DECR on session.ended, hourly reconciler via `ListActiveTenantCounts`; dashboard handler reads counter first via `SessionCounter` interface; `session_radius.go` adds `ListActiveTenantCounts` + `CountActiveByTenant`. |
| AC-5 Audit date-range bounds | T10 | ✓ PASS | `internal/api/audit/handler.go` validates both from/to present + ≤90d span (list + export); `internal/store/audit.go` adds `ErrDateRangeRequired`/`ErrDateRangeTooLarge` with store-level guard; `CodeInvalidDateRange` added in `apierr.go`; tests added in `handler_test.go`. |
| AC-6 ERROR_CODES.md drift | T2 | ✓ PASS | File ref already `internal/apierr/apierr.go` (line 4); all 9 AC-listed codes present: `MSISDN_NOT_FOUND`, `MSISDN_NOT_AVAILABLE`, `RESOURCE_LIMIT_EXCEEDED`, `TENANT_SUSPENDED`, `PROFILE_ALREADY_ENABLED`, `NOT_ESIM`, `INVALID_PROFILE_STATE`, `SAME_PROFILE`, `DIFFERENT_SIM`. 4 of them newly added in this story; others pre-existing from earlier stories. |
| AC-7 CONFIG.md drift | T3 | ✓ PASS | All 9 JOB/CRON vars present (lines 212–220); `ENCRYPTION_KEY` present (line 137); `RATE_LIMIT_ALGORITHM` present (line 200); Redis namespaces `sessions:active:count:`, `dashboard:` added; 4 NATS subjects added (`alert.triggered`, `job.completed`, `job.progress`, `audit.create`); `.env.example` DEPLOYMENT_MODE flipped to `single \| cluster` and JOB/CRON block appended. |
| AC-8 ARCHITECTURE/DSL/ALGORITHMS drift | T4 | ✓ PASS | Caching Strategy adds dashboard 30s + active sessions counter rows; tree includes `internal/api/cdr/`, `internal/api/ota/`, `internal/ota/`; removed stale `pkg/dsl/`; services/_index.md updated to `internal/policy/dsl/`; ALGORITHMS.md §5a added. |
| AC-9 GLOSSARY.md terms | T5 | ✓ PASS | All 38 terms from AC-9 list present (grep count = 39 incl. Pseudonymization Salt newly added this story). |
| AC-10 db/api _index + USERTEST | T16 | ✓ PASS | `db/_index.md` has TBL-25 (sim_segments), TBL-26 (ota_commands), TBL-27 (sla_reports), TBL-28 (anomalies). api/_index.md receives supplementary IDs via T17. USERTEST.md unchanged this story (prior corrections retained). |
| AC-11 ROUTEMAP reconciliation | T6, T18 | ✓ PASS | Phase 4 header already `[DONE]`; STORY-062 entry flipped to `[~] IN PROGRESS` → Gate step; counter normalized to 20/22 in both header and Dev Phase line (fixed during this gate); tech-debt D-003/D-010/D-011/D-012 flipped to ✓ RESOLVED (2026-04-13). |
| AC-12 11 undocumented endpoints | T17 | ✓ PASS (with 2 correctly omitted) | api/_index.md adds: API-099b diff, API-113b anomaly detail, API-130b notifications unread-count, API-140b audit alias, API-172/173/174 OTA section, API-262/263 policy-violations list/counts. Omitted: `GET /operator-grants/{id}` and `GET /policy-versions/{id}` — **these do not exist as GET handlers in `internal/gateway/router.go`** (only DELETE/POST and PATCH respectively), so correctly not documented. Footer refreshed to **Total: 201**. |
| D-003 Stale SCR IDs | T1 | ✓ RESOLVED | Genuinely stale IDs (SCR-045/075/071/072) removed from non-review story+plan files. SCR-060/070/080/100 flagged in plan are in fact valid current IDs in SCREENS.md — no action needed. STORY-058-plan.md rewritten with correct IDs. |
| D-010 Sessions/alerts CSV export | T13, T14 | ✓ RESOLVED | New `internal/api/session/export.go` + route `GET /api/v1/sessions/export.csv` under `RequireRole("sim_manager")`; `web/src/pages/alerts/index.tsx:650` `useExport('analytics/anomalies')` fix. |
| D-011 ImpersonateExit JWT | T15 | ✓ RESOLVED | `internal/api/admin/impersonate.go` — full rewrite: validates claims.Impersonated + ImpersonatedBy; `GetByIDGlobal(adminID)`; `auth.GenerateToken(...)`; audit entry `admin.impersonate_exit`; returns `impersonateResponse{jwt,user_id,email,tenant_id,role}`. |
| D-012 impersonatedBy claim access | T12 | ✓ RESOLVED | `web/src/hooks/use-impersonation.ts` changed `payload.act?.sub` → `payload.act_sub` with inline comment pointing to `internal/auth/jwt.go` serialization. |

---

## Pass 2: Compliance
- **API envelope**: `impersonate-exit` response uses `apierr.WriteSuccess` (standard envelope). Sessions CSV export writes raw CSV via `export.StreamCSV` (appropriate for `.csv` endpoint).
- **Naming**: snake_case fields, camelCase Go, kebab-case routes — clean.
- **Cursor pagination**: sessions export uses `ListActive(ctx, cursor, 500, filter)` cursor loop. Audit bounds enforced at both handler + store layers.
- **shadcn/ui + design tokens**: FE diff is 2 lines — not material. No hardcoded hex/default Tailwind colors introduced (Pass 6.4 grep clean).

## Pass 2.5: Security
- Sessions export endpoint wrapped in `JWTAuth + RequireRole("sim_manager")` (router.go:627–629).
- ImpersonateExit validates bearer token + `claims.Impersonated` before issuing fresh JWT; emits audit entry.
- No hardcoded secrets in `counter.go`, `invalidator.go`, `session/export.go`, `impersonate.go`.
- MSISDN batch INSERT uses parameterized `$1,$2,...` placeholders inside `fmt.Sprintf`-generated VALUES shell — values passed as `args...`, no SQL injection surface.
- No new tables → no RLS changes required.

## Pass 3: Tests
- `go build ./...` — exit 0
- `go test ./...` — **2738 passed across 86 packages**
- `web && npx tsc --noEmit` — clean
- `web && npm run build` — built in 4.28s, all chunks generated

## Pass 4: Performance
- Dashboard cache TTL bumped 15s → 30s at `handler.go:354`; invalidator registered on 4 NATS subjects (cdr.recorded subject absent in codebase — functional equivalent via TTL expiry).
- MSISDN bulk import: confirmed chunks of 500 with `ON CONFLICT DO NOTHING RETURNING msisdn`; per-chunk TX avoids long-held locks.
- Active sessions: Redis INCR/DECR + hourly `ListActiveTenantCounts` reconciler with `SET` (not INCRBY) to overwrite drift.
- Audit date-range bounds reject unbounded or >90d queries before DB hit.
- Sessions export cursor-streams via `ListActive(..., 500, filter)` — O(500) rows resident.

## Pass 5: Build Verification
- Go build OK, Go test suite (2738 tests) green.
- Frontend tsc + vite build green.
- No migration drift (no new tables).

## Pass 6: UI Quality (partial — functional skipped)
- Pass 6.4 token enforcement on modified FE files:
  - `web/src/hooks/use-impersonation.ts` — grep for hex/default-tailwind: 0 matches.
  - `web/src/pages/alerts/index.tsx` (1-line resource-path change only) — 0 new violations introduced.
- Pass 6.1–6.3 skipped per brief (backend-heavy, 2 FE files touched with 3 total changed lines).

---

## Fixes Applied
1. **ROUTEMAP header counter**: `Phase 10: 19/22 stories` → `20/22` (reconciled with Dev Phase line that already said 20/22).
2. **Current step bump**: `STORY-062 > Plan` → `Gate`.

## Escalations
None. All ACs and D-* items satisfied; one noteworthy non-blocking note below.

## Notes
- **AC-1 subject coverage**: Plan listed `cdr.recorded` as an invalidation trigger, but `internal/bus/nats.go` defines no such subject. Current invalidator covers `sim.updated`, `session.started`, `session.ended`, `operator.health`. Any CDR-driven dashboard staleness is bounded by the 30s TTL — acceptable per Bug Patterns note ("missing events during downtime are harmless because TTL will expire the key within 30s anyway"). No action required.
- **AC-12 endpoint count**: api/_index.md footer went **204 → 201** despite *adding* endpoints because the recount corrected over-count from prior stories. This is an accurate recount, not a regression.
- **AC-12 omissions (2/11)**: `GET /api/v1/operator-grants/{id}` and `GET /api/v1/policy-versions/{id}` do not exist in router.go — correctly NOT documented. The plan's cross-link to compliance-audit-report.md would need re-validation if those endpoints are later added.
