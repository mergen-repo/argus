# Implementation Plan: STORY-078 — SIM Compare & System Config Endpoint Backfill

## Goal

Backfill two documented-but-missing backend endpoints — `POST /api/v1/sims/compare` (API-053) and `GET /api/v1/system/config` (API-182) — so the existing frontend SIM compare page (currently broken with 404) renders real diff data, and so super_admin operators have a documented, redacted runtime config introspection endpoint. This is the final story before the Phase 10 gate.

## Architecture Context

### Components Involved

- `internal/api/sim/compare.go` (NEW) — SIM compare HTTP handler, attached as method on existing `*sim.Handler`. Reuses the handler's existing stores (`simStore`, `apnStore`, `operatorStore`, `policyStore`, `sessionStore`, `nameCache`) and `auditSvc`.
- `internal/api/sim/compare_test.go` (NEW) — table-driven unit tests.
- `internal/api/system/config_handler.go` (NEW) — System config HTTP handler in the existing `internal/api/system` package (alongside `status_handler.go`, `capacity_handler.go`, etc.).
- `internal/api/system/config_handler_test.go` (NEW) — redaction + RBAC tests.
- `internal/config/config.go` (MODIFY) — add a `Redact() RedactedConfig` method (and the `RedactedConfig` struct) that produces a JSON-safe view with all secrets either omitted or masked.
- `internal/config/config_test.go` (NEW or MODIFY) — assert every known-secret env field is omitted/masked from the redacted payload.
- `internal/gateway/router.go` (MODIFY) — wire `POST /api/v1/sims/compare` (sim_manager+) and `GET /api/v1/system/config` (super_admin) routes; extend `RouterDeps` with `SystemConfigHandler *systemapi.ConfigHandler`.
- `cmd/argus/main.go` (MODIFY) — instantiate `systemapi.NewConfigHandler(cfg, version, gitSHA, buildTime)` and pass via `RouterDeps`.
- `web/src/hooks/use-sims.ts` (MODIFY) — add `useCompareSIMs(idA, idB)` hook calling new endpoint.
- `web/src/pages/sims/compare.tsx` (MODIFY) — replace the existing per-SIM `useQueries` fan-out with the single `POST /sims/compare` call once 2 SIMs are selected; keep current 1-SIM and 3-SIM fallback paths reusing per-detail fetch (compare endpoint is strictly pairwise per AC-1).
- `docs/architecture/api/_index.md` (VERIFY) — paths already present (`API-053`, `API-182`) — confirm exact path strings match implementation.
- `docs/architecture/ERROR_CODES.md` (MODIFY) — add `FORBIDDEN_CROSS_TENANT` (422 or 403) if not present.
- `docs/USERTEST.md` (MODIFY) — add manual test entries for both flows.

### Data Flow

**SIM Compare**: FE selects 2 SIMs → `POST /api/v1/sims/compare {sim_id_a, sim_id_b}` → handler validates UUIDs and a≠b → fetches both via `simStore.GetByID(ctx, tenantID, id)` (tenant-scoped) → if either lookup returns `ErrSIMNotFound` → 404 `SIM_NOT_FOUND` → enriches names (operator, APN, policy) via existing `enrichSIMResponse`-equivalent logic → optionally fetches last session via `sessionStore.GetLastForSIM(...)` and last-auth via `cdrStore` → builds a `[]FieldDiff` array with `{field, value_a, value_b, equal}` entries → emits audit log `sim.compare` with metadata `{sim_id_b}` → 200 `{status:"success", data:{...}}`.

**System Config**: super_admin → `GET /api/v1/system/config` → handler reads injected `*config.Config` → calls `cfg.Redact()` to produce a struct that explicitly omits/maskes every `*_SECRET`/`*_PASSWORD`/`*_TOKEN`/`*_KEY` field → adds runtime metadata (build version/SHA/time, started_at, app_env, deployment_mode) → 200 envelope.

### API Specifications

**API-053 — `POST /api/v1/sims/compare`** (sim_manager+, JWT required, tenant-scoped)

Request:
```json
{ "sim_id_a": "<uuid>", "sim_id_b": "<uuid>" }
```

Response 200:
```json
{
  "status": "success",
  "data": {
    "sim_a": { "id": "...", "iccid": "...", "imsi": "...", "msisdn": "...", "state": "..." },
    "sim_b": { "id": "...", "iccid": "...", "imsi": "...", "msisdn": "...", "state": "..." },
    "diff": [
      { "field": "iccid",              "value_a": "8990...", "value_b": "8991...", "equal": false },
      { "field": "imsi",               "value_a": "...",     "value_b": "...",     "equal": false },
      { "field": "msisdn",             "value_a": "...",     "value_b": null,      "equal": false },
      { "field": "state",              "value_a": "active",  "value_b": "active",  "equal": true  },
      { "field": "state_changed_at",   "value_a": "...",     "value_b": "...",     "equal": false },
      { "field": "operator_id",        "value_a": "...",     "value_b": "...",     "equal": false },
      { "field": "operator_name",      "value_a": "...",     "value_b": "...",     "equal": false },
      { "field": "apn_id",             "value_a": "...",     "value_b": null,      "equal": false },
      { "field": "apn_name",           "value_a": "...",     "value_b": null,      "equal": false },
      { "field": "policy_version_id",  "value_a": "...",     "value_b": "...",     "equal": false },
      { "field": "static_ip",          "value_a": "10.0.0.1","value_b": null,      "equal": false },
      { "field": "esim_profile_state", "value_a": null,      "value_b": "enabled", "equal": false },
      { "field": "last_session_id",    "value_a": "...",     "value_b": null,      "equal": false },
      { "field": "last_auth_result",   "value_a": "accept",  "value_b": "reject",  "equal": false },
      { "field": "segment_count",      "value_a": 2,         "value_b": 1,         "equal": false }
    ],
    "compared_at": "2026-04-13T10:00:00Z"
  }
}
```

Errors:
- 400 `INVALID_FORMAT` — UUID parse failure.
- 422 `VALIDATION_ERROR` — sim_id_a == sim_id_b, or required field missing.
- 403 `FORBIDDEN_CROSS_TENANT` — either SIM belongs to another tenant (returned uniformly to avoid info leakage; in practice store returns `ErrSIMNotFound` when filtered by tenant — handler maps both to 404 `SIM_NOT_FOUND` to prevent enumeration; AC-1's "FORBIDDEN_CROSS_TENANT" is reserved for the explicit cross-tenant fallback path documented but the safer default is `SIM_NOT_FOUND` — see Bug Pattern Warnings).
- 404 `SIM_NOT_FOUND` — either SIM missing in caller's tenant.

**API-182 — `GET /api/v1/system/config`** (super_admin only, JWT required)

Response 200:
```json
{
  "status": "success",
  "data": {
    "app_env": "production",
    "deployment_mode": "single",
    "version": "1.0.0",
    "git_sha": "abc1234",
    "build_time": "2026-04-13T08:00:00Z",
    "started_at": "2026-04-13T09:00:00Z",
    "feature_flags": { "sba_enabled": false, "tls_enabled": true, "backup_enabled": true, "metrics_enabled": true, "rate_limit_enabled": true, "security_headers_enabled": true, "cron_enabled": true, "pprof_enabled": false },
    "protocols": { "radius_auth_port": 1812, "radius_acct_port": 1813, "radius_coa_port": 3799, "diameter_port": 3868, "diameter_tls_enabled": false, "sba_port": 8443, "sba_enabled": false, "radsec_port": 2083 },
    "limits": { "rate_limit_per_minute": 1000, "request_body_max_mb": 10, "default_max_sims": 1000000, "job_max_concurrent_per_tenant": 5 },
    "retention": { "purge_days": 90, "audit_days": 365, "cdr_days": 180 },
    "secrets_redacted": ["JWT_SECRET","ENCRYPTION_KEY","SMTP_PASSWORD","TELEGRAM_BOT_TOKEN","S3_SECRET_KEY","ESIM_SMDP_API_KEY","SMS_AUTH_TOKEN","DIAMETER_TLS_*","RADSEC_*","DATABASE_URL","REDIS_URL","NATS_URL","PPROF_TOKEN","JWT_SECRET_PREVIOUS"]
  }
}
```

Errors: 401 unauth (no JWT), 403 `INSUFFICIENT_ROLE` (non super_admin).

### Existing Components to REUSE

- `apierr.WriteSuccess` / `apierr.WriteError` and code constants (`CodeInvalidFormat`, `CodeValidationError`, `CodeNotFound`, `CodeForbidden`, `CodeInternalError`).
- `*sim.Handler` struct, `simStore.GetByID`, `enrichSIMResponse` (refactor: extract a `buildEnrichedSIMResponse(ctx, tenantID, sim) simResponse` if needed for compare reuse without duplicating the enrich logic).
- `audit.Auditor.CreateEntry(ctx, CreateEntryParams{Action:"sim.compare", EntityType:"sim", EntityID: simIDA.String(), AfterData: rawJSON({"sim_id_b": simIDB})})`.
- `internal/api/system/status_handler.go` for handler shape (uses `apierr.WriteSuccess`, exposes version/gitSHA/buildTime).
- Frontend: existing `web/src/types/sim.ts`, `web/src/lib/api.ts`, `Card`/`Table`/`Badge`/`Breadcrumb` UI atoms.

## Prerequisites

- STORY-077 merged (no direct dep but maintains plan-numbering continuity; commit sequence assumes 077 step-log finalized).
- STORY-011 (SIM CRUD) and STORY-001 (project scaffold) DONE — both already merged.
- No DB migration required.
- No new Go module dependency required.

## Story-Specific Compliance Rules

1. **Standard envelope**: every response wraps payload as `{status:"success"|"error", data?, error?, meta?}`.
2. **Tenant scoping**: `simStore.GetByID(ctx, tenantID, id)` already filters by tenant — both compare lookups MUST include tenantID. Never bypass.
3. **RBAC**: `/sims/compare` requires `RequireRole("sim_manager")`; `/system/config` requires `RequireRole("super_admin")`. Both wrapped in `JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious)`.
4. **Audit logging**: `sim.compare` action emitted on success ONLY; never log secrets in metadata.
5. **Redaction completeness**: `Redact()` MUST be a positive-list (whitelist of safe fields), not a negative-list (blacklist of known secrets). Future config additions default to redacted.
6. **No-info-leak**: cross-tenant SIM lookup returns 404 `SIM_NOT_FOUND` (not 403) to prevent ID enumeration; only when the explicit `FORBIDDEN_CROSS_TENANT` semantics is desired (e.g. caller has a flag) — for this story, prefer 404 uniformly. Document this decision in the handler's leading comment.
7. **No new code comments** unless required by handler-decision documentation (see point 6).

## Bug Pattern Warnings

- **Config redaction completeness (HIGH)**: a negative-list (`if name in {"JWT_SECRET", ...}`) silently leaks any future-added secret. Use a struct-based `RedactedConfig` with explicitly listed safe fields. Add a unit test that marshals the redacted struct to JSON, then asserts that the literal strings of every `*_PASSWORD`/`*_SECRET`/`*_TOKEN`/`*_KEY` env value (set via `t.Setenv("…", "DETECT_ME_xxxx")`) do NOT appear in the JSON output.
- **Cross-tenant comparison (HIGH)**: `simStore.GetByID(ctx, tenantID, id)` MUST receive the tenantID from the JWT context (`apierr.TenantIDKey`), never from the request body. Test: a SIM whose tenant differs from the caller's — store returns `ErrSIMNotFound` → handler returns 404 (not 200, not 500).
- **Self-compare allowed by accident**: `sim_id_a == sim_id_b` after UUID normalization (case-insensitive) must be rejected with 422 — don't normalize separately.
- **Nil dereferences in enrich**: `sim.APNID`, `sim.PolicyVersionID`, `sim.IPAddressID` are pointers — nil-guard before deref (existing `enrichSIMResponse` already does this; reuse, don't reinvent).
- **Audit hash chain**: do not call `auditSvc.CreateEntry` inside a loop or before the success branch — single call after the diff is built, before `WriteSuccess`.
- **Frontend regression**: the existing `compare.tsx` supports up to 3 SIMs but API-053 is strictly pairwise. Do NOT remove the 3-SIM UI capability — when 2 are selected, swap to the new endpoint; when 3 are selected, keep the legacy `useCompareSIMs` (per-detail fetch) path. Both code paths must coexist cleanly.

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Frontend regression on compare page (already shipped, 17 KB file) | Make changes additive: add `useSIMComparePair(a, b)` hook; only switch the 2-SIM render branch; keep 3-SIM legacy path intact. |
| Config redaction misses a future secret | Whitelist struct + table-driven redaction test that uses `t.Setenv` to inject sentinel values for every known secret env var and asserts absence in marshaled JSON. |
| Audit emission failure breaks compare response | Wrap `auditSvc.CreateEntry` in defer-style logging; do not surface error to client (compare succeeded). Mirror pattern from existing `Activate`/`Suspend` handlers. |
| Router-deps wiring forgotten in `cmd/argus/main.go` | T3 explicitly lists both code sites (router.go + main.go) and verify step boots the binary. |
| `FORBIDDEN_CROSS_TENANT` undocumented vs handler returning 404 | Resolve in the handler-leading comment + ERROR_CODES.md note: code reserved for future explicit-cross-tenant flows; current behavior is 404 SIM_NOT_FOUND. |

## Tasks

### Wave 1 — Backend handlers (parallel)

#### T1 — SIM compare handler + tests

- **Files**:
  - `internal/api/sim/compare.go` (NEW)
  - `internal/api/sim/compare_test.go` (NEW)
  - `internal/api/sim/handler.go` (MODIFY — extract `buildEnrichedSIMResponse(ctx, tenantID, sim) simResponse` helper from existing `Get`/`enrichSIMResponse` to avoid duplication)
- **Depends on**: —
- **Complexity**: Medium (highest in this story — diff struct, audit emission, enrich reuse)
- **Pattern ref**: Mirror handler shape from `internal/api/sim/handler.go` `Get` (lines 338–366) and bulk handler audit pattern from `bulk_handler.go`. Audit emission pattern: `internal/api/sim/handler.go` `Activate`/`Suspend` methods.
- **Context refs**: AC-1, AC-2, AC-3, AC-4, AC-5; Bug Pattern Warnings (cross-tenant, self-compare, nil deref, audit hash chain).
- **What**:
  1. Define `type compareRequest struct { SimIDA, SimIDB string }` with JSON tags.
  2. Define `type fieldDiff struct { Field string; ValueA, ValueB any; Equal bool }` and `type compareResponse struct { SimA, SimB simResponse; Diff []fieldDiff; ComparedAt time.Time }`.
  3. Implement `(h *Handler) Compare(w http.ResponseWriter, r *http.Request)`:
     - Extract tenantID from context (uniform with `Get`).
     - Decode JSON body; validate both UUIDs parseable, not nil, not equal (same-id → 422 `VALIDATION_ERROR`).
     - Fetch both SIMs via `simStore.GetByID(ctx, tenantID, ...)`; map `ErrSIMNotFound` → 404 `SIM_NOT_FOUND` (returns the missing ID in the error details).
     - Build enriched responses for both via `buildEnrichedSIMResponse`.
     - Optionally fetch last session via `sessionStore.GetLastForSIM(ctx, tenantID, simID)` (skip if `sessionStore == nil`).
     - Compute `[]fieldDiff` over the spec'd field list (AC-2).
     - Emit audit: `auditSvc.CreateEntry(ctx, CreateEntryParams{TenantID, UserID:&userID, Action:"sim.compare", EntityType:"sim", EntityID: simIDA.String(), AfterData: jsonRaw({"sim_id_b": simIDB.String()})})`.
     - `apierr.WriteSuccess(w, 200, compareResponse{...})`.
  4. Tests (`compare_test.go`):
     - Happy path: same-tenant, different SIMs → 200, diff array length matches spec, `equal` flag correct on identical fields, audit invoked once.
     - Same-id rejection → 422 `VALIDATION_ERROR`.
     - Cross-tenant (SIM B in another tenant) → 404 `SIM_NOT_FOUND`.
     - Missing SIM (random UUID) → 404.
     - Malformed UUID → 400 `INVALID_FORMAT`.
     - Missing tenant context → 403.
     - Audit failure does not break response (mock auditor returning error).
- **Verify**: `go test ./internal/api/sim/... -run Compare -cover` ≥ 85% line coverage on `compare.go`. `go vet` clean.

#### T2 — System config handler + Redact() + tests

- **Files**:
  - `internal/config/config.go` (MODIFY — append `Redact()` and `RedactedConfig`)
  - `internal/config/redact_test.go` (NEW)
  - `internal/api/system/config_handler.go` (NEW)
  - `internal/api/system/config_handler_test.go` (NEW)
- **Depends on**: —
- **Complexity**: Medium-Low
- **Pattern ref**: Handler shape from `internal/api/system/status_handler.go` (constructor + `apierr.WriteSuccess`); test shape from `internal/api/system/status_handler_test.go`.
- **Context refs**: AC-6, AC-7, AC-8; Bug Pattern Warnings (config redaction completeness).
- **What**:
  1. In `config.go`, define a `RedactedConfig` struct as a positive whitelist mirroring the response shape in API Specifications above (nested `FeatureFlags`, `Protocols`, `Limits`, `Retention` substructs). Add a fixed `SecretsRedacted []string` slice listing redacted env-var names for transparency.
  2. Implement `func (c *Config) Redact() RedactedConfig` returning the populated struct. Do NOT include any `*Secret`/`*Password`/`*Token`/`*Key`/`*URL` (DB/Redis/NATS URLs include credentials) field. Do not include TLS cert PATHS — paths leak filesystem layout; redact.
  3. `internal/api/system/config_handler.go`: `type ConfigHandler struct { cfg *config.Config; version, gitSHA, buildTime string; startedAt time.Time }`. Constructor `NewConfigHandler(cfg, version, gitSHA, buildTime)`. `(h *ConfigHandler) Serve(w, r)`: build payload = `cfg.Redact()` augmented with build/runtime metadata; `apierr.WriteSuccess(w, 200, payload)`.
  4. Tests:
     - **Redaction completeness (table-driven)**: For each of 14+ secret env names (JWT_SECRET, ENCRYPTION_KEY, SMTP_PASSWORD, TELEGRAM_BOT_TOKEN, S3_SECRET_KEY, ESIM_SMDP_API_KEY, SMS_AUTH_TOKEN, RADSEC_KEY_PATH, DIAMETER_TLS_KEY_PATH, PPROF_TOKEN, JWT_SECRET_PREVIOUS, RADIUS_SECRET, DATABASE_URL, REDIS_URL, NATS_URL): `t.Setenv(name, "DETECT_LEAK_<name>")`, load config, call `Redact()`, marshal to JSON, assert `!strings.Contains(out, "DETECT_LEAK_<name>")`.
     - **RBAC**: super_admin → 200; tenant_admin → 403 (router-level test; in handler-level test, just verify handler ignores caller and returns 200 with redacted body — RBAC enforced by middleware).
     - **Build metadata present**: response includes `version`, `git_sha`, `build_time`, `started_at`.
- **Verify**: `go test ./internal/config/... ./internal/api/system/... -run Config -cover` ≥ 85% on new files; redaction test passes for all 14+ secret names.

### Wave 2 — Wiring & Frontend (depends on W1)

#### T3 — Wire routes in router + main.go

- **Files**:
  - `internal/gateway/router.go` (MODIFY — add `SystemConfigHandler` field to `RouterDeps`; add 2 route blocks)
  - `cmd/argus/main.go` (MODIFY — instantiate `systemapi.NewConfigHandler(cfg, version, gitSHA, buildTime)`; pass via `RouterDeps`)
- **Depends on**: T1, T2
- **Complexity**: Low
- **Pattern ref**: Existing route wiring at `router.go:411–428` (sim routes, sim_manager+) and `router.go:756–762` (StatusHandler super_admin + JWTAuth pattern).
- **Context refs**: AC-1, AC-6.
- **What**:
  1. In `RouterDeps`, add `SystemConfigHandler *systemapi.ConfigHandler` near the other `systemapi.*` fields (line ~86).
  2. In the existing SIM route group (`router.go:411–428`, `RequireRole("sim_manager")`), append: `r.Post("/api/v1/sims/compare", deps.SIMHandler.Compare)`.
  3. After the existing `StatusHandler` block (`router.go:756–762`), add:
     ```go
     if deps.SystemConfigHandler != nil {
       r.Group(func(r chi.Router) {
         r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
         r.Use(RequireRole("super_admin"))
         r.Get("/api/v1/system/config", deps.SystemConfigHandler.Serve)
       })
     }
     ```
  4. In `cmd/argus/main.go` near `statusHandler := systemapi.NewStatusHandler(...)` (line ~1111), add: `systemConfigHandler := systemapi.NewConfigHandler(cfg, version, gitSHA, buildTime)`.
  5. In the `RouterDeps{...}` struct literal (line ~1174), add `SystemConfigHandler: systemConfigHandler,` next to `StatusHandler`.
- **Verify**: `go build ./...` succeeds; `make up` boots without error; `curl -X POST localhost:8080/api/v1/sims/compare` returns 401 (unauth); `curl localhost:8080/api/v1/system/config` returns 401.

#### T4 — Frontend `/sims/compare` consumes new endpoint

- **Files**:
  - `web/src/hooks/use-sims.ts` (MODIFY — add `useSIMComparePair` hook)
  - `web/src/pages/sims/compare.tsx` (MODIFY — add 2-SIM branch using new hook)
- **Depends on**: T3
- **Complexity**: Low
- **Pattern ref**: Existing hooks in `use-sims.ts` (`useSIM`, `useSIMUsage`); existing render structure in `compare.tsx`.
- **Context refs**: AC-9; Bug Pattern Warnings (frontend regression).
- **What**:
  1. In `use-sims.ts`, add:
     ```ts
     export function useSIMComparePair(idA: string, idB: string) {
       return useQuery({
         queryKey: [...SIMS_KEY, 'compare', idA, idB],
         queryFn: async () => {
           const res = await api.post<ApiResponse<SIMCompareResult>>('/sims/compare', { sim_id_a: idA, sim_id_b: idB })
           return res.data.data
         },
         enabled: !!idA && !!idB && idA !== idB,
         staleTime: 10_000,
       })
     }
     ```
     Add `SIMCompareResult` type to `web/src/types/sim.ts` mirroring response shape.
  2. In `compare.tsx`, add a branch: when `selectedIds.length === 2`, prefer `useSIMComparePair(selectedIds[0], selectedIds[1])`; render diff using the response's `diff[]` array (highlight rows where `equal=false`) instead of the manual client-side comparison. When `selectedIds.length !== 2`, keep existing `useCompareSIMs` per-detail path.
  3. Add toast/error-state handling: if compare endpoint returns 404, show user-friendly "SIM not found" message instead of crashing.
- **Verify**: `cd web && pnpm tsc --noEmit` clean; `pnpm build` succeeds; manual smoke: select 2 SIMs in `/sims/compare` → diff renders, no 404 in network tab.

### Wave 3 — Docs (depends on W1+W2)

#### T5 — USERTEST.md entries

- **Files**: `docs/USERTEST.md` (MODIFY)
- **Depends on**: T1, T2, T3, T4
- **Complexity**: Low
- **Pattern ref**: Existing test entries in `docs/USERTEST.md` for prior stories.
- **Context refs**: AC-11.
- **What**: Append two test scenarios:
  1. **SIM Compare**: Login as `sim_manager`, navigate `/sims/compare`, select 2 SIMs from same tenant, verify side-by-side diff renders with highlighted differences; verify audit log entry created (`/audit-logs?action=sim.compare`).
  2. **System Config (super_admin)**: Login as `admin@argus.io`, `curl -H "Authorization: Bearer <jwt>" localhost:8080/api/v1/system/config`, verify response includes build metadata and feature flags but NO secret values; login as tenant_admin, verify same call returns 403.
- **Verify**: file diff readable; entries follow existing format.

#### T6 — Verify api/_index + ERROR_CODES

- **Files**: `docs/architecture/api/_index.md` (VERIFY), `docs/architecture/ERROR_CODES.md` (MODIFY if needed)
- **Depends on**: T1, T2, T3
- **Complexity**: Low
- **Pattern ref**: Existing rows in api/_index.md.
- **Context refs**: AC-10.
- **What**:
  1. Confirm `API-053` row's path string is exactly `POST /api/v1/sims/compare` (verified — line 83) and `API-182` is `GET /api/v1/system/config` (verified — line 246). No edit needed unless paths drift during T3.
  2. In `ERROR_CODES.md`, search for `FORBIDDEN_CROSS_TENANT`. If absent, add a row near `SIM_NOT_FOUND` (line 118) and a `Code` constant near line 299 documenting it as "reserved — handler currently returns 404 SIM_NOT_FOUND for cross-tenant lookups to prevent enumeration; this code is documented for future explicit-disclosure flows."
- **Verify**: `grep -n FORBIDDEN_CROSS_TENANT docs/architecture/ERROR_CODES.md` returns a match; `grep -n 'API-053\|API-182' docs/architecture/api/_index.md` shows correct paths.

## Acceptance Criteria Mapping

| AC | Task(s) | Verify |
|----|---------|--------|
| AC-1 (compare endpoint, RBAC, tenant scope) | T1, T3 | T1 cross-tenant test; T3 route registration |
| AC-2 (diff response shape) | T1 | T1 happy-path test asserts diff field list |
| AC-3 (validation, error codes) | T1 | T1 same-id, malformed UUID, missing SIM tests |
| AC-4 (audit emission) | T1 | T1 test verifies auditor mock invoked with correct params |
| AC-5 (≥85% coverage) | T1 | `go test -cover` output |
| AC-6 (system/config endpoint, super_admin) | T2, T3 | T3 route guard; T2 handler returns redacted payload |
| AC-7 (secrets never returned) | T2 | T2 redaction table-driven test |
| AC-8 (integration test: 200/403/401, redaction) | T2 | T2 handler test + router-level role test |
| AC-9 (frontend `/sims/compare` consumes new endpoint) | T4 | Manual smoke; tsc clean |
| AC-10 (api/_index + ERROR_CODES) | T6 | Grep verification |
| AC-11 (USERTEST.md) | T5 | File diff |

## Wave Summary

| Wave | Tasks | Parallelizable | Depends on |
|------|-------|----------------|------------|
| W1 | T1, T2 | Yes | — |
| W2 | T3, T4 | Yes (T3 unblocks T4 frontend smoke, but code edits independent) | W1 |
| W3 | T5, T6 | Yes | W1, W2 |

Total: 6 tasks across 3 waves. Estimated effort: ~1 working day (matches story's S sizing).
