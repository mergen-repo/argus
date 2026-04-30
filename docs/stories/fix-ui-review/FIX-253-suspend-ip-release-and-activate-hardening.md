# FIX-253 — Suspend IP release + activate empty-pool guard + audit-on-failure (FIX-252 spinoff)

**Tier:** P2 | **Effort:** S | **Wave:** UI Review Remediation — backend hardening
**Dependencies:** none (FIX-252 closure resolved the surface symptom; latent defects below survive independently)
**Surfaced by:** FIX-252 Discovery + advisor analysis (2026-04-26)

## Problem Statement

FIX-252's surface bug (`POST /sims/{id}/activate` returning 500) was resolved by full schema reset (PAT-023 schema drift). Discovery surfaced **three latent backend defects** that survive that fix and need their own follow-up:

1. **`SIMStore.Suspend` does NOT release the SIM's allocated IP** (DEV-387 from FIX-252 plan):
   post-suspend, the previous `ip_addresses` row stays `state='allocated'` with `sim_id`
   pointing at the suspended SIM. The non-unique `idx_ip_addresses_sim`
   (`migrations/20260320000002_core_schema.up.sql:246`) means the next allocate doesn't
   collide on uniqueness, but the pool counter `used_addresses` becomes wrong; eventually
   `state='exhausted'` triggers spurious `POOL_EXHAUSTED` 422s on tenants with healthy pools.
2. **Activate handler has NO empty-pool guard** — when an APN has zero IP pools,
   `len(pools)==0` falls through to `simStore.Activate(..., uuid.Nil, ...)` which writes
   `ip_address_id = uuid.Nil` triggering FK `fk_sims_ip_address` violation (SQLSTATE 23503,
   `migrations/20260420000002_sims_fk_constraints.up.sql:54-57`) → bare 500. This is the H1
   hypothesis from the original FIX-252 plan; not the cause of the FIX-252 symptom but a
   real latent defect.
3. **Activate handler has audit log entry on success path only** — every failure branch
   (around lines 995, 1006, 1020, 1049 in `internal/api/sim/handler.go`) returns without
   writing an audit row. CLAUDE.md says "every state-changing operation creates an audit
   log entry" — failure attempts are state-meaningful (correlation ID, reason
   classification, security forensics) and must be audited too.

## Acceptance Criteria

- [ ] **AC-1:** `SIMStore.Suspend` atomically releases the allocated IP inside the suspend
      transaction: sets `ip_addresses.state='available'`, `sim_id=NULL`, decrements
      `ip_pools.used_addresses`, NULLs `sims.ip_address_id`. **Static allocations
      preserved** — no release for `allocation_type='static'`.
- [ ] **AC-2:** Activate handler explicit guard for `len(pools)==0`: returns
      `422 POOL_EXHAUSTED` with message "No IP pool configured for this APN", writes
      `sim.activate.failed` audit entry. NEVER bare 500 on this path.
- [ ] **AC-3:** Activate handler writes `sim.activate.failed` audit entry on EVERY failure
      branch (`get_sim_failed`, `list_pools_failed`, `allocate_failed`,
      `state_transition_failed`); log includes `tenant_id`, `sim_id`, `correlation_id`,
      classified `reason`.
- [ ] **AC-4:** Unit tests:
      - `TestSIMStore_Suspend_ReleasesIP` — dynamic allocation released atomically
      - `TestSIMStore_Suspend_PreservesStaticIP` — static allocation untouched
      - `TestActivate_PoolEmpty_Returns422` — empty pool → 422 (not 500) + audit row
      - `TestActivate_PoolFull_Returns422` — pool exhausted → 422 + audit row
      - `TestActivate_AuditOnFailure` — every failure branch produces an audit entry
      - `TestActivate_RoundTripAfterSuspend` — suspend → activate works post-AC-1 release
- [ ] **AC-5:** **Resume vs Activate verb resolution** (DEV-388 from FIX-252 plan): audit
      whether FE SIM-detail page (`web/src/pages/sims/detail.tsx:101` calls `/resume`)
      needs Resume to re-allocate IP after AC-1 changes Suspend semantics. Three options:
      (a) Resume re-allocates IP via the same handler-side allocate-then-call-store flow
      as Activate; (b) Resume becomes a thin alias for Activate (single endpoint
      consolidation); (c) Resume kept distinct with explicit "no-IP-on-resume" semantics
      and FE warning. Decide and implement.

## Files to Touch (best-effort)

- `internal/store/sim.go` — `SIMStore.Suspend` (release IP atomically)
- `internal/api/sim/handler.go` — `Handler.Activate` (empty-pool guard + audit-on-failure)
- `internal/api/sim/handler.go` — `Handler.Resume` (DEV-388 / AC-5 audit + decision)
- `internal/store/sim_test.go` — `TestSIMStore_Suspend_*`
- `internal/api/sim/handler_test.go` — `TestActivate_*` + `TestResume_*` if AC-5 needs tests
- Optionally: `internal/store/sim.go` `SIMStore.Activate` signature → `*uuid.UUID` (defensive,
  protects against any future caller passing `uuid.Nil`); discuss in plan.

## Plan Reference

- FIX-252 plan §"Tasks" (Tasks 2 / 3 / 4 / 5 from original — moved here)
- FIX-252 plan §"Decisions to Log" — old DEV-387 + old DEV-388 originate here
- decisions.md DEV-388 (FIX-253 spinoff rationale)
- bug-patterns.md PAT-023 (`schema_migrations` lying — context for why this story exists at all)

## Test Plan

- Unit: AC-4 list above
- Manual: round-trip suspend → activate on a SIM that previously had IP X; assert
  post-suspend `ip_address_id IS NULL` and the old `ip_addresses` row is now
  `state='available'`; post-activate, a NEW IP Y is allocated and `ip_address_id` updated.
- Manual: APN with zero pools (synthetic SIM) → activate → 422 `POOL_EXHAUSTED` envelope
  (NOT bare 500). Audit log shows `sim.activate.failed` row with reason
  `no_pool_for_apn`.
- Manual: audit log shows `sim.activate.failed` row with `reason` classification on each
  failure branch (use mock store or DB-fault injection in test environment).

## Risks & Regression

- Activate is on the hot path for every operator-driven activation. AC-1 changes Suspend
  semantics — eSIM provisioning and policy assignment may hold expectations about
  `sims.ip_address_id` persistence across suspend. Audit `internal/aaa/*` and
  `internal/policy/*` for reads of `sims.ip_address_id` on suspended SIMs BEFORE merging.
- Static allocations MUST be preserved (audit `allocation_type` discriminator before
  release in `SIMStore.Suspend`).
- AC-5 Resume decision may require small FE change (`web/src/pages/sims/detail.tsx`)
  depending on chosen option — flag in plan.
