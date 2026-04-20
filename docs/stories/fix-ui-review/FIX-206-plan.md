# Implementation Plan: FIX-206 — Orphan Operator IDs Cleanup + FK Constraints + Seed Fix

## Goal
Restore referential integrity for SIM → Operator / APN / IP Address relationships by (1) fixing the seed-008 operator-UUID typo that produced 200 orphan SIMs, (2) reconciling any existing orphans via suspend+audit, and (3) adding FK constraints online-safely to prevent future orphans. Ships as a single commit that passes a fresh-volume `make db-migrate && make db-seed` smoke test.

## Problem Context (Verified Root Cause)

- `sims` table: 200 rows point to `operator_id = 00000000-0000-0000-0000-000000000101/102/103` — these UUIDs do NOT exist in `operators`.
- Seed `008_scale_sims.sql` inserts 6 batches (40+40+20+40+40+20 = 200 SIMs) hardcoding operator UUIDs `00000000-...-101/102/103`.
- Seed `005_multi_operator_seed.sql` actually creates operators at UUIDs `20000000-0000-0000-0000-000000000001/002/003` (Turkcell / Vodafone TR / Türk Telekom).
- **This is the complete root cause**: seed 008 has a hardcoded-UUID typo. The APN UUIDs in seed 008 (`00000000-...-301/302/303/311/312/313`) ARE correct and match seed 005.
- No FK constraint exists on `sims.operator_id`, `sims.apn_id`, or `sims.ip_address_id` — the typo went undetected.

Cascade: enriched DTOs (FIX-202) LEFT JOIN operators → NULL `operator_name` → UI renders raw UUIDs (F-14 / F-22 / F-63 / F-81 / F-83 / F-93).

## Architecture Context

### Components Involved

- **`sims` table** (`internal/store/sim.go`, migrations/20260320000002_core_schema.up.sql:275-300): LIST-partitioned by `operator_id`, composite PK `(id, operator_id)`. Partitions: `sims_default` (catch-all) plus per-operator partitions created in seed 005 lines 86-xxx.
- **Migration pipeline**: `argus migrate up` (via `make db-migrate`) applies files in `migrations/*.sql` lexically; `argus seed <file>` (via `make db-seed`) runs `migrations/seed/*.sql` alphabetically.
- **Seed files**: 001_admin_user → 002_system_data → 003_comprehensive_seed → 004_notification_templates → 005_multi_operator_seed → 005a_full_pool_inventory → 006_reserve_sim_ips → 007_sim_history_seed → 008_scale_sims.
- **Store / handler**: `internal/store/sim.go` (Create method at line 149; NO general Update method — only state-transition helpers Activate/Suspend/Resume/Terminate/etc.); `internal/api/sim/handler.go` Create at line 258 (lines 313-331 already validate operator existence via `operatorStore.GetByID`).

### Data Flow — Migration Sequence

```
Fresh volume flow (make infra-up && make db-migrate && make db-seed):
  1. argus migrate up → applies ALL migrations including:
     - 20260320000002_core_schema.up.sql (creates sims, no FK)
     - [NEW] 20260420000001_sims_orphan_cleanup.up.sql (Migration A)
     - [NEW] 20260420000002_sims_fk_constraints.up.sql (Migration B)
  2. argus seed (alphabetical):
     - 001..005 → operators, apns, tenants, ip_pools
     - 005a → ip_addresses inventory
     - 006 → sim IP reservations
     - 007 → sim state history
     - 008 → [FIXED] 200 SIMs with correct operator UUIDs (20000000-...-001/002/003)
     - All inserts now reference existing operator/APN rows → FK passes

Existing-volume flow (production deploy on dirty DB):
  1. Migration A runs FIRST: orphan cleanup
     - Create sentinel "unknown" operator if absent (00000000-0000-0000-0000-000000000999)
     - UPDATE sims → state='suspended', suspended_at=NOW() for orphan-operator rows
     - audit_logs row per suspended SIM (batched via INSERT … SELECT)
     - sims.apn_id: SET NULL for orphan apn_id (nullable column)
     - sims.ip_address_id: SET NULL for orphan ip_address_id (nullable column)
  2. Migration B runs SECOND: FK add
     - ALTER TABLE sims ADD CONSTRAINT fk_sims_operator ... NOT VALID
     - ALTER TABLE sims VALIDATE CONSTRAINT fk_sims_operator  (online)
     - Same for apn_id, ip_address_id
```

**CRITICAL ORDERING**: Migration B MUST NOT run before Migration A. If Migration B runs on dirty data, `VALIDATE CONSTRAINT` fails. Both migrations land in a single commit.

### FK Direction Analysis (PAT-004 compliance)

Per advisor guidance and STORY-086/DEV-169 precedent:

| FK direction | Table pair | Pattern | Why |
|---|---|---|---|
| FROM non-partitioned INTO non-partitioned | `apns.operator_id → operators(id)` | Standard FK (already exists) | ok |
| FROM partitioned INTO non-partitioned | **`sims.operator_id → operators(id)` (NEW)** | **Standard FK** | `operators` is non-partitioned with simple PK `id` — this direction works |
| FROM partitioned INTO non-partitioned | **`sims.apn_id → apns(id)` (NEW)** | **Standard FK** | `apns` non-partitioned |
| FROM partitioned INTO non-partitioned | **`sims.ip_address_id → ip_addresses(id)` (NEW)** | **Standard FK** | `ip_addresses` non-partitioned |
| FROM non-partitioned INTO partitioned | `esim_profiles.sim_id`, `ip_addresses.sim_id`, `ota_commands.sim_id`, `sms_outbound.sim_id` | BEFORE INSERT/UPDATE trigger (`check_sim_exists`) | Postgres REJECTS `REFERENCES sims(id)` because `id` alone is not unique under composite PK (`id, operator_id`) |

**This story adds FKs only in the "FROM partitioned INTO non-partitioned" direction — all three are standard FK. The `check_sim_exists` trigger pattern is NOT needed here.** Dev MUST NOT invent an alternative; cite DEV-169 / STORY-086 precedent.

### Per-Table Orphan Cleanup Enumeration (AC-1)

Every table with `operator_id`, `apn_id`, or `ip_address_id` column — explicit disposition per table. Only `sims` gets new FK constraints; everything else is data-repair disposition only.

| Table | Column(s) | Nullable? | Disposition in Migration A | Rationale |
|---|---|---|---|---|
| `sims` | `operator_id` NOT NULL | no | `UPDATE sims SET state='suspended', suspended_at=NOW() WHERE operator_id NOT IN (SELECT id FROM operators); INSERT INTO audit_logs(...) for each` | Cannot NULL (NOT NULL + partition key); suspend preserves row for manual review per AC-1. Sentinel operator NOT used — preferred path is seed-fix (200 orphans disappear on fresh volume). For existing dirty DBs, suspend is non-destructive. |
| `sims` | `apn_id` nullable | yes | `UPDATE sims SET apn_id = NULL WHERE apn_id IS NOT NULL AND apn_id NOT IN (SELECT id FROM apns)` | SET NULL is safe (column is nullable); row retains identity |
| `sims` | `ip_address_id` nullable | yes | `UPDATE sims SET ip_address_id = NULL WHERE ip_address_id IS NOT NULL AND ip_address_id NOT IN (SELECT id FROM ip_addresses)` | Same as apn_id |
| `apns` | `operator_id` NOT NULL | no | **NO-OP** — `apns.operator_id` already has FK (core_schema:190). If any orphans exist, they'd have failed insert time. | Already enforced |
| `esim_profiles` | `operator_id` NOT NULL | no | **NO-OP** — already has FK (core_schema:259) | Already enforced |
| `msisdn_pool` | `operator_id` NOT NULL | no | **NO-OP** — already has FK (core_schema:575) | Already enforced |
| `operator_grants` | `operator_id` NOT NULL | no | **NO-OP** — already has FK (core_schema:129) | Already enforced |
| `roaming_agreements` | `operator_id` NOT NULL | no | **NO-OP** — already has FK (20260414000001:4) | Already enforced |
| `sla_reports` | `operator_id` nullable | yes | **NO-OP** — already has FK (20260412000001:4) | Already enforced |
| `sessions` | `operator_id` NOT NULL, `apn_id` nullable | no/yes | **SCOPE-CUT** — hypertable, historical data, no FK add in this story. `sessions.operator_id` is `NOT NULL` so SET NULL impossible; creating sentinel operator introduces cross-tab reconciliation burden outside FIX-206 scope. | Document in ROUTEMAP Tech Debt as D-062 target-future-story |
| `cdrs` | `operator_id` NOT NULL, `apn_id` nullable | no/yes | **SCOPE-CUT** — same as sessions; billing data must not be mutated. | Document as D-063 |
| `operator_health_logs` | `operator_id` NOT NULL | no | **SCOPE-CUT** — hypertable, operator deletion already blocked by `operators` row lifecycle; orphans would require deleting an operator with live health logs (operationally rare). | Document as D-064 |
| `audit_logs` | no direct FK to operators/apns | n/a | **NO-OP** — immutable; Migration A WRITES new audit rows here but never modifies existing | Hash-chain integrity |
| `policy_versions` | no operator_id column | n/a | **NO-OP** | n/a |
| `ip_pools` | `apn_id` NOT NULL | no | **NO-OP** — already has FK (core_schema:214) | Already enforced |
| `ip_addresses` | `pool_id` NOT NULL | no | **NO-OP** — already has FK (core_schema:235) | Already enforced |

**In-scope cleanup for Migration A**: `sims.operator_id` (suspend), `sims.apn_id` (SET NULL), `sims.ip_address_id` (SET NULL). Everything else is NO-OP or SCOPE-CUT.

### API Specifications

No new endpoints. AC-7 covers error-surfacing for existing endpoints.

- `POST /api/v1/sims` — Create SIM (existing, `internal/api/sim/handler.go:258`)
  - Existing validation (lines 313-331) already calls `operatorStore.GetByID` and `apnStore.GetByID` → returns 404 if not found; converted to `CodeNotFound`.
  - **NEW behavior after FK add**: if a race condition or tenant-isolation bypass somehow constructs a valid-looking UUID that is not in `operators`, DB FK will reject → Postgres error propagates via `scanSIM` error path. Handler must translate PG FK violation (`SQLSTATE 23503`) into HTTP 400 with `CodeInvalidFormat` + field hint.
  - Success envelope: `{ status: "success", data: {...sim...} }` (unchanged)
  - New error envelope: `{ status: "error", error: { code: "INVALID_REFERENCE", message: "operator_id does not reference an existing operator" } }` → status 400
- `POST /api/v1/sims/bulk/operator-switch` — Bulk operator change (FIX-201 already exists)
  - FIX-201 gate validated tenant-isolation target-operator check. Re-verified; no new code in this story.

**AC-7 decision (per advisor Option A)**: Two-layer defense = handler lookup (already present) + DB FK (new). `SIMStore.Create` does NOT get new code. Instead, add an error-translation helper in `internal/store/sim.go` that recognizes PG SQLSTATE 23503 and returns a typed `ErrInvalidReference` error; handler maps to 400.

### Database Schema — Changes

Source: migrations/20260320000002_core_schema.up.sql lines 275-311 (ACTUAL sims DDL, unchanged)

```sql
-- Existing sims table (unchanged by this story):
CREATE TABLE sims (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,         -- NEW FK added in Migration B
    apn_id UUID,                        -- nullable; NEW FK added in Migration B
    iccid VARCHAR(22) NOT NULL,
    imsi VARCHAR(15) NOT NULL,
    msisdn VARCHAR(20),
    ip_address_id UUID,                 -- nullable; NEW FK added in Migration B
    policy_version_id UUID,
    esim_profile_id UUID,
    sim_type VARCHAR(10) NOT NULL DEFAULT 'physical',
    state VARCHAR(20) NOT NULL DEFAULT 'ordered',
    ... (24 columns total)
    PRIMARY KEY (id, operator_id)
) PARTITION BY LIST (operator_id);
```

#### Migration A — `migrations/20260420000001_sims_orphan_cleanup.up.sql`

```sql
-- FIX-206 Migration A: orphan cleanup (data repair, must precede FK add).
-- Idempotent: running twice is a no-op.
-- Non-destructive: orphan SIMs are suspended, not deleted. apn_id / ip_address_id are NULLed.

BEGIN;

-- 1. Capture counts for summary (NOTICE at end)
DO $$
DECLARE
    orphan_operator_count INTEGER;
    orphan_apn_count      INTEGER;
    orphan_ip_count       INTEGER;
    suspended_count       INTEGER;
BEGIN
    SELECT COUNT(*) INTO orphan_operator_count
      FROM sims s WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id);
    SELECT COUNT(*) INTO orphan_apn_count
      FROM sims s WHERE s.apn_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM apns a WHERE a.id = s.apn_id);
    SELECT COUNT(*) INTO orphan_ip_count
      FROM sims s WHERE s.ip_address_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ip_addresses ia WHERE ia.id = s.ip_address_id);

    RAISE NOTICE 'FIX-206: % orphan operator_id, % orphan apn_id, % orphan ip_address_id',
        orphan_operator_count, orphan_apn_count, orphan_ip_count;
END $$;

-- 2. Audit trail: record each soon-to-be-suspended sim in audit_logs before mutation.
--    Uses hash = sha256(prev_hash || tenant_id || sim_id || action) via trigger/app; this SQL
--    produces a placeholder hash chain — Dev task: confirm hash-chain compatibility. If hash-chain
--    update requires app-layer (audit.Service.Append), expose a maintenance endpoint instead and
--    reference-call from post-migration smoke. Default here: direct INSERT with computed hashes.
--
-- Option chosen (advisor-aligned): direct SQL INSERT mirroring audit_logs schema; hash computed
-- as encode(sha256(prev_hash || ...), 'hex'). Prev_hash lookup = latest audit_logs row per tenant.
-- If ANY orphan SIM exists, one audit row per SIM with action='fix206.suspend_orphan'.

INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id,
                        before_data, after_data, diff, correlation_id, hash, prev_hash, created_at)
SELECT
    s.tenant_id,
    NULL::uuid,                                          -- system action
    'fix206.suspend_orphan',
    'sim',
    s.id::text,
    jsonb_build_object('state', s.state, 'operator_id', s.operator_id),
    jsonb_build_object('state', 'suspended', 'reason', 'orphan_operator_id'),
    jsonb_build_object('op', 'update', 'fields', ARRAY['state','suspended_at']),
    gen_random_uuid(),
    encode(sha256(convert_to(s.id::text || ':fix206', 'UTF8')), 'hex'),  -- placeholder hash
    repeat('0', 64),                                      -- placeholder prev_hash
    NOW()
FROM sims s
WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id)
  AND s.state != 'suspended';  -- idempotency: skip already-handled

-- 3. Suspend orphan-operator SIMs (non-destructive; preserves row for manual review)
UPDATE sims
   SET state = 'suspended',
       suspended_at = COALESCE(suspended_at, NOW()),
       updated_at  = NOW()
 WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = sims.operator_id)
   AND state != 'suspended';  -- idempotency

-- 4. NULL out orphan apn_id (nullable column; safe)
UPDATE sims
   SET apn_id = NULL,
       updated_at = NOW()
 WHERE apn_id IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM apns a WHERE a.id = sims.apn_id);

-- 5. NULL out orphan ip_address_id (nullable column; safe)
UPDATE sims
   SET ip_address_id = NULL,
       updated_at = NOW()
 WHERE ip_address_id IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM ip_addresses ia WHERE ia.id = sims.ip_address_id);

-- 6. Final summary NOTICE
DO $$
DECLARE
    remaining INTEGER;
BEGIN
    SELECT COUNT(*) INTO remaining
      FROM sims s WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id);
    IF remaining > 0 THEN
        RAISE EXCEPTION 'FIX-206 Migration A: % orphan SIMs remain after cleanup — cannot proceed to Migration B', remaining;
    END IF;
END $$;

COMMIT;
```

#### Migration A down — `migrations/20260420000001_sims_orphan_cleanup.down.sql`

```sql
-- Reverse Migration A is intentionally a no-op for suspended rows (data repair is one-way).
-- Rollback strategy: restore from pg_dump + remove FK via Migration B down. Suspended SIMs
-- remain suspended — manual operator triage is expected. Audit entries are immutable.
-- This down file exists to satisfy the migration tooling contract.

-- no-op
SELECT 1;
```

#### Migration B — `migrations/20260420000002_sims_fk_constraints.up.sql`

```sql
-- FIX-206 Migration B: add FK constraints to sims after Migration A cleared all orphans.
-- Uses NOT VALID + VALIDATE pattern to avoid full table lock on 10M-row production tables.
-- Postgres handles cross-partition VALIDATE correctly (validates each partition sequentially).

BEGIN;

-- 1. sims.operator_id → operators(id)
ALTER TABLE sims
    ADD CONSTRAINT fk_sims_operator
    FOREIGN KEY (operator_id) REFERENCES operators(id) ON DELETE RESTRICT NOT VALID;

-- 2. sims.apn_id → apns(id)  (nullable — NULL values pass FK)
ALTER TABLE sims
    ADD CONSTRAINT fk_sims_apn
    FOREIGN KEY (apn_id) REFERENCES apns(id) ON DELETE SET NULL NOT VALID;

-- 3. sims.ip_address_id → ip_addresses(id)  (nullable)
ALTER TABLE sims
    ADD CONSTRAINT fk_sims_ip_address
    FOREIGN KEY (ip_address_id) REFERENCES ip_addresses(id) ON DELETE SET NULL NOT VALID;

COMMIT;

-- VALIDATE takes a SHARE UPDATE EXCLUSIVE lock per partition (concurrent writes OK).
-- Run outside a transaction block to avoid a single long-running tx.
ALTER TABLE sims VALIDATE CONSTRAINT fk_sims_operator;
ALTER TABLE sims VALIDATE CONSTRAINT fk_sims_apn;
ALTER TABLE sims VALIDATE CONSTRAINT fk_sims_ip_address;
```

**ON DELETE choices**:
- `operator_id`: RESTRICT — prevents accidental operator delete; intentional deletes require SIM reassignment first (AC-2).
- `apn_id`: SET NULL — APN deletion is a legitimate admin action; SIMs fall back to default APN at next session.
- `ip_address_id`: SET NULL — releasing an IP address should not block; the SIM can be re-allocated.

#### Migration B down — `migrations/20260420000002_sims_fk_constraints.down.sql`

```sql
BEGIN;
ALTER TABLE sims DROP CONSTRAINT IF EXISTS fk_sims_ip_address;
ALTER TABLE sims DROP CONSTRAINT IF EXISTS fk_sims_apn;
ALTER TABLE sims DROP CONSTRAINT IF EXISTS fk_sims_operator;
COMMIT;
```

### Seed Fix — `migrations/seed/008_scale_sims.sql`

**The Fix**: replace all 6 occurrences of `00000000-0000-0000-0000-000000000101/102/103` with the seed-005 canonical UUIDs:

| Current (WRONG) | Replacement (CORRECT) | Operator |
|---|---|---|
| `00000000-0000-0000-0000-000000000101` | `20000000-0000-0000-0000-000000000001` | Turkcell |
| `00000000-0000-0000-0000-000000000102` | `20000000-0000-0000-0000-000000000002` | Vodafone TR |
| `00000000-0000-0000-0000-000000000103` | `20000000-0000-0000-0000-000000000003` | Türk Telekom |

APN UUIDs (`00000000-...-301/302/303/311/312/313`) are correct (seed 005 creates them) — DO NOT change.

**APN-operator alignment check**: seed 005 creates:
- APNs `...301/302/303` on operator `20000000-...-001` (Turkcell) for tenant XYZ
- APNs `...311/312/313` on operator `20000000-...-002` (Vodafone) for tenant ABC

Seed 008 uses `...301` with Turkcell (correct) but also uses `...302` / `...303` with Vodafone / Türk Telekom — **cross-check**: is APN `302` defined for operator Vodafone? Dev MUST verify by reading seed 005 APN inserts. If APN-operator mismatch exists, dev MUST create additional missing APN rows in seed 005 or adjust seed 008 APN UUIDs. Non-negotiable: all inserted SIM rows MUST satisfy the new FK on `apn_id → apns(id)` AND the logical constraint that `apn.operator_id == sim.operator_id` (this is a tenancy invariant, not a DB FK, but the schema will reject if APN is orphan).

### Pending Pre-Session Seed Diffs

`git status` shows unstaged edits to `migrations/seed/001_admin_user.sql`, `002_system_data.sql`, `003_comprehensive_seed.sql`, `005_multi_operator_seed.sql`, and `005a_full_pool_inventory.sql`. Inspection:

- **001**: adds `BEGIN/COMMIT` + replaces `ON CONFLICT (domain)` with bare `ON CONFLICT` — idempotency hygiene, aligned with FIX-206 goal.
- **002**: same pattern — BEGIN/COMMIT + `ON CONFLICT (code) → ON CONFLICT`.
- **003**: 212-line delta — removes pre-existing DELETE cascades that were incompatible with "additive on fresh volume". Aligned.
- **005**: single-line change `ON CONFLICT (domain) → ON CONFLICT`.
- **005a**: also has pending edits.

**Decision** (advisor-confirmed): all pending seed edits are seed-idempotency prerequisites for FIX-206's fresh-volume smoke (AC-4). They belong in this commit. Dev MUST inspect and stage them together with the 008 operator-UUID fix in Task T-1.

## Prerequisites

- [x] `sims` table exists with composite PK `(id, operator_id)` (confirmed — core_schema:275-300)
- [x] `operators`, `apns`, `ip_addresses` tables exist with non-partitioned PKs (confirmed)
- [x] Seed 005 creates operators at UUIDs `20000000-...-001/002/003` (confirmed, line 49)
- [x] FIX-201 (bulk operator switch) complete — bulk handler already validates tenant-target-operator ownership
- [x] STORY-086 (check_sim_exists trigger for INTO-sims FK direction) complete — no conflict with this story's FROM-sims FK direction

## Task Decomposition

### Task 1: Seed reconciliation and operator-UUID typo fix

- **Files**: Modify `migrations/seed/008_scale_sims.sql` (6 UUID replacements); inspect + stage-as-is `migrations/seed/001_admin_user.sql`, `002_system_data.sql`, `003_comprehensive_seed.sql`, `005_multi_operator_seed.sql`, `005a_full_pool_inventory.sql`.
- **Depends on**: —
- **Complexity**: medium
- **Pattern ref**: Read `migrations/seed/005_multi_operator_seed.sql` lines 49-75 for canonical operator-UUID literals. Read `git diff migrations/seed/` to confirm pending edits are idempotency hygiene, NOT orphan-introducing.
- **Context refs**: "Per-Table Orphan Cleanup Enumeration", "Seed Fix", "Pending Pre-Session Seed Diffs"
- **What**: 
  1. Run `git diff migrations/seed/` — confirm pending edits are BEGIN/COMMIT + `ON CONFLICT` simplifications (idempotency hygiene). If any diff introduces orphan UUIDs or destructive cascades, STOP and escalate.
  2. Edit `008_scale_sims.sql`: replace all 6 occurrences of `00000000-0000-0000-0000-000000000101/102/103` at lines 79, 94, 109, 124, 139, 154 with `20000000-0000-0000-0000-000000000001/002/003` respectively. Use a deterministic mapping (101→001, 102→002, 103→003).
  3. Verify APN-operator alignment: for each `(operator_id, apn_id)` pair in 008, confirm the APN row in seed 005 has matching `operator_id`. If mismatch found (e.g. APN 302 defined on Turkcell but 008 uses it with Vodafone), either ADD the missing APN row in seed 005 OR change the APN UUID in 008 to an existing Vodafone APN. Dev MUST document the chosen resolution in a comment at the top of seed 008.
  4. No new files; only edits.
- **Verify**: 
  - `grep -c "00000000-0000-0000-0000-000000000101\|102\|103" migrations/seed/008_scale_sims.sql` → returns 0
  - `grep -c "20000000-0000-0000-0000-000000000001\|002\|003" migrations/seed/008_scale_sims.sql` → returns 6
  - APN-operator alignment: for each SIM insert block in 008, confirm via `psql` query: `SELECT a.id FROM apns a WHERE a.id = '<apn_id>' AND a.operator_id = '<operator_id>'` returns 1 row after seed 005+008 applied.

### Task 2: Migration A — orphan cleanup (data repair)

- **Files**: Create `migrations/20260420000001_sims_orphan_cleanup.up.sql` and `.down.sql`.
- **Depends on**: Task 1 (seed fix must be in place before Migration A runs against a freshly-seeded DB — otherwise A is a no-op on clean data, which is fine)
- **Complexity**: high
- **Pattern ref**: Read `migrations/20260419000001_fix_audit_hashchain.up.sql` for an example of app-logic SQL migration with audit-log insertion; read `migrations/20260417000004_sms_outbound_recover.up.sql` for BEGIN/COMMIT pattern.
- **Context refs**: "Per-Table Orphan Cleanup Enumeration", "Database Schema — Migration A"
- **What**: Write the up migration exactly as specified in "Database Schema — Migration A" above. Note on audit-log hash chain: the direct-SQL hash is a placeholder — the audit.Service hash-chain is verified by `internal/audit/hashchain.go`. Dev MUST either (a) verify that a standalone audit row with placeholder hashes passes the schema-check migration (20260419000001), or (b) omit the audit_logs insert and replace with a `RAISE NOTICE` line per-SIM during the UPDATE loop. Choose (a) if hashchain verification is per-chain-tail; choose (b) if any hash mismatch breaks startup. This is the single non-trivial decision in Migration A.
  - down.sql: no-op (data repair is one-way; document in file header).
- **Verify**: 
  - `argus migrate up` on a DB with 200 orphan SIMs: post-migration `SELECT COUNT(*) FROM sims WHERE operator_id NOT IN (SELECT id FROM operators)` returns 0.
  - `SELECT COUNT(*) FROM sims WHERE state='suspended'` increases by exactly the pre-migration orphan count.
  - `SELECT COUNT(*) FROM audit_logs WHERE action='fix206.suspend_orphan'` equals the orphan count (or equals 0 if option (b) chosen).
  - Running migration a second time is a no-op (idempotency guards in clauses).

### Task 3: Migration B — FK constraints (online-safe)

- **Files**: Create `migrations/20260420000002_sims_fk_constraints.up.sql` and `.down.sql`.
- **Depends on**: Task 2
- **Complexity**: high
- **Pattern ref**: Read `migrations/20260320000002_core_schema.up.sql` line 181-182 for the `ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY` pattern used on non-partitioned tables. Note no precedent for NOT VALID → VALIDATE split in this repo — this story introduces that pattern.
- **Context refs**: "FK Direction Analysis", "Database Schema — Migration B"
- **What**: Write Migration B exactly as specified. Key points:
  1. Three FK adds: operator_id (RESTRICT), apn_id (SET NULL), ip_address_id (SET NULL).
  2. NOT VALID phase inside BEGIN/COMMIT (fast, ACCESS EXCLUSIVE lock for ~ms).
  3. VALIDATE phase OUTSIDE BEGIN/COMMIT (each statement autocommits; holds SHARE UPDATE EXCLUSIVE per partition, allows concurrent DML).
  4. Migration runner must handle multi-statement files: verify `argus migrate` handles sequential statements in a single .up.sql file (if not, split VALIDATE into a separate migration 20260420000003_*.up.sql).
  5. down.sql: drop all three constraints.
- **Verify**: 
  - `\d sims` in psql shows three new foreign-key constraints.
  - Post-migration attempt to insert a SIM with a random UUID operator_id fails with PG error 23503 (foreign_key_violation).
  - Running migration a second time is idempotent (CREATE idempotency: Postgres rejects duplicate constraint by name — Dev MUST use `DO $$ BEGIN IF NOT EXISTS ... ADD CONSTRAINT ... END $$` wrapper OR rely on `argus migrate` dirty-flag tracking).

### Task 4: Handler error-code surface for FK violations

- **Files**: Modify `internal/api/sim/handler.go` (Create method error-translation block); add new error code if not present in `internal/apierr/apierr.go`.
- **Depends on**: Task 3
- **Complexity**: low
- **Pattern ref**: Read `internal/api/sim/handler.go:345-361` for the existing `errors.Is(err, store.ErrICCIDExists)` pattern — mirror it for a new `store.ErrInvalidReference`.
- **Context refs**: "API Specifications"
- **What**: 
  1. In `internal/store/sim.go`, extend the Create error-branching around `scanSIM`: detect PG SQLSTATE `23503` via `pgx.PgError.Code == "23503"`; return a new `ErrInvalidReference` exported var with wrapped constraint name.
  2. In `internal/api/sim/handler.go:345-361`, add an `errors.Is(err, store.ErrInvalidReference)` branch that emits 400 with code `INVALID_REFERENCE` + field derived from constraint name (`fk_sims_operator` → `operator_id`; etc).
  3. If `apierr.CodeInvalidReference` does not exist, add it to `internal/apierr/apierr.go`.
- **Verify**: 
  - `POST /api/v1/sims` with `operator_id = "99999999-9999-9999-9999-999999999999"` (non-existent) returns 400 + `code: "INVALID_REFERENCE"` + field hint. Note: handler-layer validation (line 313) already catches the same case with 404 `NOT_FOUND`; the FK path is a defensive duplicate invoked only if the handler-layer check races with an operator deletion. Dev MUST preserve the handler-layer 404 as primary path.
  - Run `go vet ./...` clean.

### Task 5: Fresh-volume smoke test + store unit test

- **Files**: Create `internal/store/sim_fk_integration_test.go` (store-level integration test gated on `DATABASE_URL`); modify `internal/store/migration_freshvol_test.go` if present (add assertion).
- **Depends on**: Task 2, Task 3, Task 4, Task 1
- **Complexity**: medium
- **Pattern ref**: Read `internal/store/migration_freshvol_test.go` for the fresh-volume pattern; read `internal/store/ippool_test.go` (modified pre-session) for trigger assertions.
- **Context refs**: "Data Flow", "API Specifications", "Acceptance Criteria Mapping"
- **What**: 
  1. `TestFreshVolume_NoOrphanSims` — spins up DB from scratch (per pattern in migration_freshvol_test), runs all migrations + all seeds, asserts `SELECT COUNT(*) FROM sims s LEFT JOIN operators o ON s.operator_id=o.id WHERE o.id IS NULL` returns 0.
  2. `TestFreshVolume_FKConstraintsInstalled` — asserts `pg_constraint` has three rows named `fk_sims_operator`, `fk_sims_apn`, `fk_sims_ip_address`, each with `convalidated=true`.
  3. `TestSIMCreate_RejectsNonexistentOperator` — calls `SIMStore.Create` with a garbage operator_id; asserts error is `ErrInvalidReference`.
  4. `TestSIMCreate_RejectsNonexistentAPN` — same for apn_id.
- **Verify**: All tests pass with `DATABASE_URL` set + fresh schema. `go test -run TestFreshVolume ./internal/store/...` passes.

### Task 6: Documentation update

- **Files**: Modify `docs/architecture/db/sim-apn.md` (or equivalent TBL-10 sims doc) to document the three new FKs; modify `docs/ROUTEMAP.md` Tech Debt table to add D-062/D-063/D-064 for scope-cut tables (sessions/cdrs/operator_health_logs); modify `docs/brainstorming/decisions.md` to record the "FK direction + suspend-not-delete" decision as DEV-NNN.
- **Depends on**: Task 3
- **Complexity**: low
- **Pattern ref**: Read `docs/architecture/db/sim-apn.md` lines 100-130 for the sims table documentation style.
- **Context refs**: "Per-Table Orphan Cleanup Enumeration", "FK Direction Analysis"
- **What**: 
  1. Add three FK rows to sims TBL-10 doc.
  2. Add D-062, D-063, D-064 entries to Tech Debt table with `Target Story: future data-integrity extension`, Status OPEN.
  3. Append a DEV-NNN decision to decisions.md: "FIX-206 scope: sims FK + seed fix only; sessions/cdrs/op_health_logs hypertable FKs deferred because no non-destructive cleanup exists for NOT NULL columns."
- **Verify**: `grep -c "fk_sims_" docs/architecture/db/sim-apn.md` ≥ 3; new D-062..D-064 entries present; decisions.md tail has new DEV entry.

### Complexity Mapping

Story Effort = M. Task mix:
- 3 tasks medium+ (Task 1 — seed reconciliation; Task 2 — Migration A; Task 5 — integration tests)
- 2 tasks high (Task 2, Task 3 — migration ordering + online-safe FK add)
- 1 task low (Task 4 — error translation)
- 1 task low (Task 6 — docs)

Task 2 and Task 3 are marked **high** because they involve migration ordering, hashchain compatibility decision, NOT VALID/VALIDATE split, and multi-partition cross-partition validation — requires opus-quality rigour.

## Acceptance Criteria Mapping

| AC | Implemented In | Verified By |
|---|---|---|
| AC-1 Data integrity job (reconcile orphan operator_ids — suspend + audit) | Task 2 | Task 5 `TestFreshVolume_NoOrphanSims` + post-migration psql `SELECT` |
| AC-2 FK `sims.operator_id → operators(id) ON DELETE RESTRICT` | Task 3 | Task 5 `TestFreshVolume_FKConstraintsInstalled` |
| AC-3 FK `sims.apn_id → apns(id)`, `sims.ip_address_id → ip_addresses(id)` | Task 3 | Task 5 `TestFreshVolume_FKConstraintsInstalled` |
| AC-4 Seed fresh-volume clean — zero orphans | Task 1, Task 2 | Task 5 `TestFreshVolume_NoOrphanSims` + manual `make infra-up && make db-migrate && make db-seed && psql -c "SELECT COUNT(*) FROM sims s LEFT JOIN operators o ON s.operator_id=o.id WHERE o.id IS NULL"` |
| AC-5 Idempotent + records summary | Task 2 | Migration A second-run = no-op; `RAISE NOTICE` in migration output |
| AC-6 Audit handlers that might create orphans | Already present — handler.go:313-331 | Task 5 store tests + existing FIX-201 bulk tests |
| AC-7 `store.sim.Create` fails fast 400 on invalid operator_id/apn_id | Task 4 | Task 5 `TestSIMCreate_RejectsNonexistentOperator` + `TestSIMCreate_RejectsNonexistentAPN` |
| AC-8 UI regression — SIM list no longer shows orphan UUIDs | Task 1 (seed fix) | Visual smoke: navigate to `/sims`, confirm no `00000000-...` UUID strings in operator column |

## Story-Specific Compliance Rules

- **DB**: Migration A must run before Migration B (enforced by filename lexical order `20260420000001` < `20260420000002`). Migration A must not run standalone against FK-protected data (down.sql is no-op by design — not a support statement for rollback-reapply-rollback cycle).
- **DB — PAT-004 compliance**: Dev MUST use STANDARD FK syntax for all three FKs in Migration B. Dev MUST NOT invent `check_sim_exists`-style triggers for this story — that pattern applies only to the inverse direction (INTO sims from non-partitioned table, which is NOT in FIX-206 scope). Cite DEV-169 / STORY-086 precedent in migration header comment.
- **DB — Online-safe pattern**: Migration B MUST use `ADD CONSTRAINT ... NOT VALID` followed by `VALIDATE CONSTRAINT` in a separate non-transactional statement. Blocking `ALTER TABLE ADD CONSTRAINT` without NOT VALID holds ACCESS EXCLUSIVE for seconds-to-minutes on a 10M-row table and will trip cascading deadlocks with live RADIUS/Diameter traffic.
- **Seed — AC-4 non-negotiable** (per `feedback_no_defer_seed.md` user memory): `make db-seed` on a fresh Docker volume MUST pass clean. NO `--force`. NO skipping failing inserts. If ANY seed still produces orphans after Task 1, fix the seed in this story. If a secondary orphan source is discovered mid-implementation (e.g. eSIM switch runtime writes), add a scope-addition task or escalate.
- **API — error envelope**: Task 4 new error path uses standard envelope. Code `INVALID_REFERENCE` added to ERROR_CODES.md (out-of-scope for this story — documented as D-064 follow-up if CodeInvalidReference constant doesn't exist).
- **ADR-001 (Tenant Isolation)**: FK constraints do not leak tenant data — they only enforce structural integrity. Multi-tenant queries remain scoped via `tenant_id` predicate + app-layer enforcement.

## Bug Pattern Warnings

- **PAT-004 CORRECTION**: The dispatch referenced "PAT-004" for composite-PK FK direction — but `docs/brainstorming/bug-patterns.md` PAT-004 is actually **goroutine cardinality (STORY-090 Gate F-A5)**. The correct precedent for "cannot FK into partitioned sims due to composite PK" is **DEV-169 / STORY-086 (sms_outbound_recover)**. Dev MUST cite DEV-169 in Migration B header comment. This story's FK direction is FROM partitioned INTO non-partitioned (opposite of DEV-169's constraint) — standard FK works.
- **PAT-006 (shared payload struct silently omitted)**: Not applicable — no shared DTO changes.
- **PAT-009 (nullable FK in analytics COALESCE)**: Related but separate — after this FK lands, `sims.apn_id = NULL` is a valid state. Any analytics query that joins on `apn_id` MUST use LEFT JOIN + COALESCE (already handled by FIX-202 DTO resolver).

## Tech Debt (from ROUTEMAP)

- **D-047 (FIX-202 deferred — partition-pruning JOINs)**: Not unblocked by this story. D-047 remains OPEN. This story's FK adds do not change JOIN plans; partition pruning requires `operator_id` in the JOIN predicate.
- **D-051/D-052/D-053/D-054**: Unrelated to FIX-206.
- **New debt items created by this story**: D-062 (sessions hypertable FK deferred), D-063 (cdrs hypertable FK deferred), D-064 (operator_health_logs hypertable FK deferred). Added in Task 6.

## Mock Retirement

No mocks affected. Story is backend-only + seed fix.

## Risks & Mitigations

- **Risk 1 — Production 10M-row lock**: Mitigated by NOT VALID + VALIDATE split (Migration B). VALIDATE takes SHARE UPDATE EXCLUSIVE, concurrent DML continues. Tested empirically in staging before prod cutover.
- **Risk 2 — Seed 008 APN-operator mismatch**: Task 1 step 3 explicitly verifies every `(operator_id, apn_id)` pair. If mismatch found, Dev fixes in-scope (either adds missing APN rows to seed 005 or changes APN UUID in 008).
- **Risk 3 — Hashchain incompatibility in Migration A audit inserts**: Task 2 documents decision branch — if hashchain check is strict, fall back to `RAISE NOTICE` per-SIM instead of audit_logs inserts. Audit trail preserved via migration-run log.
- **Risk 4 — Down-migration data loss**: Migration A down is no-op by design (suspended SIMs cannot be un-suspended programmatically without knowing the original state). Documented in down.sql header. Production rollback path = restore from pre-migration pg_dump.
- **Risk 5 — Multi-statement migration executor**: If `argus migrate` doesn't support VALIDATE statements outside BEGIN/COMMIT, Task 3 has a fallback — split into 20260420000003_sims_fk_validate.up.sql. Dev verifies empirically in Task 3.
- **Risk 6 — Pending pre-session seed edits conflict**: Task 1 step 1 performs `git diff migrations/seed/` inspection as the first action. If any diff introduces orphan-introducing content, escalate to user before proceeding.

## Pre-Validation Checklist

- [x] Min 60 lines (current: ~290 lines — PASS)
- [x] Min 3 tasks (current: 6 tasks — PASS)
- [x] Required sections: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping — PASS
- [x] Per-table enumeration with explicit disposition (NOT "audit all") — PASS (16 tables enumerated in "Per-Table Orphan Cleanup Enumeration")
- [x] Migration ordering: A-cleanup → B-FKs, both in same commit — PASS
- [x] Down migrations specified for A (no-op + justification) and B (drop constraints) — PASS
- [x] Seed fix in THIS story's commit (Task 1) — PASS
- [x] PAT-004 clarification (goroutine cardinality vs. composite PK / DEV-169) — PASS
- [x] Bug Pattern Warnings section cites DEV-169 precedent — PASS
- [x] AC-4 fresh-volume smoke task (Task 5 `TestFreshVolume_NoOrphanSims`) — PASS
- [x] At least 1 high-complexity task (Task 2, Task 3 marked high) — PASS
- [x] Every task has Pattern ref + Context refs + Verify — PASS
- [x] No UI → Design Token Map section skipped — CORRECT (backend-only story)
