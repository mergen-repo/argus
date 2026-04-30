# Fix Plan: FIX-302 — Audit Hash Chain Broken at Entry 1 (RECURRENCE batch1 F-10)

**Bucket**: BUG (CRITICAL)
**Source**: UAT-001/UAT-012 finding **F-3** in `docs/reports/uat-acceptance-2026-04-30.md`
**RECURRENCE of**: FIX-104 (commit `2d7f917`) — original "Audit Hash Chain Integrity"
**Effort**: M (2-4h plan, ~1 day implementation incl. fresh-DB E2E test)

---

## Symptom

`GET /api/v1/audit-logs/verify` returns:

```json
{"verified":false,"entries_checked":1,"first_invalid":1,"total_rows":1}
```

despite the database holding 805+ audit_logs rows. UI screen `uat-012-audit-log.png` confirms the audit page renders the chain as broken / unverifiable.

### Reproduction (current main branch, 2026-04-30)

```bash
make up && make db-migrate && make db-seed     # seed runs `argus repair-audit` post-seed
TOKEN=$(curl -s http://localhost:8084/api/v1/auth/login -d '{"email":"admin@argus.io","password":"admin"}' | jq -r .data.token)
curl -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/audit-logs/verify
# → {"verified":false,"entries_checked":1,"first_invalid":1,"total_rows":1}
```

`total_rows:1` is misleading — verifier bails on first invalid; real DB has 805 rows.

---

## Root Cause Investigation

### Evidence A — entry 1's stored hash IS valid in seed format

```sql
SELECT id, hash AS stored, encode(sha256((tenant_id::text||'|'||COALESCE(user_id::text,'system')||'|'||action||'|'||entity_type||'|'||entity_id||'|'||to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US000"Z"')||'|'||prev_hash)::bytea),'hex') AS seed_pad_hash FROM audit_logs WHERE id = 1;
-- stored == seed_pad_hash → seed wrote a self-consistent hash
```

The seed-format recompute matches. Entry 1 has `prev_hash = GenesisHash` correctly. So the seed's chain IS internally consistent.

### Evidence B — Go's `VerifyChain` produces a DIFFERENT hash for entry 1

Reproduced via standalone Go program connecting to seeded DB and running `audit.ComputeHash(entry, GenesisHash)`:

```
created_at:  2026-04-30 13:20:55.65019 +0300 +03   (pgx returns server-tz)
rfc3339nano: 2026-04-30T13:20:55.65019+03:00      (NOT 'Z')
hash_input:  ...|2026-04-30T13:20:55.65019+03:00|...
computed:    f665f25e674faceee3afd069e9910667a044c4c5476b597d78e9d2ad6c73cc95
stored:      96c644f40698e22b2e8b2ffdd9ecb6f0dcc327444e8058baffa03770093c715d
match:       false
```

### Evidence C — bucket scan over post-seed rows (251..805)

```sql
SELECT COUNT(*) FILTER (WHERE recomputed=hash) AS ok,
       COUNT(*) FILTER (WHERE recomputed<>hash) AS broken,
       MIN(id) FILTER (WHERE recomputed<>hash) AS first_broken_id
FROM (… recompute via to_char .US no-pad …) WHERE id > 250;
-- ok=544  broken=64  first_broken=254
```

Even **runtime-written rows** (Go path through `store.AuditStore.CreateWithChain`) verify-fail intermittently — about 11% of post-seed rows. So the defect is not seed-only.

### Two distinct defects

#### DEFECT 1 — Timezone non-normalization in verify path (`internal/audit/audit.go:75-93`, `internal/audit/service.go:154`)

`audit.ComputeHash` formats `entry.CreatedAt` with `time.RFC3339Nano`. RFC3339Nano renders the timezone as the time's zone (`Z` for UTC, `+03:00` otherwise). At INSERT time, `service.go:104` calls `time.Now().UTC()` so the format is `Z`. At VERIFY time, pgx (per its default config) returns `timestamptz` columns with the **session timezone** — which is the Postgres server's `timezone` setting (Europe/Istanbul = `+03:00` in this deployment). Result: same instant, different string, different hash.

This affects **every row, every verify** — the DB sees this as invalidating ~100% of rows in non-UTC environments. Why does verify report `first_invalid:1` (not "all rows")? Because `VerifyChain` (`service.go:120`) bails on the first failure.

#### DEFECT 2 — RFC3339Nano trailing-zero ambiguity vs PG `to_char('.US')` (`migrations/seed/003_comprehensive_seed.sql:1009`, `internal/store/audit.go:65`)

When microseconds end in trailing zero(s), Go's `time.RFC3339Nano` strips them (`.650190` → `.65019`), but PG's `to_char('YYYY-MM-DD"T"HH24:MI:SS.US"Z"')` always emits 6 digits (`.650190`). Seed lines 1008-1011 compute the hash via `to_char(... 'US000"Z"')` which forces **9 digits** (`.650190000`). All three formats are inequivalent on micros ending in zero.

Even if Defect 1 is fixed, Defect 2 means: (a) seeded rows 1..250 will verify-fail on the ~10% with trailing-zero microseconds, and (b) `repair-audit` post-seed should overwrite hashes — except `RepairChain` uses Go's `ComputeHash` which suffers Defect 1 itself, so repair installs more wrong hashes. The combination produces the 64 broken-out-of-608 statistic in Evidence C.

#### Why the existing trigger `trg_audit_chain_guard` did not catch it

`migrations/20260419000001_fix_audit_hashchain.up.sql:7-23` validates only `prev_hash == tail_hash`. It is content-blind — it never recomputes the row's own hash. A row with a valid `prev_hash` but a wrong (any wrong) `hash` passes the guard. This is by design (the guard cannot recompute Go's hash from PL/pgSQL without exact format parity), but it means defects 1 and 2 are invisible until verify runs.

---

## Root Cause (final)

**`audit.ComputeHash` is not deterministic against the database round-trip.** Two contributing format defects:

1. **Timezone:** `time.RFC3339Nano` includes the time's zone. `time.Now().UTC()` writes with `Z`, but pgx reads back with the server zone (`+03:00`). The verify path must normalize to UTC before formatting.
2. **Sub-second precision:** `time.RFC3339Nano` strips trailing zeros, while PG `to_char('.US')` emits a fixed 6 digits and the seed uses `.US000` (9 digits). The hash formula must use a single canonical sub-second representation that survives PG round-trip.

FIX-104 (commit `2d7f917`) addressed transactional chain writes and the seed format mismatch (lines 1008-1011 are FIX-104's "fixed" format), but:
- FIX-104 did **not** add a fresh-DB E2E test that boots the binary, runs `make db-seed`, and asserts `verify → true`. All FIX-104 unit tests use Go-on-both-sides (no DB round-trip), masking Defect 1. Integration test `internal/store/audit_integration_test.go` does write+read but never calls `VerifyChain` after a `time.Now().UTC()` insert from a non-UTC PG session.
- FIX-104 introduced `.US000Z` in the seed believing it matched Go's `RFC3339Nano`, but RFC3339Nano is variable-length (Defect 2).
- FIX-104's Task 4 §iii explicitly chose `'YYYY-MM-DD"T"HH24:MI:SS.US000"Z"'` as "Go-compatible". It is not.

---

## Fix Approach

**Decision: Approach B (single source of truth = Go).** Strip seed's hash computation entirely; rely solely on Go's `RepairChain` to write hashes after seed completes.

Rationale (vs. Approach A "change ComputeHash to fixed-width"):
- Approach A still leaves the seed and Go as parallel implementations — any future format tweak must be applied to both. That is exactly the failure mode FIX-104 exhibited.
- Approach B yields one hash function in the entire codebase. Seed inserts rows with `prev_hash = '0' x 64`, `hash = '0' x 64` placeholder. Then `repair-audit` (already wired into `make db-seed` at Makefile:174) recomputes both columns in a single ordered pass.
- The trigger `trg_audit_chain_guard` must be temporarily disabled around the placeholder-hash inserts, then re-enabled before runtime starts. Seed already does this (lines 967, 1038).

**Fix Defect 1 (timezone) — `internal/audit/audit.go:ComputeHash`:**

Change line 87 from
```go
entry.CreatedAt.Format(time.RFC3339Nano),
```
to
```go
entry.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
```

This fixes both defects simultaneously:
- `.UTC()` normalizes timezone before formatting (Defect 1).
- `"2006-01-02T15:04:05.000000Z"` is a Go layout with a literal `Z` suffix and a fixed 6-digit microsecond fractional part with zero-padding (Defect 2). It is byte-for-byte equivalent to PG `to_char('YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`. Microsecond is the storage precision (`Truncate(time.Microsecond)` at `store/audit.go:64`), so no information is lost.

**Update `RepairChain`** (`internal/store/audit.go:147`): no code change — it already calls `audit.ComputeHash`. Once `ComputeHash` is fixed, repair produces the right hashes.

**Update seed** (`migrations/seed/003_comprehensive_seed.sql:962-1040`): two options, choose B.1.

- **B.1 (recommended):** Replace the whole DO block with a simpler one that inserts 250 rows using placeholder hashes (`'0' x 64`) and chained `prev_hash = previous row's hash placeholder`. Wrap in `DISABLE TRIGGER trg_audit_chain_guard` (already there). The `repair-audit` step that follows in the Makefile recomputes correct hashes.
- **B.2 (alt):** Switch the hash format to `'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'` and use a single global `prev_h` variable. Equivalent correctness but duplicates the hash formula in two languages. Not recommended.

Selecting B.1.

**Update `audit_chain_guard` trigger** (`migrations/<new>_audit_hashchain_format_fix.up.sql`): no change to existing trigger; it remains content-blind. Add a separate **on-startup verification** (see AC-5) instead.

---

## Acceptance Criteria

| AC | Description | Verification |
|---|---|---|
| **AC-1** | After fresh `make up && make db-migrate && make db-seed && make db-repair-audit`, `GET /audit-logs/verify` returns `verified:true, total_rows >= 250, entries_checked == total_rows, first_invalid == null`. | Manual + scripted curl in CI. |
| **AC-2** | **Reproduction Go test (fresh-DB E2E)** — `internal/audit/verify_e2e_test.go` boots a real Postgres (testcontainers), runs migrations + seed SQL + `RepairChain`, opens a pool with `timezone = 'Asia/Istanbul'` (deliberately non-UTC), inserts ≥10 audit entries via `FullService.CreateEntry`, then calls `FullService.VerifyChain(ctx)` and asserts `Verified == true` and `TotalRows == seeded_count + inserted_count`. **Test must FAIL on current main and PASS after this fix.** | `go test ./internal/audit/ -run TestVerifyChainEndToEndAcrossTimezones` |
| **AC-3** | First audit row has `prev_hash == GenesisHash` after seed+repair. | `SELECT prev_hash FROM audit_logs ORDER BY id LIMIT 1` returns `0` × 64. |
| **AC-4** | All `audit_logs` writers route through `store.AuditStore.CreateWithChain` (no direct INSERT in Go code; the only SQL INSERT is the seed which writes placeholder hashes overwritten by repair). Re-run FIX-104 Task 7 gate-scout enumeration. | `grep -rn 'INSERT INTO audit_logs' internal/ cmd/ --include='*.go'` returns only `internal/store/audit.go:CreateWithChain`. The seed's INSERT is acceptable because hashes are placeholders. |
| **AC-5** | **Boot-time self-check:** at argus startup, after migrations + seed (idempotent guard), call `FullService.VerifyChain` once and log a structured WARN with `first_invalid` if it returns `verified:false`. Does not block boot (the chain may be intentionally broken in dev), but surfaces breaks in monitoring. | grep startup logs for `audit_chain_self_check` event. Failure path tested via deliberately corrupting one row in test. |
| **AC-6** | Unit test `TestComputeHash_TimezoneInvariant` — given the same instant in different `time.Location`s (UTC, Europe/Istanbul, America/Los_Angeles), `ComputeHash` returns the same digest. | `go test ./internal/audit/ -run TestComputeHash_TimezoneInvariant` |
| **AC-7** | Unit test `TestComputeHash_TrailingZeroMicroseconds` — instants ending `.000000`, `.123450`, `.123456` all hash deterministically and round-trip via PG `to_char('.US')`. Pure-Go test (no DB) using the new fixed-width formatter. | `go test ./internal/audit/ -run TestComputeHash_TrailingZeroMicroseconds` |
| **AC-8** | **Regression test FIX-104 was missing:** `TestVerifyChain_AfterRepairFromSeed` — performs `repair-audit` on a freshly seeded DB (real PG, server tz != UTC), then verifies via `FullService.VerifyChain`. Documents in test comment "FIX-104 shipped without this test; FIX-302 adds it after recurrence." | `go test ./internal/audit/ -run TestVerifyChain_AfterRepairFromSeed` |
| **AC-9** | UAT-001/UAT-012 audit-log verify panel re-runs PASS in next UAT cycle. | UAT cycle. |
| **AC-10** | `VerifyChain` response shape is unchanged (`{verified, entries_checked, first_invalid, total_rows}`). | Contract test. |

---

## Files Changed

| File | Change | Reason |
|---|---|---|
| `internal/audit/audit.go` | One-line edit: `ComputeHash` formats `entry.CreatedAt.UTC()` with `"2006-01-02T15:04:05.000000Z"`. | Fix Defects 1 + 2. |
| `internal/audit/audit_test.go` | Add `TestComputeHash_TimezoneInvariant`, `TestComputeHash_TrailingZeroMicroseconds`. | AC-6, AC-7. |
| `internal/audit/verify_e2e_test.go` | **New file.** Fresh-DB E2E with non-UTC PG session. Behind `//go:build integration` if needed; runs in CI integration job. | AC-2 (reproduction test FIX-104 lacked), AC-8. |
| `migrations/seed/003_comprehensive_seed.sql` | Lines 962-1040 rewritten: insert 250 rows with `prev_hash = '0' x 64`, `hash = '0' x 64` placeholders. Keep DISABLE/ENABLE trigger guards. Drop the in-SQL hash computation entirely. | Single source of truth = Go. |
| `cmd/argus/main.go` | Add boot-time self-check call to `FullService.VerifyChain` after audit service init; log WARN on mismatch. | AC-5. |
| `docs/architecture/ALGORITHMS.md` (audit hash section, if exists) | Document the canonical hash format `2006-01-02T15:04:05.000000Z` and the rule "all writes go through Go; SQL must use placeholder hashes + repair". | Prevent recurrence. |

No new migration is required (the audit table schema is unchanged; only seed-data generation logic moves). No down-migration concerns.

---

## What FIX-104 Was Missing — explicit gap analysis

FIX-104 closed with these test types:
- Unit tests in `audit_test.go` using Go's `ComputeHash` on both writer and verifier (same code path → trivially passes regardless of format).
- Integration test `internal/store/audit_integration_test.go:CreateWithChain` round-trips one row; never calls `VerifyChain`.
- Concurrency test (10 goroutines × 10 writes) — also Go-on-both-sides; no DB time round-trip exposed.

**Test that would have caught this**:
- A fresh-DB E2E that (a) seeds, (b) lets PG and Go write rows, (c) sets `timezone = 'Asia/Istanbul'` on the verify connection, (d) calls `FullService.VerifyChain`, (e) asserts `verified:true`.

This test is mandatory under AC-2/AC-8 of this plan. It is impossible to write a unit test for this defect because the bug is in the **PG ↔ Go time round-trip** — a pure-Go test where the same code formats both sides cannot reproduce it.

---

## Risks

| Risk | Mitigation |
|---|---|
| Existing dev/UAT databases have wrong hashes seeded by the old format. | Pre-release; `make db-seed` re-runs are fine. The seed's `IF already_seeded THEN RETURN` guard (line 992-993) prevents duplication, but operators must `make db-reset` (or `DROP TABLE audit_logs CASCADE; make db-migrate; make db-seed`) on existing DBs. Document in plan handoff. |
| Production DB (none yet, pre-release) would need a one-shot rehash. | Out of scope; `make db-repair-audit` already exists for forward use. |
| `time.Now().UTC()` writes are now redundant (ComputeHash forces UTC). | Harmless — keep for clarity; `.UTC()` is idempotent and cheap. |
| Boot-time self-check (AC-5) noisy in dev environments where chain is intentionally tampered. | Self-check emits a single WARN with `first_invalid` field; does not block boot; observable but not alarming. |
| `audit_chain_guard` trigger remains content-blind. | Documented as defense-in-depth limit. The boot-time self-check (AC-5) closes the detection gap without forcing PL/pgSQL to recompute Go-format hashes. |
| Some downstream code may depend on RFC3339Nano formatting elsewhere. | The format change is **scoped to `ComputeHash`'s internal hash input only**. The `Entry.CreatedAt` JSON marshal still uses Go default (RFC3339Nano). API responses unchanged. |

---

## Implementation Order

1. **Edit `audit.ComputeHash`** (1 line) — Defects 1+2.
2. **Add unit tests** AC-6, AC-7 — should fail on main, pass after step 1.
3. **Rewrite seed audit block** — placeholder hashes only.
4. **Add boot-time self-check** (`cmd/argus/main.go`, ~10 lines).
5. **Add E2E test** AC-2/AC-8 — must require real PG with non-UTC session timezone.
6. **Run `make db-reset && make db-migrate && make db-seed`** — verify endpoint returns `verified:true`.
7. **Update `docs/architecture/ALGORITHMS.md`** with canonical format note.
8. **Re-run UAT-001/012** subset to confirm AC-9.

---

## Plan File Path

`docs/stories/uat-remediation/FIX-302-plan.md` (this file).
