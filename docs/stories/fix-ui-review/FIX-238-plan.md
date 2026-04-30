# Implementation Plan: FIX-238 — Remove Roaming Feature (Full Stack)

## Goal
Surgically remove the entire Roaming feature (FE + BE + DB + DSL + tests + docs) while preserving generic multi-operator cost optimization in the SoR engine; provide AC-10 backward-compat for existing policy_versions containing the `roaming` keyword.

## Spec Reference
- Story: `docs/stories/fix-ui-review/FIX-238-remove-roaming-feature.md` (10 ACs, Effort L, Wave 10)
- Finding: `docs/reviews/ui-review-2026-04-19.md` `### F-229`
- Pattern precedent: FIX-245 → PAT-026 (6-layer feature-deletion sweep mandatory)

## Architecture Context

### Affected Layers (PAT-026 — 6-layer mandatory sweep)
| Layer | Files (verified by grep) |
|-------|-------------------------|
| 1. HTTP handler | `internal/api/roaming/handler.go` + `handler_test.go` |
| 2. Store (DB-access) | `internal/store/roaming_agreement.go` + `roaming_agreement_test.go` |
| 3. DB schema/migration | `migrations/20260414000001_roaming_agreements.up/down.sql` (immutable history) + NEW `drop_roaming_agreements.up.sql` |
| 4. Seed/templates | `migrations/seed/005_multi_operator_seed.sql:191` (policy DSL with `roaming = false`) |
| 5. Background job/processor | `internal/job/roaming_renewal.go` + `_test.go`; `internal/job/types.go` constants |
| 6. `cmd/argus/main.go` wiring | lines 62, 1572-1585, 1723 (import, store init, handler, processor, cron, deps) |

### Cross-cutting layers (extension to PAT-026)
- DSL grammar: `internal/policy/dsl/parser.go:32` (allowed match field) + `evaluator.go:15,118-119,248-249` (`SessionContext.Roaming`)
- SoR engine: `internal/operator/sor/engine.go` + `roaming_test.go` + `types.go:51` (audit-driven branch removal)
- Event catalog (FIX-212 follow-up): `internal/api/events/catalog.go:205-216` + `tiers.go:43` + `notification/service.go:727`
- API errors: `internal/apierr/apierr.go:110-113` (4 error codes)
- Config: `internal/config/config.go:136-137` (`RoamingRenewalAlertDays`, `RoamingRenewalCron`)
- View allowlist: `internal/store/user_view.go:24`
- Bench: `internal/aaa/bench/bench_test.go:262, 307`
- Gateway router: `internal/gateway/router.go:32, 96, 911-931` (6 routes + Dependencies field)
- Frontend: pages, hooks, types, sidebar, router, operator detail tab + 8 bonus refs

### SoR Engine Audit (Risk 1 — preserve generic cost optimization)
**REMOVE** (roaming-specific):
- `RoamingAgreementProvider` interface (lines 24-27)
- `agreementProvider` struct field (line 31) + constructor wire-up
- `activeAgreementByOperator` block (lines 102-142) — agreement loading + cost-overlay
- `agreementID` block (lines 172-180) — decision-time agreement attribution
- `SetAgreementProvider` method (line 220)
- `ReasonRoamingAgreement` const in `types.go:51`
- `roaming_test.go` entire file
- `SoRDecision.AgreementID` field (verify in types.go)

**KEEP** (generic multi-op cost):
- `sortCandidates` cost-tiebreak logic
- `ReasonCostOptimized` (lines 168-170)
- `ReasonRATPreference`, `ReasonIMSIPrefixMatch`, `ReasonManualLock`, `ReasonDefault`
- IMSI/RAT/CB filters
- `migrations/20260321000001_sor_fields.up.sql` (sor_priority, cost_per_mb, region, sor_decision — all generic; only top comment mentions "Steering of Roaming" — keep historical or rewrite to "Steering Operator Routing")

### AC-10 DSL Backward-Compat — DECISION: ARCHIVE strategy

**Chosen:** One-shot Go startup migration job that runs at boot:
1. Scans `policy_versions WHERE dsl_content ILIKE '%roaming%'` (case-insensitive, simple LIKE — DSL parsing not needed for detection)
2. For each match: appends an audit_logs entry (`event_type='policy_version.archived_roaming_removed'`, contains old DSL excerpt) and sets `state='archived'`
3. Logs total count at INFO level; idempotent (archived rows excluded from next run)
4. Lives in `internal/job/roaming_keyword_archiver.go` — invoked once from `main.go` boot phase, after `schemacheck` and before route registration

**Rejected:** strip+save+audit (spec text). Rationale: editing DSL strings is destructive (regex around clause boundaries is fragile, risks corrupting valid DSL). Archive is non-destructive — admin can manually rewrite + reactivate. Documented in `docs/brainstorming/decisions.md`.

### Event Catalog Cleanup (FIX-212 thread)
Roaming events found:
- `roaming.agreement.renewal_due` in `catalog.go:205`, `tiers.go:43`, `notification/service.go:727`
- Frontend alert mapping at `web/src/pages/alerts/index.tsx:148`
- Entity-button entry at `event-entity-button.tsx:21-22` (`agreement` + `roaming_agreement`)
All four sites must drop the entries together to keep catalog/wire/UI in sync.

## Database Schema

### Migration to ADD
```sql
-- migrations/YYYYMMDDHHMMSS_drop_roaming_agreements.up.sql
DROP TABLE IF EXISTS roaming_agreements CASCADE;
-- DOWN: re-create (see 20260414000001_roaming_agreements.up.sql for full DDL)
```

### Seed to MODIFY (W1 — BEFORE DSL parser removal)
`migrations/seed/005_multi_operator_seed.sql:191`:
```sql
-- BEFORE
'POLICY "abc-roaming-block" { MATCH { roaming = false } RULES { bandwidth_down = 10mbps } }'
-- AFTER (rename + change MATCH to a non-roaming field — e.g. apn IN to mirror xyz pattern)
'POLICY "abc-data-cap-secondary" { MATCH { apn IN ("iot.abc.local", "m2m.abc.local") } RULES { bandwidth_down = 10mbps } }'
```
Also rename row 169 policy `name='ABC Roaming Block'` → `'ABC Secondary Cap'` and `description` to match.

## Bug Pattern Warnings

- **PAT-026 (FIX-245)** — feature deletion 6-layer sweep MANDATORY. Each task lists which layer it covers; final task is project-wide grep gate.
- **PAT-006 family** — adding/removing in one place but not propagating. Final grep across `internal/`, `web/src`, `cmd/`, `migrations/seed/` MUST return zero hits in active code (tests + docs + immutable migration history allowed).
- **PAT-017** — config parameter threading: `RoamingRenewalAlertDays` + `RoamingRenewalCron` removal must propagate to `main.go:1576-1585` (cron registration) and any test that hardcodes them.
- **PAT-024** — fakes hide CHECK violations: not directly applicable here (no new INSERTs), but the AC-10 archiver INSERTs into audit_logs — verify `audit_logs` accepts `event_type='policy_version.archived_roaming_removed'`.
- **PAT-025** — semantic identifier confusion: not applicable.
- **No defer seed (memory)** — `make db-seed` MUST stay green. Seed change is W1, before parser keyword removal in W3.

## Tasks (5 waves, 14 tasks)

### Wave 1 — Migrations & Seed (sequential, 3 tasks)

#### Task 1: Drop migration for `roaming_agreements`
- **Files:** Create `migrations/<ts>_drop_roaming_agreements.up.sql` + `.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read any prior single-table-drop migration (or read `migrations/20260414000001_roaming_agreements.up.sql` for the inverse). New `up.sql` does `DROP TABLE IF EXISTS roaming_agreements CASCADE;`. New `down.sql` re-creates the table verbatim from the original up migration (for rollback safety).
- **Context refs:** "Database Schema > Migration to ADD"
- **What:** Idempotent forward drop; reversible down. AC-9 requirement: `IF EXISTS` clause. Add a top comment noting "running against prod with active roaming_agreements rows will lose data — admin must export CSV first".
- **Verify:** `make db-migrate` succeeds; `psql -c '\d roaming_agreements'` returns "Did not find any relation"; `make db-migrate-down` re-creates.

#### Task 2: Seed roaming-block policy update
- **Files:** Modify `migrations/seed/005_multi_operator_seed.sql` (lines 169 + 191)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Mirror the existing `xyz-data-cap` pattern at line 175-177 (apn IN match + bandwidth rule).
- **Context refs:** "Database Schema > Seed to MODIFY"
- **What:** Rename `ABC Roaming Block` → `ABC Secondary Cap`; change DSL MATCH from `roaming = false` to `apn IN ("iot.abc.local", "m2m.abc.local")`; preserve `compiled_rules` JSON shape (action: reject) or update to a non-roaming equivalent. Critical: this MUST happen BEFORE Task 8 (DSL parser removal) so seed re-runs are green.
- **Verify:** `make db-reset && make db-seed` runs clean.

#### Task 3: AC-10 startup roaming-keyword archiver job (Go-side)
- **Files:** Create `internal/job/roaming_keyword_archiver.go` + `_test.go`; modify `cmd/argus/main.go` boot phase to invoke once
- **Depends on:** Task 1 (DROP table — no dependency on roaming_agreements)
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/data_portability.go` (FIX-245 deletion template) for boot-time job structure; read any audit_logs INSERT in `internal/audit/` for audit entry shape.
- **Context refs:** "AC-10 DSL Backward-Compat — DECISION: ARCHIVE strategy", "Bug Pattern Warnings > PAT-024"
- **What:** Boot-time function `ArchiveRoamingKeywordPolicyVersions(ctx, db, auditLogger) error`:
  - SQL: `SELECT id, policy_id, version, dsl_content FROM policy_versions WHERE dsl_content ILIKE '%roaming%' AND state != 'archived'`
  - For each row: `UPDATE policy_versions SET state='archived' WHERE id=$1` + `INSERT INTO audit_logs(...)` with event_type `policy_version.archived_roaming_removed`, actor `system`, details containing policy_id + version + first 200 chars of dsl_content
  - Returns count; logs INFO `archived N policy_versions containing roaming keyword`
  - Idempotent — `state != 'archived'` guard
- **Verify:** Unit test (a) seeded `policy_versions` row with `roaming` keyword + state=`active` is archived after run; (b) audit_logs entry written; (c) re-run is no-op; (d) row without roaming keyword untouched.

### Wave 2 — Backend removals (parallel, 4 tasks) — depends on Wave 1

#### Task 4: Delete roaming HTTP handler + store + apierr codes
- **Files:** Delete `internal/api/roaming/handler.go` + `handler_test.go`; Delete `internal/store/roaming_agreement.go` + `roaming_agreement_test.go`; Modify `internal/apierr/apierr.go` (remove lines 110-113); Modify `internal/store/user_view.go:24` (remove `"roaming": true`)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** FIX-245 deletion precedent (handler+store deleted as one unit).
- **Context refs:** "Affected Layers — PAT-026 6-layer table"
- **What:** Layer 1 + Layer 2 of PAT-026 sweep. Compile MUST stay clean — every import of these packages must also be removed (Wave 2 Task 6 covers main.go + router; cross-confirm).
- **Verify:** `go build ./...` succeeds after Task 6 lands; `grep -rn "roamingapi\|RoamingAgreementStore" internal/` returns zero in active code.

#### Task 5: Delete roaming renewal job + types + config fields
- **Files:** Delete `internal/job/roaming_renewal.go` + `roaming_renewal_test.go`; Modify `internal/job/types.go` (remove `JobTypeRoamingRenewal` const + slice entry); Modify `internal/config/config.go` (remove `RoamingRenewalAlertDays` + `RoamingRenewalCron` fields)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** FIX-245 → PAT-026 layer 5 (background job removal).
- **Context refs:** "Affected Layers — PAT-026 6-layer table", "Bug Pattern Warnings > PAT-017"
- **What:** Layer 5 + config thread cleanup. PAT-017 ref — config field removal must propagate to main.go cron wiring (Task 6).
- **Verify:** `go build ./...` succeeds after Task 6; `grep -rn "JobTypeRoamingRenewal\|RoamingRenewal" internal/` returns zero.

#### Task 6: Gateway router + main.go wiring cleanup
- **Files:** Modify `internal/gateway/router.go` (remove import line 32, `RoamingHandler` field line 96, 6 route entries lines 911-931); Modify `cmd/argus/main.go` (remove import line 62, store init line 1573, handler init 1574, config read 1576-1578, processor register 1580-1582, cron entry 1585, deps wiring 1723; ADD invocation of archiver from Task 3 in boot phase)
- **Depends on:** Task 3 (archiver function exists), Task 4 (handler gone), Task 5 (job gone)
- **Complexity:** high
- **Pattern ref:** FIX-245 main.go wiring removal (DataPortabilityProcessor lines stripped). Read `cmd/argus/main.go` boot phase order (schemacheck → migrations → seed-check → archiver invocation here → route registration).
- **Context refs:** "Affected Layers", "AC-10 DSL Backward-Compat"
- **What:** Layer 6 of PAT-026 sweep. Critical that no orphan reference remains (PAT-026 root cause). After this, `go build ./cmd/argus/` MUST succeed. Add the AC-10 archiver call: `if err := job.ArchiveRoamingKeywordPolicyVersions(ctx, pg.Pool, log); err != nil { log.Error().Err(err).Msg("AC-10 archiver failed") }` — non-fatal (don't block boot on policy_versions write fail).
- **Verify:** `go build ./...` clean; `argus` boots; `curl /api/v1/roaming-agreements` returns 404 (route gone).

#### Task 7: SoR engine roaming-branch removal (audit-driven)
- **Files:** Modify `internal/operator/sor/engine.go` (remove lines 24-27, 31, 102-142, 172-180, 220 per audit); Modify `internal/operator/sor/types.go` (remove line 51 `ReasonRoamingAgreement` + `SoRDecision.AgreementID` field if present); Delete `internal/operator/sor/roaming_test.go`
- **Depends on:** Task 4 (`store.RoamingAgreement` type gone)
- **Complexity:** high
- **Pattern ref:** Surgical edit — read engine.go lines 60-218 to confirm what stays; the cost-tiebreak logic at line 168-170 (`ReasonCostOptimized`) MUST remain.
- **Context refs:** "SoR Engine Audit (Risk 1 — preserve generic cost optimization)"
- **What:** Remove agreement-coupling code paths. KEEP candidate sorting, IMSI/RAT/CB filters, generic cost optimization. Comment top of `migrations/20260321000001_sor_fields.up.sql`: "Steering of Roaming" → "Steering of Routing" OR leave as historical (immutable migration — preferred: leave). Existing tests in `engine_test.go` (non-roaming) must still pass.
- **Verify:** `go test ./internal/operator/sor/...` passes; engine still produces `ReasonCostOptimized` in unit test; `grep -n "Roaming\|roaming\|agreementProvider" internal/operator/sor/` returns zero in active files.

### Wave 3 — DSL grammar removal (sequential, 1 task) — depends on Wave 1 Task 2

#### Task 8: Remove `roaming` from DSL parser + evaluator + tests
- **Files:** Modify `internal/policy/dsl/parser.go:32` (remove `"roaming": true,`); Modify `internal/policy/dsl/evaluator.go:15,118-119,248-249` (remove `Roaming bool` field from `SessionContext`, remove case branches); Modify `internal/policy/dsl/parser_test.go:201,243,274` (remove 3 test cases or rewrite to non-roaming match field); Modify `internal/policy/dsl/evaluator_test.go:407-422` (remove roaming evaluation test)
- **Depends on:** Task 2 (seed updated), Task 7 (any roaming-using callers removed)
- **Complexity:** medium
- **Pattern ref:** Read `parser.go:32` allowed-fields map; remove the entry preserving valid syntax.
- **Context refs:** "Affected Layers > Cross-cutting"
- **What:** DSL grammar layer. Tests rewritten to use a still-supported field (`apn`, `imsi`, `tenant`, `rat_type`, `sim_type`). After this, ANY existing prod policy_version DSL containing `roaming` will fail compile — but Task 3's archiver runs BEFORE policies are loaded at runtime (boot order: archiver → routes → traffic), so nothing in production attempts to compile a roaming policy.
- **Verify:** `go test ./internal/policy/dsl/...` passes; `grep -n "Roaming\|roaming" internal/policy/dsl/` returns zero in active code.

### Wave 4 — Frontend removals (parallel, 4 tasks) — independent of BE waves

#### Task 9: Delete roaming pages, hooks, types
- **Files:** Delete `web/src/pages/roaming/index.tsx`; Delete `web/src/pages/roaming/detail.tsx`; Delete `web/src/hooks/use-roaming-agreements.ts`; Delete `web/src/types/roaming.ts`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** N/A — pure deletion.
- **Context refs:** "Affected Layers"
- **What:** Whole-file removal. Subsequent tasks remove imports.
- **Verify:** Files don't exist; `npm run build` clean after Tasks 10-11 land.

#### Task 10: Router + Sidebar cleanup
- **Files:** Modify `web/src/router.tsx` (remove lines 71-72 lazy imports, lines 173-174 routes); Modify `web/src/components/layout/sidebar.tsx:95` (remove `Roaming` entry)
- **Depends on:** Task 9
- **Complexity:** low
- **Pattern ref:** Read `web/src/router.tsx` route array structure.
- **Context refs:** "Affected Layers"
- **What:** Routes and nav entry removal. Risk 3 mitigation in Task 11.
- **Verify:** Visit `/roaming-agreements` in browser → Not Found page; sidebar no longer shows "Roaming".

#### Task 11: Operator detail tab cleanup + Risk 3 redirect
- **Files:** Modify `web/src/pages/operators/detail.tsx` (remove lines 68, 70, 984-1024 = entire Agreements tab + tab nav entry; redirect `?tab=agreements` → Overview tab via tab handler default-case)
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/operators/detail.tsx` tab handler — replicate redirect for unknown tab values to Overview.
- **Context refs:** "Affected Layers", "Risks (Risk 3)"
- **What:** Tab removal + URL backward-compat. Existing bookmarks `/operators/:id?tab=agreements` MUST silently redirect to `?tab=overview` (no 404).
- **Verify:** Manually load `/operators/<id>?tab=agreements` → Overview tab renders; no console errors.

#### Task 12: Frontend bonus refs cleanup (8 sites)
- **Files:**
  - Modify `web/src/components/event-stream/event-entity-button.tsx:21-22` (remove `agreement` + `roaming_agreement` entity routes)
  - Modify `web/src/lib/api/policies.ts:86` (remove `'roaming'` from match_fields list)
  - Modify `web/src/pages/alerts/index.tsx:148` (remove `roaming.agreement.renewal_due` mapping)
  - Modify `web/src/pages/policies/editor.tsx:627` (remove `WHEN NOT roaming = true` example; replace with non-roaming example, e.g. `WHEN apn = "iot.local" {...}`)
  - Modify `web/src/pages/settings/knowledge-base/sections/section-9-business-rules-reference.tsx:115,127` (rephrase "SIM roaming outside declared regions" → "SIM access from disallowed operator region"; "Roaming zone change" → "Operator/region change")
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** N/A — line-level edits.
- **Context refs:** "Affected Layers > Cross-cutting", "Event Catalog Cleanup"
- **What:** Cross-cutting frontend cleanup. AC-1 explicit + advisor's "bonus refs" enumeration. Note: knowledge-base lines 115/127 are conceptual, not feature refs — rephrase to remove "roaming" terminology entirely (no broken links since these are descriptive text only).
- **Verify:** `grep -rn 'roaming\|Roaming' web/src/` returns ONLY git history / test fixtures (zero matches in src code expected).

### Wave 5 — Catalog cleanup, bench, docs, regression gate (sequential where noted, 2 tasks)

#### Task 13: Event catalog + notification mapping + bench cleanup
- **Files:**
  - Modify `internal/api/events/catalog.go` (remove lines 204-216 entire `roaming.agreement.renewal_due` event entry)
  - Modify `internal/api/events/tiers.go:43` (remove the `roaming.agreement.renewal_due` tier entry)
  - Modify `internal/notification/service.go:727` (remove the `roaming.agreement.renewal_due` source mapping)
  - Modify `internal/notification/service_test.go` (any test referencing this event type — remove)
  - Modify `internal/aaa/bench/bench_test.go:262, 307` (remove `Field: "roaming"` matcher line + `Roaming: false` SessionContext field)
- **Depends on:** Wave 2 + Wave 3 (engine + DSL gone)
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/events/catalog.go` event entry struct shape — remove entire struct literal block.
- **Context refs:** "Event Catalog Cleanup (FIX-212 thread)", "Affected Layers"
- **What:** FIX-212 catalog hygiene + AC-6 bench cleanup. Cross-layer consistency: catalog/tiers/notification mapping all drop the same event type.
- **Verify:** `go test ./internal/api/events/... ./internal/notification/... ./internal/aaa/bench/...` passes; `grep -n "roaming" internal/api/events/ internal/notification/ internal/aaa/bench/` returns zero.

#### Task 14: Docs update + final grep regression gate (AC-7, AC-8)
- **Files:**
  - Modify `docs/PRODUCT.md` (remove Roaming feature section, mention F-229 deletion in changelog)
  - Modify `docs/ARCHITECTURE.md` (remove Roaming-Agreements service/component refs)
  - Modify `docs/architecture/services/_index.md` if STORY-071 listed (mark as REMOVED — note the feature was deleted by FIX-238)
  - Modify `docs/architecture/db/_index.md` (remove TBL row for roaming_agreements)
  - Modify `docs/migrations-notes.md` (or create — AC-9: doc note about prod data loss without export)
  - Modify `docs/brainstorming/decisions.md` (record AC-10 archive-vs-strip decision)
  - Modify `docs/brainstorming/bug-patterns.md` (PAT-026 RECURRENCE entry — document how this story applied the 6-layer sweep)
- **Depends on:** All prior tasks
- **Complexity:** low
- **Pattern ref:** Read recent doc updates (FIX-245 closure docs).
- **Context refs:** "Bug Pattern Warnings", "AC-10 DSL Backward-Compat"
- **What:** Final docs sync. **Final grep gate:**
  ```bash
  grep -rn "roaming\|Roaming\|ROAMING" \
    --include='*.go' --include='*.ts' --include='*.tsx' --include='*.sql' \
    internal/ web/src/ cmd/ migrations/seed/
  ```
  MUST return ZERO matches in active code. Allowed: `migrations/20260414000001_*` (immutable history), `migrations/<new>_drop_roaming_agreements.*` (the drop migration itself contains "roaming" in name only — body uses `IF EXISTS`), `docs/`, git history.
- **Verify:** `make test` passes. `make db-reset && make db-migrate && make db-seed` clean (no errors). Browser smoke: operator detail loads, sidebar shows no Roaming, `/roaming-agreements/*` shows Not Found, policy editor compiles seed policies. Final grep returns zero in active code.

## Acceptance Criteria Mapping
| AC | Implemented In | Verified By |
|----|----------------|-------------|
| AC-1 FE removal | Tasks 9, 10, 11, 12 | Task 14 (browser smoke + grep) |
| AC-2 BE removal | Tasks 4, 5, 6, 7, 13 | Task 14 (grep + tests) |
| AC-3 DSL grammar | Task 8 | Task 14 (`go test ./internal/policy/dsl/...`) |
| AC-4 DB removal + seed | Tasks 1, 2 | Task 14 (`make db-reset && make db-seed`) |
| AC-5 config cleanup | Task 5 (config.go) | Task 14 (`go build`) |
| AC-6 test cleanup | Tasks 4, 5, 7, 8, 13 | Task 14 (`make test`) |
| AC-7 grep verification | Task 14 | Task 14 final grep |
| AC-8 regression gate | Task 14 | `make test` + `make db-seed` + browser smoke |
| AC-9 migration safety | Task 1 (`IF EXISTS` + comment) + Task 14 (migrations-notes.md) | Task 14 doc review |
| AC-10 DSL backward compat | Task 3 (archiver) + Task 6 (boot wiring) | Task 3 unit test + idempotency check |

## Risks & Mitigations
- **Risk 1 — SoR cost optimization regression:** Audit at Task 7 explicitly preserves `ReasonCostOptimized`/`sortCandidates`/IMSI+RAT filters. Existing `engine_test.go` (non-roaming) must remain green.
- **Risk 2 — Existing prod DSL with `roaming`:** Task 3 archiver runs at boot BEFORE traffic — no compile attempt occurs.
- **Risk 3 — `?tab=agreements` deep links:** Task 11 redirects unknown tabs to Overview.
- **Risk 4 — Seed regression:** Task 2 (W1) updates seed BEFORE Task 8 (W3) removes the keyword — order is mandatory.
- **Risk 5 — Orphan event publisher (PAT-026):** Task 13 removes catalog entry; Tasks 4/5 remove publisher (no remaining job emits `roaming.agreement.renewal_due` after roaming_renewal job is deleted).
- **Risk 6 — Build break mid-wave:** Wave 2 tasks all touch independent files but `go build` only passes after Task 6 (gateway/main wiring) — dispatch Task 4/5/7 in parallel, then Task 6 sequentially.

## Tech Debt
- D-XXX: Re-evaluate whether `migrations/20260321000001_sor_fields.up.sql` comment header should be rewritten to drop the historical "Steering of Roaming" naming — defer (immutable migration; cosmetic only).

## Mock Retirement
N/A — feature removal, no mocks.
