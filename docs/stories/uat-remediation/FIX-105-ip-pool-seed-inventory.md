# FIX-105: IP Pool Seed Inventory

> Tier 1 (foundational) — unblocks UAT-003 Activate path and any test that
> exercises admin-initiated IP allocation via `POST /sims/{id}/activate` or
> `POST /sims/{id}/assign-ip`.

## User Story

As a platform operator running a fresh deploy, I expect `POST /sims/{id}/activate`
to succeed with an IP assigned, rather than failing with `POOL_EXHAUSTED` because
the seed created `ip_pools` rows but left `ip_addresses` empty.

## Source Finding

- UAT Batch 1 report: `docs/reports/uat-acceptance-batch1-2026-04-18.md` — **F-15 CRITICAL**
- Evidence: UAT-003 Step 2 — `curl POST /sims/{id}/activate` → `422 POOL_EXHAUSTED — No IP addresses available`
- Root cause hypothesis: seed creates `ip_pools` with CIDR but does not materialise `ip_addresses` inventory rows
- STORY-092 precedent: seed-006 already materialises IPs for seed-005's 16 SIMs; the same pattern must extend to seed-003's pools

## Acceptance Criteria

- [ ] AC-1: Every `ip_pools` row created by seed has corresponding `ip_addresses` rows for its entire CIDR (minus network + broadcast) in state `available`
- [ ] AC-2: After `make db-seed` on a fresh DB, `SELECT COUNT(*) FROM ip_addresses WHERE pool_id = <any seed pool>.id AND state = 'available'` returns the expected usable-host count for each pool's CIDR
- [ ] AC-3: `POST /api/v1/sims/{id}/activate` on a freshly-seeded tenant succeeds (200) and returns an `ip_address` field; DB `sims.ip_address_id` IS NOT NULL
- [ ] AC-4: `ip_pools.used_addresses` counter increments when a SIM is allocated (either via trigger or app-level inc — decision documented in the plan)
- [ ] AC-5: `ReleaseIP` path (SIM terminate / accounting-stop) decrements the counter and returns the address to `available` state
- [ ] AC-6: Regression test: boot a fresh DB → seed → activate a SIM → assert IP attached
- [ ] AC-7: Counter reconciliation: `used_addresses = (SELECT COUNT(*) FROM ip_addresses WHERE pool_id = p.id AND state <> 'available')` for every pool after activate + release cycle

## Out of Scope

- Bulk import IP allocation (that moved to dynamic/auth-time per STORY-092 — see FIX-102)
- RADIUS/Diameter dynamic allocation path (STORY-092 already landed)
- Pool creation API (existing, not changing)

## Dependencies

- Blocked by: — (foundational)
- Blocks: FIX-101 (onboarding wizard Upload SIMs step), FIX-102 (bulk import completeness), rerun of UAT-001, UAT-002, UAT-003

## Architecture Reference

- Seed module: `internal/seed/` (locate pool-creation logic — grep `ip_pools`)
- Allocation store: `internal/store/ippool.go` — `AllocateIP` (~line 554), `ReleaseIP`
- Callers of AllocateIP: `internal/job/import.go:346`, `internal/api/sim/handler.go:876`, `internal/api/ippool/handler.go:432`
- STORY-092 plan (reference pattern): `docs/stories/test-infra/STORY-092-plan.md` seed-006 section
- Migration dir: `migrations/` — if counter-trigger chosen, new migration needed

## Test Scenarios

- [ ] Unit: `AllocateIP` on empty pool returns `ErrPoolExhausted`, on populated pool returns usable IP
- [ ] Unit: `ReleaseIP` returns IP to available state, decrements counter
- [ ] Integration: Fresh seed → activate 10 SIMs → verify 10 IPs allocated, `used_addresses = 10`, rest available
- [ ] Integration: Terminate those SIMs → verify IPs released, `used_addresses = 0`
- [ ] Regression: UAT-003 Step 2 passes end-to-end (no POOL_EXHAUSTED)

## Effort

M — primarily a seed migration/data change plus counter reconciliation. Hot-path code should not need modification.
