# Argus Deep Scope Audit

Date: 2026-04-17
Scope: `docs/PRODUCT.md` + `docs/SCOPE.md` + story contracts + current codebase
Method: source-backed doc-to-code audit followed by implementation closure on every material gap found in this pass

## Verdict

The material gaps identified in the original 2026-04-17 audit pass are now closed in code.

Fixed areas in this pass:

- `STORY-044`: SIM detail usage tab now includes a real, paginated CDR table backed by `GET /api/v1/cdrs?sim_id=...`
- `F-057` / product-scope OAuth2: client-credentials token issuance and runtime scope-aware auth are now implemented
- `STORY-069`: scheduled reporting no longer uses the empty stub provider and now reads from real stores
- `STORY-073`: admin tenant resource metrics now return real sparkline, active session, storage, API RPS, and 30-day CDR volume data
- `STORY-077`: undo is now operable end-to-end for policy delete and reversible SIM state changes

## Closure Summary

### 1. OAuth2 client credentials is now implemented as a real feature

- Added `POST /api/v1/oauth/token` for `client_credentials`
- Reused API keys as OAuth client credentials with scope subset enforcement
- Issued scoped bearer JWTs carrying `auth_type=oauth2`, scopes, and `api_key_id`
- Enforced scope checks for both `api_key` and `oauth2` auth paths

Evidence:

- `docs/PRODUCT.md:106`
- `docs/PRODUCT.md:280`
- `docs/SCOPE.md:122`
- `internal/api/auth/handler.go`
- `internal/auth/jwt.go`
- `internal/gateway/auth_middleware.go`
- `internal/gateway/rbac.go`
- `internal/gateway/router.go`
- `cmd/argus/main.go`

### 2. Undo now works end-to-end for shipped actions

- Boot wiring now registers undo executors at runtime
- Policy delete returns `meta.undo_action_id` and restores archived policy state through `policy_restore`
- SIM suspend, resume, and report-lost now return `meta.undo_action_id`
- Undo execution restores SIM state through `sim_state_restore`
- Frontend now registers undo toasts for both policy delete and SIM state changes

Evidence:

- `internal/api/undo/handler.go`
- `internal/api/policy/handler.go`
- `internal/store/policy.go`
- `internal/api/sim/handler.go`
- `internal/store/sim.go`
- `cmd/argus/main.go`
- `web/src/hooks/use-policies.ts`
- `web/src/pages/policies/index.tsx`
- `web/src/hooks/use-sims.ts`
- `web/src/pages/sims/detail.tsx`

### 3. Scheduled reporting now uses real data providers

- Replaced `emptyReportProvider` boot wiring with `report.NewStoreProvider(...)`
- Report generation now reads from compliance, SLA, CDR, audit, and SIM inventory stores instead of returning empty datasets

Evidence:

- `internal/report/store_provider.go`
- `cmd/argus/main.go`

### 4. Tenant resource metrics are now materially real

- Sparkline now uses `cdrStore.GetDailyKPISparklines(...)`
- `TenantStore.GetStats(...)` now populates `ActiveSessions` and estimated `StorageBytes`
- Admin tenant resource endpoint now emits `api_rps`, `cdr_bytes_30d`, `storage_bytes`, and real spark data
- Tenant quota storage percentage now uses actual storage estimation instead of zero

Evidence:

- `docs/stories/phase-10/STORY-073-admin-compliance-screens.md:29`
- `internal/store/tenant.go`
- `internal/api/admin/tenant_resources.go`
- `cmd/argus/main.go`

### 5. SIM usage tab now satisfies the missing CDR table contract

- Added `useSIMCDRs`
- Added paginated `CDR History` table to the SIM usage tab

Evidence:

- `docs/stories/phase-8/STORY-044-frontend-sim.md:18`
- `docs/stories/phase-8/STORY-044-frontend-sim.md:34`
- `web/src/hooks/use-sims.ts`
- `web/src/pages/sims/detail.tsx`

## Verification

Passed:

- `GOCACHE=/tmp/argus-gocache go test ./internal/api/auth ./internal/api/policy ./internal/api/admin ./internal/report ./internal/store ./cmd/argus`
- `npx tsc --noEmit`

Known environment limitation:

- `go test ./internal/gateway` cannot run in this sandbox because its tests need local bind/listen privileges (`miniredis` and `httptest` fail with `bind operation not permitted`)
- `npm run build` still depends on a missing optional local package in `web/node_modules` (`@rollup/rollup-darwin-arm64`), so full frontend production build was not revalidated in this environment

## Residual Risk

- The implemented closures are covered by compile-target and package-test validation, but gateway-level HTTP integration tests remain unexecuted in this sandbox
- Production frontend build health is still partially blocked by the local optional Rollup dependency issue rather than application code
