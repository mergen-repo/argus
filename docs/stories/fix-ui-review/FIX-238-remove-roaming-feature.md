# FIX-238: Remove Roaming Feature (Full Stack — UI + BE + DB + DSL)

## Problem Statement
Current `roaming_agreements` feature stores only **contract metadata** (partner_operator_name free text, SLA JSONB, cost_terms JSONB, dates, auto_renew). No operational integration:
- Partner HLR/HSS endpoints for auth forwarding — **not present**
- Session-to-agreement mapping — **not present**
- Real-time cost meter for roaming sessions — **not present**
- Active roaming session tracking — **not present**
- DSL parser/evaluator has `roaming` keyword but nothing consumes it meaningfully

User decision (DEV-253, 2026-04-19): remove the entire feature. Paper-contract storage adds maintenance burden with no operational value.

## User Story
As a product owner, I want the Roaming feature removed from Argus entirely so the codebase isn't cluttered with an unmaintained, incomplete, paper-only feature.

## Architecture Reference
- ~25 files across FE/BE/DB/DSL/tests

## Findings Addressed
- F-229 (scope reduction decision)

## Acceptance Criteria
- [ ] **AC-1:** **Frontend removal:**
  - Delete `web/src/pages/roaming/index.tsx`, `detail.tsx`
  - Delete `web/src/hooks/use-roaming-agreements.ts`
  - Delete `web/src/types/roaming.ts`
  - Remove from `web/src/router.tsx`
  - Remove "Roaming" sidebar entry in `components/layout/sidebar.tsx`
  - Remove "Agreements" tab from `operators/detail.tsx`
  - Remove roaming references from `policies/editor.tsx` (if DSL editor lists `roaming` as condition type — suppress)
  - Remove roaming sections from `settings/knowledgebase.tsx` (AAA reference doesn't mention roaming)

- [ ] **AC-2:** **Backend removal:**
  - Delete `internal/api/roaming/handler.go` + `handler_test.go`
  - Delete `internal/job/roaming_renewal.go` + `roaming_renewal_test.go`
  - Remove `JobTypeRoamingRenewal` from `internal/job/types.go`
  - Remove cron entry for `roaming_renewal` in `cmd/argus/main.go`
  - Remove roaming routes (6 entries) from `internal/gateway/router.go`
  - Remove `RoamingHandler` field from Dependencies struct
  - Review `internal/operator/sor/engine.go` + `roaming_test.go` + `types.go` — SoR may have roaming routing branches; remove those branches but KEEP multi-operator cost optimization (that value survives)
  - `internal/apierr/apierr.go` — remove roaming-specific error codes

- [ ] **AC-3:** **DSL grammar removal:**
  - `internal/policy/dsl/parser.go` — remove `roaming` keyword from grammar
  - `internal/policy/dsl/evaluator.go` — remove `roaming` condition evaluator branch
  - `parser_test.go` + `evaluator_test.go` — remove roaming test cases
  - Migration concern: existing policies with `roaming` in DSL will fail compile post-removal. AC-10 handles.

- [ ] **AC-4:** **DB removal:**
  - New forward migration `YYYYMMDDHHMMSS_drop_roaming_agreements.up.sql` drops `roaming_agreements` table
  - Original `20260414000001_roaming_agreements.up.sql` and `down.sql` stay in history (migrations immutable) — but unused
  - Seed `005_multi_operator_seed.sql` — remove any roaming INSERTs
  - `20260321000001_sor_fields.up.sql` — review for `roaming_partner_id` or `roaming_cost` columns on SIMs/sessions — remove if exist (OR mark deprecated + use in SoR cost routing)

- [ ] **AC-5:** **Config cleanup:**
  - `internal/config/config.go` — remove roaming-related config fields

- [ ] **AC-6:** **Test cleanup:**
  - `internal/aaa/bench/bench_test.go` — remove roaming references

- [ ] **AC-7:** **Cross-reference removal:**
  - Grep `grep -rn "roaming" internal/ web/src/` after change — only documentation + history references remain
  - `docs/PRODUCT.md`, `docs/ARCHITECTURE.md` — remove Roaming feature descriptions

- [ ] **AC-8:** **Regression gate:**
  - Full test suite passes (some tests updated to remove roaming assumption)
  - Policy DSL grammar still compiles all seed policies (none use `roaming` — verify seed)
  - Operator detail page loads without "Agreements" tab without crash
  - F-229 data portability notification event `data_portability.ready` (DSAR removal) NOT part of this story — belongs FIX-245

- [ ] **AC-9:** **Migration safety:**
  - Forward drop migration has `IF EXISTS` clauses — idempotent
  - Document in `docs/migrations-notes.md` that running against prod with active roaming_agreements will lose data (export CSV first, admin responsibility)

- [ ] **AC-10:** **Policy DSL backward compat:**
  - Any existing policy_versions with `roaming` keyword → migration job scans + marks as `state=archived` with warning (don't fail compile outright)
  - Or add compile-warning but still accept (strip roaming clause, warn user)
  - Chosen approach: scan + warn + strip + save updated DSL + audit entry

## Files to Touch
(~25 files — listed above per AC)

## Risks & Regression
- **Risk 1 — SoR engine roaming routing coupling:** Some tenants may rely on roaming cost optimization. Verify real usage (likely zero per operational assessment). Preserve generic multi-op cost routing — only remove roaming-specific code.
- **Risk 2 — Existing production DSL with roaming keywords:** AC-10 defensive — don't break existing policies mid-deploy.
- **Risk 3 — Operator detail tab removal breaks direct URL `?tab=agreements`:** Redirect to Overview tab.

## Test Plan
- Build passes after all deletes
- Regression suite: full `make test` passes
- Browser: Operator detail loads, sidebar shorter, no broken links
- Policy DSL: existing policies compile

## Plan Reference
Priority: P2 · Effort: L · Wave: 10 · Depends: FIX-212 (event envelope — remove roaming events from catalog)
