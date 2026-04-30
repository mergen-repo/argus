# STORY-095: IMEI Pool Management (white/grey/black + bulk + lookup)

## User Story
As a security admin, I want to manage org-wide White / Grey / Black IMEI lists with full-IMEI and TAC-range entries, bulk-import them from CSV, and look up any IMEI to see its pool membership and bound SIMs, so that I can curate approved devices and react to stolen-device feeds without per-SIM editing.

## Description
Add tenant-scoped IMEI pool tables (TBL-56 whitelist, TBL-57 greylist, TBL-58 blacklist) and the CRUD + bulk import + lookup endpoints (API-331..335). Pools accept either a full 15-digit IMEI (`kind='full_imei'`) or an 8-digit TAC range prefix (`kind='tac_range'`). Bulk CSV import reuses the existing async job infrastructure (SVC-09, STORY-013) to deliver per-row partial-success / failure reporting. The IMEI Lookup endpoint cross-references a typed IMEI against all three pools and any SIMs currently bound to it. Frontend lands SCR-196 (pool tabs + bulk import) and SCR-197 (lookup modal/drawer). The DSL `device.imei_in_pool(...)` predicate (parsed in STORY-094) becomes functional in this story by reading these tables.

This story does not change AAA enforcement — STORY-096 wires the pre-check that consults blacklist/greylist/whitelist results. Pool data is consumed by `device.imei_in_pool()` in policy DSL evaluation immediately, however.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-05 (Policy Engine — pool reader), SVC-09 (Job system)
- Packages: `migrations/`, `internal/store/imei_pool.go` (new), `internal/api/imei_pool/handler.go` (new), `internal/job/imei_pool_import.go` (new), `internal/policy/dsl/evaluator.go` (wire `device.imei_in_pool`), `web/src/pages/settings/imei-pools/` (new), `web/src/pages/settings/imei-pools/lookup-modal.tsx` (new)
- Source: `docs/architecture/db/_index.md` (TBL-56, TBL-57, TBL-58), `docs/architecture/api/_index.md` (API-331..335), `docs/screens/SCR-196-imei-pool-management.md`, `docs/screens/SCR-197-imei-lookup.md`
- Spec: `docs/adrs/ADR-004-imei-binding-architecture.md`, `docs/brainstorming/decisions.md` DEV-412 (UX briefing)

## Screen Reference
- SCR-196 — Settings → IMEI Pools (4 tabs: White List, Grey List, Black List, Bulk Import)
- SCR-197 — IMEI Lookup modal (input) + drawer (rich result with pool memberships, bound SIMs, history)

## Acceptance Criteria
- [ ] AC-1: Migration `YYYYMMDDHHMMSS_imei_pools.up.sql` creates TBL-56 `imei_whitelist`, TBL-57 `imei_greylist`, TBL-58 `imei_blacklist` per the column specs in `db/_index.md`. Greylist adds `quarantine_reason TEXT NOT NULL`; blacklist adds `block_reason TEXT NOT NULL` and `imported_from VARCHAR(20) NOT NULL CHECK (imported_from IN ('manual','gsma_ceir','operator_eir'))`. Each table has UNIQUE (tenant_id, imei_or_tac), index on (tenant_id, kind), RLS policy tenant-scoped. `.down.sql` drops all three.
- [ ] AC-2: API-331 GET `/api/v1/imei-pools/{kind}` (`kind ∈ {whitelist, greylist, blacklist}`) returns cursor-paginated pool entries scoped to the caller's tenant. Filters: `tac` (8-digit prefix), `imei` (exact full IMEI), `device_model` (ILIKE). When `?include_bound_count=1`, each row carries `bound_sims_count` (number of SIMs with `bound_imei` matching this entry).
- [ ] AC-3: API-332 POST `/api/v1/imei-pools/{kind}` adds a single entry. Body: `{kind: "full_imei"|"tac_range", imei_or_tac, device_model?, description?, quarantine_reason?, block_reason?, imported_from?}`. Validates: full_imei must be 15 digits, tac_range must be 8 digits, greylist requires `quarantine_reason`, blacklist requires `block_reason` and `imported_from`. UNIQUE violation returns 409 `IMEI_POOL_DUPLICATE`. Audit `imei_pool.entry_added` written + hash-chained.
- [ ] AC-4: API-333 DELETE `/api/v1/imei-pools/{kind}/{id}` removes an entry. 204 success; 404 `POOL_ENTRY_NOT_FOUND` cross-tenant. Audit `imei_pool.entry_removed`.
- [ ] AC-5: API-334 POST `/api/v1/imei-pools/{kind}/import` accepts a multipart CSV (max 10 MB / 100 000 rows) with columns `imei_or_tac, kind, device_model, description, quarantine_reason, block_reason, imported_from`. Returns 202 `{job_id}`. Job runs async via SVC-09; reuses STORY-013 progress + result endpoints. Per-row malformed entries (bad length, missing required field for greylist/blacklist, UNIQUE conflict) are reported in the job result with row number + reason. Audit `imei_pool.bulk_imported` once per completed job.
- [ ] AC-6: API-335 GET `/api/v1/imei-pools/lookup?imei={imei}` returns `{lists: [{kind, entry_id, matched_via: "exact"|"tac_range"}], bound_sims: [{sim_id, iccid, binding_mode, binding_status}], history: [...]}`. `history` is the last 30 days of `imei_history` rows for any SIM that observed this IMEI (max 50, ordered DESC). Validates 15-digit IMEI input → 422 `INVALID_IMEI` otherwise. Empty lists when no match (200, not 404).
- [ ] AC-7: Move-between-lists action: a single endpoint or sequenced (DELETE old + POST new) that allows moving an entry from whitelist→greylist, greylist→blacklist, etc. Each move emits both audit events. UI flows match SCR-196 toolbar action.
- [ ] AC-8: TAC range matching at lookup time: when a 15-digit IMEI is queried, both the exact full-IMEI rows and any `kind='tac_range'` rows whose `imei_or_tac` equals `IMEI[0:8]` MUST be returned with the corresponding `matched_via` flag.
- [ ] AC-9: DSL `device.imei_in_pool('whitelist'|'greylist'|'blacklist')` becomes functional — evaluator reads from the new tables, applying both exact and TAC-range matching against `SessionContext.IMEI`. Result cached per evaluation pass to avoid duplicate queries when multiple `WHEN` clauses test the same predicate.
- [ ] AC-10: SCR-196 frontend: `/settings/imei-pools` page with 4 tabs (#whitelist, #greylist, #blacklist, #bulk-import). Each list tab renders a paginated table with column toggles per SCR-196, action menu per row (delete, move to other list), bulk-select toolbar (delete N), filter bar (TAC, IMEI, device model). Bulk Import tab renders the upload form, links to SVC-09 job result, downloads error CSV. Loading, empty, and error states match SCR-196 mockup.
- [ ] AC-11: SCR-197 frontend: `IMEI Lookup` modal/drawer accessible from SCR-196 toolbar AND from SCR-050 (Live Sessions) AND from SCR-020 (SIM List) toolbars. Compact-form modal accepts a 15-digit IMEI or 8-digit TAC; on submit, opens a rich drawer with three sections (List Membership, Bound SIMs, History). Cross-links to SIM Detail (#device-binding tab) and to Pool Detail row.
- [ ] AC-12: RBAC: read endpoints (API-331, API-335) require `sim_manager+`; write endpoints (API-332, API-333, API-334) require `tenant_admin+`. Forbidden roles return 403 `INSUFFICIENT_PERMISSIONS`.
- [ ] AC-13: Audit on every pool mutation; hash chain remains valid after a mixed sequence (add, delete, bulk import, manual entry). Audit shape carries `kind`, `imei_or_tac`, `entry_id`.
- [ ] AC-14: Regression — full `make test` green; SCR-196/197 do not break the existing settings sidebar nav; `make db-seed` produces zero rows in any pool by default.

## Dependencies
- Blocked by: STORY-094 (binding_mode and `device.imei_in_pool` parser must exist); SVC-09 / STORY-013 (job infrastructure)
- Blocks: STORY-096 (enforcement consumes pool membership for `allowlist`/blacklist hard-deny)

## Test Scenarios
- [ ] Integration: migrate up → all three pool tables exist with correct constraints; migrate down clean.
- [ ] Integration: API-332 add 15-digit IMEI to whitelist → row visible via API-331 GET.
- [ ] Integration: API-332 add 8-digit TAC to whitelist → API-335 lookup with a 15-digit IMEI sharing that TAC returns `matched_via: "tac_range"`.
- [ ] Integration: API-332 add same `imei_or_tac` twice → 409 `IMEI_POOL_DUPLICATE`.
- [ ] Integration: API-332 to greylist without `quarantine_reason` → 422.
- [ ] Integration: API-332 to blacklist without `block_reason` or `imported_from` → 422.
- [ ] Integration: API-334 bulk import 1000-row CSV with 5 malformed rows → job completes with 995 success / 5 failures; error CSV downloadable.
- [ ] Integration: API-335 lookup for IMEI not in any pool → `{lists: [], bound_sims: [], history: []}` with 200.
- [ ] Integration: API-335 lookup for IMEI in whitelist + bound to 2 SIMs → both lists populated correctly.
- [ ] Integration: API-333 delete entry → 204; subsequent GET 404.
- [ ] Integration: cross-tenant GET / POST / DELETE on entries from another tenant return 404 / 403.
- [ ] Unit: DSL evaluator — `device.imei_in_pool('blacklist')` returns true when SessionContext.IMEI matches a blacklist entry (full or TAC).
- [ ] E2E (Playwright): Settings → IMEI Pools → Whitelist tab → Add Entry → fill form → see new row.
- [ ] E2E: Bulk Import tab → upload CSV with 100 rows → poll job → see "completed" + per-row counts.
- [ ] E2E: SIM List → toolbar IMEI Lookup → enter 15-digit IMEI → drawer opens → click Bound SIM link → navigates to SIM Detail.
- [ ] Regression: full `make test` + Vitest + existing E2E suites green.

## Effort Estimate
- Size: M
- Complexity: Medium (3 tables + 5 endpoints + 2 frontend screens + bulk job; pattern reuse from STORY-013)
- Notes: First story to expose IMEI data to operators. Lookup is the most-used surface — invest in drawer ergonomics.
