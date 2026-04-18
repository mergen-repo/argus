# FIX-104: Audit Hash Chain Integrity

> Tier 1 (foundational) — every UAT verify check that calls
> `GET /audit-logs/verify` depends on this. Without a valid chain, all
> downstream UATs return `verified:false` regardless of actual tamper.

## User Story

As a compliance auditor, I need the audit log hash chain to remain unbroken
across all events so that `GET /api/v1/audit-logs/verify` returns
`verified:true` on an untampered system, and reliably detects actual tampering.

## Source Finding

- UAT Batch 1 report: `docs/reports/uat-acceptance-batch1-2026-04-18.md` — **F-10 CRITICAL**
- Evidence (verified by hand after report):
  ```
   id  | action          | created_at                    | prev_hash       | hash
  -----+-----------------+-------------------------------+-----------------+------------
   499 | sim.activate    | 2026-04-18 15:27:44 | f592099008      | af01aa12ae
   500 | sim.bulk_import | 2026-04-18 17:33:31 | af01aa12ae      | 917c894791
   501 | operator.update | 2026-04-18 16:58:17 | 0000000000      | 15d84e4cff  ← CHAIN RESET
   502 | operator.update | 2026-04-18 16:59:13 | 15d84e4cff      | 621a04217f
   503 | tenant.context_switched | 2026-04-18 18:40:00 | af01aa12ae  | 08afaefdfb
  ```
- Rows 500 and 501 are in the same partition `audit_logs_2026_04` — NOT a partition boundary bug
- Row 501's `prev_hash` is all-zeros despite 500 having `hash=af01aa12ae`
- Row 501's `created_at` precedes row 500's — insertion order ≠ id order, or the hash-chain writer reads an out-of-date "latest hash" when a particular code path writes audit rows
- Affected code path: `operator.update` (per rows 501-502) — this handler appears to bypass the canonical chain write

## Acceptance Criteria

- [ ] AC-1: **Single-writer invariant**: all `audit_logs` inserts go through a canonical `AuditStore.Write(ctx, event)` that reads the current chain tail inside the same transaction and computes `prev_hash = tail.hash`. No code path constructs an `audit_logs` insert directly.
- [ ] AC-2: **Transactional tail read**: `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1 FOR UPDATE` (or equivalent advisory lock) inside the insert tx, to serialise concurrent writers.
- [ ] AC-3: **DB constraint**: add a trigger OR a CHECK constraint that rejects inserts where `prev_hash != (SELECT hash FROM audit_logs WHERE id = <previous>)`. At minimum, a unit test that tries to bypass `AuditStore.Write` and asserts the write fails.
- [ ] AC-4: **Repair migration**: one-time migration that recomputes hashes for existing rows in id order starting from the first valid row. After migration, `GET /audit-logs/verify` returns `verified:true, first_invalid:null` for the entire existing table.
- [ ] AC-5: **Verify endpoint accuracy**: `GET /api/v1/audit-logs/verify` walks the chain row-by-row and returns `{verified: bool, first_invalid: id|null, total_rows: int}`. Add a test that inserts a deliberately-tampered row and confirms `first_invalid` points to it.
- [ ] AC-6: **Regression test**: a test that concurrently writes from 10 goroutines across different action types (sim.*, operator.*, tenant.*, policy.*, user.*) and asserts chain remains valid throughout.
- [ ] AC-7: Gate scout: grep for all `INSERT INTO audit_logs` in the repo — enumerate every caller and verify each routes through the canonical writer.

## Out of Scope

- Partitioning scheme (continues monthly, chain crosses partition boundaries fine)
- Tamper-proof crypto upgrade (SHA-256 HMAC, key rotation) — that's a future story
- UI changes to audit log screen

## Dependencies

- Blocked by: —
- Blocks: any UAT's `audit log hash chain integrity` verify check (UAT-001 verify 7, UAT-002 verify 6, UAT-003 verify 6, UAT-012 entire flow)

## Architecture Reference

- Audit store: `internal/audit/` (or `internal/store/audit.go` — locate)
- Partitioned table: `audit_logs_YYYY_MM` (monthly)
- Canonical writer candidates: grep `audit_logs.*INSERT` and `INSERT INTO audit_logs`
- Suspicious handler: `internal/api/operator/handler.go` (operator.update action)
- Verify endpoint: `internal/api/audit/` (locate)
- STORY-056 AC-4 registered `/api/v1/audit` route — chain logic landed before that

## Test Scenarios

- [ ] Unit: `AuditStore.Write` computes `prev_hash` from current tail
- [ ] Unit: two concurrent Write calls serialise correctly — neither races to zero `prev_hash`
- [ ] Integration: insert 100 events via mixed action types → `verify` returns true
- [ ] Integration: manually UPDATE one row's `data` column → `verify` returns `first_invalid = that row id`
- [ ] Regression: rerun UAT-001 verify 7, UAT-002 verify 6, UAT-003 verify 6 — all pass

## Effort

L — chain repair migration + audit of every writer + concurrency hardening + tests. Minimum 2 Dev cycles.
