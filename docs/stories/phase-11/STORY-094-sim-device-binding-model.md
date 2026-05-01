# STORY-094: SIM-Device Binding Model + Policy DSL Extension

## User Story
As a platform engineer, I want SIM rows to carry per-SIM binding state and the Policy DSL to expose `device.*` predicates, so that operators can configure how each SIM verifies its device and policy authors can reason about device identity in rules.

## Description
Extend the `sims` table (TBL-10) with the device-binding columns mandated by ADR-004, add the `sim_imei_allowlist` join table (TBL-60) for the `allowlist` binding mode, and extend the Policy DSL grammar with the `device.*` namespace, `tac()` function, and `device.imei_in_pool()` membership predicate. Migrations default `binding_mode` to NULL for every existing SIM (DEV-410 — zero-risk opt-in). Backend layer only — operator-facing UI controls land in STORY-095 (pool screens), STORY-096 (enforcement reporting), and STORY-097 (re-pair workflow).

This story plumbs the data model and the parser/evaluator. It does not enforce anything at the AAA path (STORY-096 does that). After this story, a dry-run policy that references `device.binding_status == 'mismatch'` parses, type-checks, and evaluates without error against an enriched `SessionContext`.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-05 (Policy Engine)
- Packages: `migrations/`, `internal/store/sim.go`, `internal/store/imei_history.go` (read-side helpers), `internal/store/sim_imei_allowlist.go` (new), `internal/policy/dsl/lexer.go`, `internal/policy/dsl/parser.go`, `internal/policy/dsl/evaluator.go`
- Source: `docs/architecture/db/_index.md` (TBL-10 column extensions, TBL-59 imei_history, TBL-60 sim_imei_allowlist), `docs/architecture/DSL_GRAMMAR.md` (lines 43–55, 146–155), `docs/architecture/api/_index.md` (API-327, API-328, API-330, API-336)
- Spec: `docs/adrs/ADR-004-imei-binding-architecture.md`, `docs/brainstorming/decisions.md` DEV-409, DEV-410, DEV-412
- API contract: API-327 GET `/api/v1/sims/{id}/device-binding`, API-328 PATCH `/api/v1/sims/{id}/device-binding`, API-330 GET `/api/v1/sims/{id}/imei-history`, API-336 POST `/api/v1/sims/bulk/device-bindings`

## Screen Reference
- SCR-021f (SIM Detail → Device Binding tab) — read API-327, write API-328 (UI controls land in STORY-097's re-pair patch but the read+write endpoints must function in this story)
- No new pages added by this story; SCR-196/197/198 belong to STORY-095 and STORY-098

## Acceptance Criteria
- [ ] AC-1: Migration `YYYYMMDDHHMMSS_sim_device_binding_columns.up.sql` adds six nullable columns to `sims` (TBL-10): `bound_imei VARCHAR(15) NULL`, `binding_mode VARCHAR(20) NULL CHECK (binding_mode IN ('strict','allowlist','first-use','tac-lock','grace-period','soft'))`, `binding_status VARCHAR(20) NULL CHECK (binding_status IN ('verified','pending','mismatch','unbound','disabled'))`, `binding_verified_at TIMESTAMPTZ NULL`, `last_imei_seen_at TIMESTAMPTZ NULL`, `binding_grace_expires_at TIMESTAMPTZ NULL`. Partial index `idx_sims_binding_mode ON sims (binding_mode) WHERE binding_mode IS NOT NULL`. Existing rows untouched (`binding_mode = NULL`).
- [ ] AC-2: Corresponding `.down.sql` reverses all six columns and the partial index cleanly. `make db-migrate-down` then `make db-migrate-up` round-trips with no errors against a populated dev DB.
- [ ] AC-3: Migration `YYYYMMDDHHMMSS_imei_history.up.sql` creates TBL-59 `imei_history` (append-only) with columns and indexes per `db/_index.md` row TBL-59. RLS policy tenant-scoped. `.down.sql` drops it.
- [ ] AC-4: Migration `YYYYMMDDHHMMSS_sim_imei_allowlist.up.sql` creates TBL-60 `sim_imei_allowlist` with `(sim_id, imei)` PK, FK to `sims (sim_id) ON DELETE CASCADE`, RLS via the parent `sims` row. `.down.sql` drops it.
- [ ] AC-5: `internal/store/sim.go` adds CRUD for the new columns: `GetDeviceBinding(simID)`, `SetDeviceBinding(simID, mode, boundIMEI, statusOverride)`, `ClearBoundIMEI(simID)`. All queries are tenant-scoped (RLS-respecting). Cursor-based pagination of `sims` continues to work; new columns surface in the SIM DTO when present.
- [ ] AC-6: `internal/store/sim_imei_allowlist.go` provides `Add(simID, imei)`, `Remove(simID, imei)`, `List(simID)`, `IsAllowed(simID, imei) bool`. All operations enforce the parent SIM's `tenant_id`.
- [ ] AC-7: API-327 GET `/api/v1/sims/{id}/device-binding` returns the binding DTO (`bound_imei, binding_mode, binding_status, binding_verified_at, last_imei_seen_at, binding_grace_expires_at, history_count`). 404 `SIM_NOT_FOUND` cross-tenant or missing SIM.
- [ ] AC-8: API-328 PATCH `/api/v1/sims/{id}/device-binding` accepts `{binding_mode?, bound_imei?, binding_status_override?}`. Validates `binding_mode` enum (rejects unknown with 422 `INVALID_BINDING_MODE`), validates `bound_imei` is exactly 15 numeric digits (422 `INVALID_IMEI`), and emits audit `sim.binding_mode_changed` (hash-chained per existing audit framework). Setting `binding_mode=NULL` is allowed and clears related fields per row-level rules.
- [ ] AC-9: API-336 POST `/api/v1/sims/bulk/device-bindings` accepts a multipart CSV (`iccid, bound_imei, binding_mode`), enqueues a job via SVC-09 (reuse STORY-013 infrastructure), returns 202 `{job_id}`. Per-row failures (unknown ICCID, invalid IMEI, mode conflict) surface in the existing job-result endpoint. Each successful row writes audit `sim.binding_mode_changed`.
- [ ] AC-10: API-330 GET `/api/v1/sims/{id}/imei-history` is cursor-paginated (default 50, max 200), supports `since` (RFC3339) and `protocol` (radius/diameter_s6a/5g_sba) filters, returns rows from TBL-59. Cross-tenant access returns 404.
- [ ] AC-11: DSL lexer/parser accept the new tokens: `device.imei`, `device.tac`, `device.imeisv`, `device.software_version`, `device.binding_status`, `sim.binding_mode`, `sim.bound_imei`, `sim.binding_verified_at`, `device.imei_in_pool('whitelist'|'greylist'|'blacklist')`, `tac(<device-field>)`. Golden parser tests cover at least 6 representative DSL programs that mix these predicates.
- [ ] AC-12: DSL evaluator wires each new field to `SessionContext` (populated upstream by STORY-093 capture and by this story's SIM lookup). `tac(device.imei)` returns the first 8 digits of `device.imei`, or empty string when `device.imei` is empty. `device.imei_in_pool('whitelist')` etc. return boolean (always `false` until STORY-095 wires the pool tables — placeholder evaluator that does not crash).
- [ ] AC-13: A dry-run policy `WHEN device.binding_status == "mismatch" AND sim.binding_mode IN ("strict","tac-lock") THEN reject` evaluates against a synthetic `SessionContext` without panic and returns the expected boolean for each branch.
- [ ] AC-14: Audit log entries (`sim.binding_mode_changed`, future `sim.imei_repaired` from STORY-097) participate in the existing tamper-proof hash chain. Verifier passes after a sequence of binding-mode changes.
- [ ] AC-15: Regression — full `make test` green; existing AAA, policy, store, and API tests unchanged. `make db-seed` succeeds against the migrated schema; seed produces zero rows with non-NULL `binding_mode` (default off per DEV-410).

## Dependencies
- Blocked by: STORY-093 (capture must populate `SessionContext.IMEI`), STORY-022 (Policy DSL baseline)
- Blocks: STORY-095, STORY-096, STORY-097

## STORY-093 Handoff Notes (added by Reviewer 2026-05-01)
- **`session.Session.IMEI` / `SoftwareVersion` contract:** STORY-093 Gate extended `internal/aaa/session/session.go` with `IMEI string` and `SoftwareVersion string` fields (JSON `omitempty`). The S6a enricher in this story MUST read `session.Session.IMEI` from the in-memory/Redis session blob — NOT from a context-value stash. Field is populated by AUSF and UDM paths for SBA; RADIUS wires it via `SessionContext`. See `internal/aaa/session/session.go:89-90` and `internal/policy/dsl/evaluator.go:24-25`.
- **D-182 (Diameter listener deferral):** `internal/aaa/diameter/imei.go` ships parser-only (zero production callers) per ADR-004 §Out-of-Scope. STORY-094 S6a enricher is the intended first consumer — wire `ExtractTerminalInformation` at the Diameter Notify-Request / ULR listen path in this story (see ROUTEMAP D-182).
- **D-183 (5G non-3GPP PEI raw retention):** PROTOCOLS.md §PEI documents that `mac-` / `eui64-` prefix forms should be preserved as forensic `PEIRaw`. This is currently unimplemented (`SessionContext` has no `PEIRaw` field). Evaluate whether to add `SessionContext.PEIRaw string` + `ParsePEI` extension in this story or defer to STORY-097. If deferred again, re-target D-183 to STORY-097 in ROUTEMAP.
- **D-184 (AC-10 1M-SIM bench):** Run the literal 1M-SIM bench during STORY-094 binding pre-check and record the result in the STORY-094 plan §Perf note. Update STORY-093-plan.md §AC-10 Perf Addendum with the measured p95 number.

## Test Scenarios
- [ ] Integration: migrate up → `sims` table has all six new columns; `imei_history` and `sim_imei_allowlist` exist; `\d sims` shows partial index `idx_sims_binding_mode`.
- [ ] Integration: migrate down → all three migrations roll back cleanly, no orphan indexes, no orphan FKs.
- [ ] Integration: API-327 GET on a SIM with `binding_mode=NULL` returns DTO with nulls, no error.
- [ ] Integration: API-328 PATCH with `binding_mode="strict"` + valid 15-digit `bound_imei` succeeds; subsequent GET reflects the change; audit row `sim.binding_mode_changed` exists with hash-chain valid.
- [ ] Integration: API-328 PATCH with `binding_mode="bogus"` → 422 `INVALID_BINDING_MODE`, no DB write, no audit row.
- [ ] Integration: API-328 PATCH with `bound_imei="123"` → 422 `INVALID_IMEI`.
- [ ] Integration: API-336 bulk CSV with 3 rows (1 valid, 1 unknown ICCID, 1 invalid IMEI) → 202 + job_id; job result reports 1 success, 2 failures with reasons; 1 audit row written.
- [ ] Integration: API-330 history endpoint paginates correctly with `cursor` parameter; `since` filter narrows the result; cross-tenant access returns 404.
- [ ] Integration: cross-tenant API-327/328 access returns 404 (RLS enforced).
- [ ] Unit: DSL parser accepts each new token; golden tests for 6 sample policies pass.
- [ ] Unit: DSL evaluator returns expected booleans for `WHEN device.binding_status == "mismatch"` against synthetic SessionContext.
- [ ] Unit: `tac("359211089765432")` returns `"35921108"`; `tac("")` returns `""`.
- [ ] Unit: `sim_imei_allowlist.Add` then `IsAllowed` returns `true`; cross-tenant `IsAllowed` returns `false`.
- [ ] Regression: full Go test suite green, full Vitest suite green, `make db-seed` succeeds.

## Effort Estimate
- Size: M
- Complexity: Medium (3 migrations + grammar/AST extension + 4 endpoints, no AAA path changes)
- Notes: Backend-only. UI consumers (SCR-021f Device Binding tab actions) wire up in STORY-097.
