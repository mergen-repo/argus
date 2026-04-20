# FIX-206: Orphan Operator IDs Cleanup + FK Constraints + Seed Fix

## Problem Statement
200 SIMs in DB point to non-existent `operator_id` values (`00000000-...-101/102/103` prefixes not present in `operators` table). Seed creates orphan operators that get deleted/renamed, leaving SIM rows with dangling FK references. No FK constraint enforces referential integrity. Cascade effect: F-82 DTO join returns NULL operator_name → F-14 UUID display across UI.

**Verified:**
```sql
SELECT s.operator_id, COUNT(*) FROM sims s 
  LEFT JOIN operators o ON s.operator_id=o.id WHERE o.id IS NULL 
  GROUP BY s.operator_id;
-- 3 orphan operator_ids, 200 SIMs affected
```

## User Story
As a platform operator, I want referential integrity for SIM operator/APN references so every SIM has a valid owner and downstream UI/analytics don't show orphan UUID placeholders.

## Architecture Reference
- DB: `sims.operator_id`, `sims.apn_id`, `sims.ip_address_id` FK constraints missing
- Seed: `migrations/seed/003_comprehensive_seed.sql` + `005*_full_pool_inventory.sql`
- Related: F-146a (sims.policy_version_id dual source vs policy_assignments)

## Findings Addressed
F-22 (partial — SIM orphan), F-63, F-81, F-83, F-93

## Acceptance Criteria
- [ ] **AC-1:** Data integrity job (new migration): reconcile orphan operator_ids. Policy: reassign to "unknown" operator (create if absent) OR suspend orphan SIMs pending manual review. Default: suspend + audit log entry per SIM.
- [ ] **AC-2:** After cleanup: add FK constraint `sims.operator_id REFERENCES operators(id)` with `ON DELETE RESTRICT`. Prevents future orphans.
- [ ] **AC-3:** Same for `sims.apn_id → apns(id)`, `sims.ip_address_id → ip_addresses(id)`.
- [ ] **AC-4:** Seed files reviewed — all operator_id/apn_id values must reference seeded operators/apns. Test: `make db-seed` → zero orphan rows.
- [ ] **AC-5:** Reconciliation migration is idempotent + safe — runs on production without data loss. Records summary: `N SIMs migrated to 'unknown', M SIMs suspended`.
- [ ] **AC-6:** Existing handlers that might create orphans audited: `SIM create`, `SIM import`, bulk operator_switch. Verify operator_id validation exists before insert.
- [ ] **AC-7:** Backend `store.sim.Create` / `Update` fails fast with 400 if operator_id/apn_id not found in tenant's visible records.
- [ ] **AC-8:** UI regression test: SIM list no longer shows `00000000-...-101` UUID prefixes (F-14 side effect).

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_orphan_cleanup_and_fks.up.sql` / `.down.sql`
- `migrations/seed/003_comprehensive_seed.sql` — fix operator/APN refs
- `migrations/seed/005a_full_pool_inventory.sql` — verify
- `internal/store/sim.go` — pre-insert validation
- `internal/api/sim/handler.go` — 400 for invalid references

## Risks & Regression
- **Risk 1 — Existing production data:** Reconciliation must be non-destructive. AC-1 suspend + audit preserves rows.
- **Risk 2 — Cascade delete risk:** `ON DELETE RESTRICT` blocks accidental operator deletion. Intentional deletes require SIM reassignment first.
- **Risk 3 — Migration lock on large table:** 10M sims locked during FK add. Mitigation: `ALTER TABLE ADD CONSTRAINT NOT VALID` → `VALIDATE CONSTRAINT` in separate transaction (Postgres online).

## Test Plan
- Migration dry-run: apply to clone of prod, verify row counts before/after
- Integration: attempt insert with invalid operator_id → 400
- Browser: no orphan UUIDs in SIM list

## Plan Reference
Priority: P0 · Effort: M · Wave: 1
