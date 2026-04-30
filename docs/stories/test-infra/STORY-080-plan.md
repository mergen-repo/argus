# Implementation Plan: STORY-080 — Realistic Multi-Operator Test Seed

## Goal

Seed the system with a realistic multi-operator, multi-tenant data set so the rest of Test Infrastructure (STORY-082 simulator, live-stream verification, end-to-end demo) has concrete SIMs, APNs, IP pools, policies, and operator grants to exercise. This replaces the ambiguity of the current single-tenant + mock-operator footprint with fixtures that mirror a real Turkish MVNO on-prem deployment.

Idempotent SQL only — no Go code changes. New SIM partitions are a **hard requirement**; without them, SIM inserts for the three real operators will fail at runtime.

## Architecture Context

### Components Involved

- `migrations/seed/005_multi_operator_seed.sql` (NEW) — idempotent seed using `ON CONFLICT DO NOTHING` everywhere. Runs after the existing 001–004 seeds without disturbing them.
- `migrations/seed/` (NO changes to 001–004) — `001_admin_user.sql` (super_admin + demo tenant `00000000-...-0001`), `002_system_data.sql` (mock operator `0100` + partition `sims_mock`), `003_comprehensive_seed.sql` (optional large dataset), `004_notification_templates.sql` all stay intact.
- `cmd/argus/seed.go` (VERIFY, not modify) — confirm the `argus seed` subcommand picks up `005_*.sql` via its directory-scan. If it hard-codes a file list, we will need a minimal patch noted as a task.
- `docs/architecture/db/_index.md` (MODIFY) — add 5 operator partitions (`sims_turkcell`, `sims_vodafone`, `sims_turk_telekom` — plus existing `sims_mock`) to the partition section.

### Data To Seed

All UUIDs are deterministic (`00000000-0000-0000-0000-000000000XXX`) for predictable test fixtures.

**Tenants (2)**
- `...0001` — **XYZ Edaş** (already seeded in 001 as "Argus Demo"; rename + re-set domain to `xyz.local`)
- `...0002` — **ABC Edaş**, domain `abc.local`, active

**Operators (3)**
| UUID | Name | Code | MCC | MNC | Adapter | IMSI Prefix |
|------|------|------|-----|-----|---------|-------------|
| `...0101` | Turkcell | `turkcell` | 286 | 01 | mock | `28601` |
| `...0102` | Vodafone TR | `vodafone` | 286 | 02 | mock | `28602` |
| `...0103` | Türk Telekom | `turk_telekom` | 286 | 03 | mock | `28603` |

All three use `adapter_type='mock'` with low-latency happy-path config (`{"latency_ms":5}`). The simulator (STORY-082) sits in front of these from the SIM-side — it sends RADIUS traffic as if from subscriber equipment; the mock adapter remains Argus's upstream-mock for its own subscriber lookup. Two different layers.

**SIM Partitions (3)** — one per new operator
```sql
CREATE TABLE sims_turkcell PARTITION OF sims FOR VALUES IN ('...0101');
CREATE TABLE sims_vodafone PARTITION OF sims FOR VALUES IN ('...0102');
CREATE TABLE sims_turk_telekom PARTITION OF sims FOR VALUES IN ('...0103');
```

**Operator Grants (6)** — both tenants granted all three operators
- XYZ Edaş (`...0001`) × {Turkcell, Vodafone, Türk Telekom}
- ABC Edaş (`...0002`) × {Turkcell, Vodafone, Türk Telekom}

**APNs (6, 3 per tenant)**
- XYZ: `iot.xyz.local`, `m2m.xyz.local`, `private.xyz.local`
- ABC: `iot.abc.local`, `m2m.abc.local`, `private.abc.local`

**IP Pools (4, 2 per tenant)**
- XYZ-iot: `10.100.0.0/22` (1024 IPs), linked to `iot.xyz.local`
- XYZ-m2m: `10.100.4.0/22`, linked to `m2m.xyz.local`
- ABC-iot: `10.200.0.0/22`, linked to `iot.abc.local`
- ABC-m2m: `10.200.4.0/22`, linked to `m2m.abc.local`

**SIMs (16, 8 per tenant)** — round-robin across the 3 operators per tenant:
- XYZ: 3 Turkcell + 3 Vodafone + 2 Türk Telekom = 8
- ABC: 3 Turkcell + 3 Vodafone + 2 Türk Telekom = 8

Per-SIM fields:
- `imsi`: `<operator_prefix>0<tenant_digit><seq>` — e.g. XYZ Turkcell SIM #1 = `2860100001`, ABC Vodafone SIM #2 = `2860200012`
- `iccid`: `8990<imsi>` (19 digits)
- `msisdn`: `+905<operator_digit><seq>` pattern
- `apn_id`: round-robin among tenant's APNs
- `state`: all `active`
- `policy_version_id`: NULL initially (policy matcher will assign after first session; we deliberately do NOT pre-seed this to test the auto-match path)
- `ip_address_id`: NULL initially (IP pool allocator fills on first authentication)

**Policies (4, 2 per tenant)**
- XYZ-basic-data-cap: simple DSL limiting usage at 5GB/month
- XYZ-business-hours: time-based policy (9:00–18:00 access)
- ABC-basic-data-cap: 10GB/month
- ABC-roaming-block: reject sessions from operators outside tenant's grants (just a DSL example; no actual roaming in this seed)

Each policy has one policy_version (v1) marked as current.

## Tasks

1. **Write `005_multi_operator_seed.sql`**
   - UPDATE tenant `...0001` name/domain to XYZ Edaş / xyz.local (idempotent via `WHERE id = ...`)
   - INSERT tenant `...0002` ABC Edaş
   - INSERT 3 operators (ON CONFLICT on `code` DO NOTHING)
   - CREATE 3 SIM partitions (wrapped in DO blocks like 002 does for mock)
   - INSERT 6 operator_grants
   - INSERT 6 APNs
   - INSERT 4 IP pools + link to APNs
   - INSERT 16 SIMs
   - INSERT 4 policies + 4 policy_versions (active)
   - All with explicit deterministic UUIDs for predictability

2. **Verify `argus seed` pickup** (`cmd/argus/seed.go`)
   - Test: run `make db-seed` on fresh DB — if it skips 005, adjust the seed runner's file-glob.

3. **Docs**
   - `docs/architecture/db/_index.md`: add 3 new SIM partitions to the partition listing
   - `docs/GLOSSARY.md`: add "Test Seed 005" brief entry if warranted (optional)

4. **Smoke test after seed**
   - `GET /api/v1/tenants` returns 2 entries
   - `GET /api/v1/operators` returns 4 (3 real + 1 mock)
   - `GET /api/v1/sims?limit=100` returns 16
   - `GET /api/v1/apns` returns 6
   - `GET /api/v1/ip-pools` returns 4

## Acceptance Criteria

- **AC-1** Running `make db-seed` on a freshly-migrated DB completes without error and creates exactly: 2 tenants, 4 operators (3 real + 1 mock), 4 SIM partitions (mock + 3 real), 6 operator_grants, 6 APNs, 4 IP pools (linked to APNs), 16 SIMs, 4 policies with 4 active policy_versions.
- **AC-2** Running the seed a **second time** on an already-seeded DB completes silently (zero row changes) — idempotency verified.
- **AC-3** Every SIM's `tenant_id` matches a real tenant, its `operator_id` has an `operator_grant` for that tenant, and its `apn_id` belongs to its tenant.
- **AC-4** IMSI format follows `<operator_mcc><operator_mnc>0<tenant_digit><seq>` and is unique across all 16 SIMs.
- **AC-5** SIM partitions exist for all 3 real operators — SELECT-from-partition works for each: `SELECT COUNT(*) FROM sims_turkcell`.

## Risks

- **005 runs before 002's mock partition**: no — numbering guarantees 005 comes after 002.
- **Tenant rename breaks references**: the seed only updates `name` and `domain` of `...0001`; FK relationships are via UUID, unaffected. Admin login (`admin@argus.io`) continues to work.
- **Auto policy matcher** (cd41969 committed matcher) may immediately assign policies to SIMs post-seed if sessions start. Acceptable — that's the exercise. Seed itself sets `policy_version_id = NULL`; matcher fills via event.
- **Existing 003 comprehensive seed conflicts**: 003 creates a large synthetic data set under the demo tenant. If both run, UUID conflicts are impossible (005 uses fixed IDs, 003 generates its own). Data volume is additive; no functional clash.

## Dependencies

- None. STORY-080 can land first and is a prerequisite for STORY-082.

## Out of Scope

- Historical CDR / session / audit data — those fill naturally once the simulator runs.
- Seed for analytics baseline — the simulator will generate real traffic; no synthetic analytics needed.
- FE fixtures — they fetch from the API like any other run.
