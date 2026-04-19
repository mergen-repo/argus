# Fix Plan: FIX-104 - Audit Hash Chain Integrity

## Bug Description

`GET /api/v1/audit-logs/verify` returns `verified: false` on an untampered system. Row 501 (`operator.update`) has `prev_hash = 0000000000` despite row 500 having a valid hash, breaking the chain. Row 501's `created_at` precedes row 500's, indicating insertion order diverged from id order. All downstream UAT verify checks (UAT-001/002/003/012) fail because the chain is broken.

## Root Cause

**Three co-existing defects produce the chain break:**

### Defect 1: Non-atomic read-then-write in ProcessEntry (RACE)

**File:** `internal/audit/service.go:89-129`

`ProcessEntry` reads the chain tail (`GetLastHash`) and then inserts the new row (`Create`) as **two separate operations** — no database transaction wraps them. The in-process `sync.Mutex` per tenant (service.go:84-87) only synchronizes within a single Go process. Under blue-green deploys, NATS `QueueSubscribe` with queue group `"audit-writers"` (main.go:467) allows any instance to consume audit events, so two instances can race on the chain tail. Even within a single process, the async NATS dispatch path (`CreateEntry` → `PublishAuditEvent` → consumer picks up later) can reorder writes.

### Defect 2: Seed SQL bypasses the canonical Go writer (BYPASS)

**File:** `migrations/seed/003_comprehensive_seed.sql:968-1022`

The seed script directly inserts 250 rows into `audit_logs` with its own hash chain computation. It uses a **single global `prev_h` variable** while alternating between two different tenant IDs (lines 986-991). This means the chain within each tenant is already broken at seed time. Additionally, the seed's hash input format differs from Go's `ComputeHash`:

- **Seed SQL:** `sha256(prev_h || '|' || t_id || '|' || u_id || '|' || act || '|' || ent_type || '|' || ent_id || '|' || ts::text)`
- **Go code:** `SHA256(tenant_id|user_id|action|entity_type|entity_id|created_at_RFC3339Nano|prev_hash)`

Field order differs (seed puts `prev_h` first, Go puts it last). Timestamp format differs (`ts::text` PostgreSQL format vs RFC3339Nano). These mismatches mean even if the chain order were correct, `VerifyChain` would fail because recomputed hashes wouldn't match.

### Defect 3: Chain scope ambiguity — per-tenant vs global

**Files:** `internal/store/audit.go:59-73` (GetLastHash), `internal/audit/service.go:94` (caller)

`GetLastHash` filters by `tenant_id`, making the chain per-tenant. But the spec evidence shows a single chain crossing tenant boundaries (rows with `sim.*`, `operator.*`, `tenant.*` actions in a single sequence ordered by `id`). The `EmitSystemEvent` handler (audit handler.go:317) uses `uuid.Nil` for tenant_id, creating an orphan chain segment that the per-tenant verify can never reach.

**Decision:** The chain MUST be **global by `id` order** (not per-tenant). Rationale:
- The `id` column is a monotonic BIGSERIAL — it's the natural chain ordering key.
- Per-tenant chains allow cross-tenant audit log deletion to go undetected.
- The verify endpoint should validate the entire audit trail's integrity, not just one tenant's view.
- System events (`uuid.Nil` tenant) must be part of the same chain.
- The spec AC-2 and AC-4 describe a single global chain.

### Summary

Row 501 gets `prev_hash = Genesis (0000...)` because:
1. It was the first `operator.update` for a particular tenant context, and `GetLastHash` (scoped by tenant) found no prior rows for that tenant → returned `GenesisHash`.
2. The in-process mutex prevented in-process races but not cross-instance or async races.
3. Meanwhile row 500 (a different action) was written under a different tenant context and got the correct chain tail because the chain was per-tenant isolated.

## Affected Files

| File | Change | Reason |
|------|--------|--------|
| `internal/store/audit.go` | Major rewrite | Remove tenant filter from `GetLastHash`; add transactional `SELECT...FOR UPDATE` + INSERT; add `GetTotalCount` method |
| `internal/audit/service.go` | Major rewrite | Remove in-process `sync.Mutex` + `sync.Map`; use DB-level locking via new transactional store method; simplify `ProcessEntry` |
| `internal/audit/audit.go` | Minor edit | Add `TotalRows` to `VerifyResult`; update `VerifyChain` to also verify first entry's `PrevHash == GenesisHash` |
| `internal/api/audit/handler.go` | Minor edit | Update `verifyResponse` to include `total_rows` field per AC-5 |
| `internal/audit/audit_test.go` | Major additions | Add concurrency test (AC-6), tamper detection test (AC-5), first-entry genesis verification |
| `internal/store/audit_test.go` | Additions | Add integration-style tests for transactional chain write |
| `internal/audit/httpaudit.go` | No change | Already routes through canonical `Auditor.CreateEntry` — verified clean |
| `migrations/seed/003_comprehensive_seed.sql` | Rewrite audit section | Fix hash computation to match Go format, use global chain (no tenant alternation) |
| `migrations/NNNNNNNNNNNNNN_fix_audit_hashchain.up.sql` | New migration | Add `BEFORE INSERT` trigger enforcing chain integrity |
| `migrations/NNNNNNNNNNNNNN_fix_audit_hashchain.down.sql` | New migration | Drop the trigger |
| `cmd/argus/main.go` | No change | Wiring is correct — single `auditSvc` instance, single consumer |

## Fix Steps (Tasks)

### Task 1: Make AuditStore.Create transactional with chain tail lock

- **Files:** `internal/store/audit.go`
- **Depends on:** none
- **Complexity:** high
- **Pattern ref:** `internal/store/ip_store.go` (uses `FOR UPDATE SKIP LOCKED` pattern in `AllocateIP`)
- **Context refs:** Defect 1, AC-1, AC-2
- **What:**
  1. Add a new method `CreateWithChain(ctx context.Context, entry *audit.Entry) (*audit.Entry, error)` that:
     - Begins a transaction
     - Acquires an advisory lock: `SELECT pg_advisory_xact_lock(hashtext('audit_chain_lock')::bigint)` — this is cheaper than `FOR UPDATE` on a partitioned table and avoids partition-scanning issues
     - Reads chain tail: `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1` (NO tenant filter — global chain)
     - If no rows, uses `GenesisHash`
     - Sets `entry.PrevHash = tailHash`
     - Computes `entry.Hash = ComputeHash(*entry, tailHash)` (import the audit package)
     - Inserts the row via existing `INSERT INTO audit_logs...`
     - Commits
  2. Delete `GetLastHash` entirely — it is superseded by the in-transaction tail read inside `CreateWithChain`. No callers will remain after Task 2.
  3. Add `GetAll(ctx context.Context) ([]Entry, error)` for full-chain verification and `GetTotalCount(ctx context.Context) (int64, error)` for the verify endpoint's `total_rows` field
  4. Ensure the `ORDER BY id DESC` query works efficiently — add index `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_id_desc ON audit_logs (id DESC)` (note: on partitioned table, each partition gets its own local index)
- **Verify:** `go build ./internal/store/...`

### Task 2: Simplify FullService.ProcessEntry to delegate chain logic to store

- **Files:** `internal/audit/service.go`, `internal/audit/audit.go` (AuditStore interface)
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** current `ProcessEntry` at service.go:89-129
- **Context refs:** Defect 1, AC-1
- **What:**
  1. Update the `AuditStore` interface in `audit.go`: add `CreateWithChain(ctx context.Context, entry *Entry) (*Entry, error)`, remove `GetLastHash`, add `GetAll`
  2. In `ProcessEntry`:
     - Remove `getTenantMutex` call and `mu.Lock/Unlock`
     - Remove `GetLastHash` call
     - Build the entry with all fields EXCEPT `Hash` and `PrevHash`
     - Call `s.store.CreateWithChain(ctx, entry)` which handles chain atomically
  3. Remove the `tenantMu sync.Map` field from `FullService`
  4. Remove the `getTenantMutex` method
- **Verify:** `go build ./internal/audit/...`

### Task 3: Add BEFORE INSERT trigger for chain integrity guard (AC-3)

- **Files:** new `migrations/NNNNNNNNNNNNNN_fix_audit_hashchain.up.sql`, new `migrations/NNNNNNNNNNNNNN_fix_audit_hashchain.down.sql`
- **Depends on:** none (migration, independent of Go code)
- **Complexity:** medium
- **Context refs:** AC-3
- **What:**
  1. Create `BEFORE INSERT` trigger function `audit_chain_guard()` on `audit_logs`:
     ```sql
     CREATE OR REPLACE FUNCTION audit_chain_guard() RETURNS TRIGGER AS $$
     DECLARE
       tail_hash VARCHAR(64);
     BEGIN
       SELECT hash INTO tail_hash FROM audit_logs ORDER BY id DESC LIMIT 1;
       IF tail_hash IS NULL THEN
         -- First row: prev_hash must be genesis
         IF NEW.prev_hash != '0000000000000000000000000000000000000000000000000000000000000000' THEN
           RAISE EXCEPTION 'audit_chain_violation: first row prev_hash must be genesis';
         END IF;
       ELSE
         IF NEW.prev_hash != tail_hash THEN
           RAISE EXCEPTION 'audit_chain_violation: prev_hash (%) does not match tail hash (%)', NEW.prev_hash, tail_hash;
         END IF;
       END IF;
       RETURN NEW;
     END;
     $$ LANGUAGE plpgsql;
     ```
  2. Attach trigger to parent table: `CREATE TRIGGER trg_audit_chain_guard BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION audit_chain_guard();`
  3. Add the `idx_audit_id_desc` index (if not already created by Task 1 migration)
  4. Down migration: `DROP TRIGGER` + `DROP FUNCTION`
  **Note:** The trigger fires inside the same transaction as the INSERT, so the advisory lock from Task 1 ensures serialization. The trigger is a defense-in-depth safety net.
- **Verify:** `make db-migrate` succeeds on a clean DB

### Task 4: Repair existing chain data (AC-4)

- **Files:** new Go repair command or one-shot migration script
- **Depends on:** Task 1, Task 2, Task 3
- **Complexity:** high
- **Context refs:** AC-4
- **What:**
  1. **Must be a Go program** (not PL/pgSQL) because the hash computation uses Go's `time.RFC3339Nano` format and Go's `ComputeHash` function. A SQL-only repair would produce different hashes than future Go writes.
  2. Implementation options (prefer option A):
     - **Option A:** Add a `RepairChain(ctx context.Context) error` method to `AuditStore` that:
       - Temporarily disables the `trg_audit_chain_guard` trigger: `ALTER TABLE audit_logs DISABLE TRIGGER trg_audit_chain_guard`
       - Reads ALL rows `ORDER BY id ASC` (paginated in batches of 1000)
       - Iterates, recomputing `prev_hash` and `hash` for each row using `audit.ComputeHash`
       - First row gets `PrevHash = GenesisHash`
       - Updates each row's `hash` and `prev_hash` in batches
       - Re-enables the trigger: `ALTER TABLE audit_logs ENABLE TRIGGER trg_audit_chain_guard`
       - Verifies the chain via `VerifyChain`
     - **Option B:** Separate `cmd/repair-audit/main.go` binary invoked once
  3. Add a `make db-repair-audit` Makefile target
  4. Also fix `migrations/seed/003_comprehensive_seed.sql` audit section:
     - Rewrite to use a single global `prev_h` variable with correct Go-compatible hash format
     - Use `to_char(ts, 'YYYY-MM-DD"T"HH24:MI:SS.US000"Z"')` for timestamp formatting
     - Reverse the hash input to match Go's field order: `tenant_id|user_id|action|entity_type|entity_id|created_at|prev_hash`
- **Verify:** After repair, `GET /api/v1/audit-logs/verify?count=10000` returns `verified: true`

### Task 5: Update verify endpoint response shape (AC-5)

- **Files:** `internal/audit/audit.go`, `internal/api/audit/handler.go`
- **Depends on:** Task 1 (for `GetTotalCount`)
- **Complexity:** low
- **Pattern ref:** current `VerifyResult` at audit.go:68-72
- **Context refs:** AC-5
- **What:**
  1. Add `TotalRows int` field to `VerifyResult`: `TotalRows int \`json:"total_rows"\``
  2. Update `verifyResponse` in handler.go to include `TotalRows int \`json:"total_rows"\``
  3. **Change verify to walk the entire chain**, not a windowed count:
     - Remove the `count` query parameter from the verify endpoint
     - Update `FullService.VerifyChain` to call a new `GetAll(ctx) ([]Entry, error)` store method that reads ALL rows `ORDER BY id ASC` (paginated internally in batches of 5000)
     - Set `result.TotalRows = len(entries)`
     - This matches AC-5: "walks the chain row-by-row" and enables proper genesis check
  4. Verify endpoint becomes **super_admin-only** with global scope (no tenant filter) — the global chain decision in Root Cause requires this. Update the route registration to use the super_admin middleware guard.
  5. Add genesis check to `VerifyChain`: the first entry (index 0) must have `PrevHash == GenesisHash`. Currently the loop starts at `i=1` and skips this check.
  6. Delete the `GetLastHash` method entirely — after Task 2, no caller remains. Keeping a tenant_id parameter that silently does nothing is a maintenance trap.
- **Verify:** `go build ./internal/api/audit/...` + manual API test

### Task 6: Add comprehensive tests (AC-5, AC-6)

- **Files:** `internal/audit/audit_test.go`, `internal/store/audit_test.go`
- **Depends on:** Task 1, Task 2, Task 5
- **Complexity:** medium
- **Context refs:** AC-5 (tamper detection test), AC-6 (concurrency regression test)
- **What:**
  1. **Tamper detection test** (AC-5): Insert entries via canonical writer, then manually UPDATE one row's `after_data` column, call `VerifyChain`, assert `first_invalid` points to the tampered row
  2. **Concurrency test** (AC-6): Launch 10 goroutines, each writing 10 events with different action types (`sim.*`, `operator.*`, `tenant.*`, `policy.*`, `user.*`), then verify chain remains valid. **Must run against real Postgres** (testcontainers or the existing integration harness — see `internal/store/tracer_slow_test.go` for existing pattern). Pure-mock tests do NOT exercise the advisory lock and would pass trivially because `mockAuditStore` self-serializes via its own `sync.Mutex`
  3. **Bypass prevention test** (AC-3): Attempt to insert a row directly via SQL with wrong `prev_hash`, assert the trigger rejects it
  4. **First entry genesis test**: Verify first row has `PrevHash == GenesisHash`
  5. **System event test**: Verify `EmitSystemEvent` (uuid.Nil tenant) integrates into the global chain correctly
  6. Update `mockAuditStore` to implement `CreateWithChain` interface method
- **Verify:** `go test ./internal/audit/... ./internal/store/... -v -count=1`

### Task 7: Gate scout — enumerate all audit_logs writers (AC-7)

- **Files:** documentation / code review
- **Depends on:** Task 2
- **Complexity:** low
- **Context refs:** AC-7
- **What:** Verify every path that writes to `audit_logs` routes through the canonical writer. Known writers to verify:
  1. `internal/store/audit.go:Create` — canonical store method (now wrapped by `CreateWithChain`)
  2. `migrations/seed/003_comprehensive_seed.sql:968-1022` — seed SQL (to be fixed in Task 4)
  3. `internal/audit/service.go:ProcessEntry` — the canonical Go path
  4. `internal/audit/httpaudit.go:Emit` → `CreateEntry` → `ProcessEntry` ✓
  5. `internal/api/operator/handler.go:1590` → `CreateEntry` ✓
  6. `internal/api/policy/handler.go:789` → `CreateEntry` ✓
  7. `internal/api/tenant/handler.go:368` → `CreateEntry` ✓
  8. `internal/api/session/handler.go:540` → `CreateEntry` ✓
  9. `internal/api/esim/handler.go:734` → `CreateEntry` ✓
  10. `internal/api/roaming/handler.go:521` → `CreateEntry` ✓
  11. `internal/api/admin/maintenance_window.go:141,180` → `CreateEntry` ✓
  12. `internal/api/admin/sessions_global.go:203` → `CreateEntry` ✓
  13. `internal/api/undo/handler.go:87` → `CreateEntry` ✓
  14. `internal/api/audit/handler.go:325` (EmitSystemEvent) → `ProcessEntry` ✓
  15. `internal/auth/key_rotation.go:38` → `CreateEntry` ✓
  16. `internal/compliance/service.go:335` → `CreateEntry` ✓
  17. `internal/api/sim/compare.go:217` → `CreateEntry` ✓
  18. `cmd/argus/main.go:2201` → `CreateEntry` ✓
  19. `internal/api/compliance/data_portability.go:90` → `audit.Emit` → `CreateEntry` ✓
  20. `internal/api/reports/handler.go` (multiple) → `audit.Emit` → `CreateEntry` ✓

  All Go paths route through `CreateEntry` → `ProcessEntry` → `store.CreateWithChain`. No direct SQL INSERT exists in Go code. Only the seed SQL is a bypass (fixed in Task 4).
- **Verify:** `grep -r 'INSERT INTO audit_logs' --include='*.go' --include='*.sql' .` returns only the canonical store method and the fixed seed

## Acceptance Criteria Mapping

| AC | Task(s) | How Verified |
|----|---------|-------------|
| AC-1: Single-writer invariant | Task 1, Task 2, Task 7 | All `audit_logs` inserts go through `AuditStore.CreateWithChain`; gate scout confirms no bypass |
| AC-2: Transactional tail read | Task 1 | `pg_advisory_xact_lock` + `SELECT hash ORDER BY id DESC LIMIT 1` inside the insert tx |
| AC-3: DB constraint | Task 3 | `BEFORE INSERT` trigger rejects wrong `prev_hash`; unit test confirms bypass attempt fails |
| AC-4: Repair migration | Task 4 | Go-based repair recomputes all hashes in id order; verify endpoint returns `verified: true` |
| AC-5: Verify endpoint accuracy | Task 5, Task 6 | Response includes `{verified, first_invalid, total_rows}`; tamper test confirms detection |
| AC-6: Regression test | Task 6 | 10-goroutine concurrent write test asserts chain validity throughout |
| AC-7: Gate scout | Task 7 | Exhaustive enumeration of all 20 callers; all route through canonical writer |

## Regression Risk

**Medium**

- **Verify endpoint scope change**: Moving from per-tenant to global chain changes the verify endpoint's semantics. Any consumer expecting per-tenant verify results will see different data. Mitigation: the verify endpoint currently has no known external consumers beyond UAT.
- **Advisory lock contention**: Under high audit write throughput, `pg_advisory_xact_lock` serializes ALL writes globally. This is acceptable because audit writes are not on the AAA hot path (they go through NATS async). If throughput becomes an issue, the lock key could be sharded by tenant_id — but that would reintroduce per-tenant chains.
- **Seed data change**: Existing dev databases with seeded data will have broken chains until `make db-repair-audit` is run. Fresh `make db-seed` will produce valid chains.
- **Trigger on partitioned table**: The `BEFORE INSERT` trigger must be created on the parent `audit_logs` table and will automatically apply to all partitions. Tested on PG 16 — this works correctly.
- **Performance**: The `ORDER BY id DESC LIMIT 1` query needs the new `idx_audit_id_desc` index. Without it, PG may scan multiple partitions. With the index, it's a single index scan on the latest partition.

## Task Dependency Graph

```
Task 1 (Store transactional write)
  └─► Task 2 (Service simplification)
        └─► Task 6 (Tests)
        └─► Task 7 (Gate scout)
Task 3 (DB trigger) ──────────────┐
Task 1 ────────────────────────────┤
Task 2 ────────────────────────────┤
                                   └─► Task 4 (Repair migration)
Task 1 ──► Task 5 (Verify endpoint)
           └─► Task 6 (Tests)
```

## Implementation Order

1. Task 1 + Task 3 (parallel — Go store + SQL migration)
2. Task 2 (depends on Task 1)
3. Task 5 (depends on Task 1)
4. Task 4 (depends on Task 1, 2, 3)
5. Task 6 (depends on Task 1, 2, 5)
6. Task 7 (depends on Task 2 — verification pass)
