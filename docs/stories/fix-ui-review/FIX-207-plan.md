# Implementation Plan: FIX-207 — Session/CDR Data Integrity — Negative Duration, Cross-Pool IP, IMSI Format

## Goal

Restore invariant-level data integrity for sessions and CDRs so downstream analytics, billing, and audit can trust their inputs: (1) impossible-duration rows are prevented at write time, (2) framed_ip stays within the SIM's APN-scoped IP pool set, (3) malformed IMSIs are rejected with a deterministic error code before they enter sessions/CDRs, (4) a daily scan publishes ops-visible invariant violations, (5) existing bad rows are preserved in a `session_quarantine` table rather than deleted, and (6) the RADIUS NAS-IP-Address AVP path is verified end-to-end and emits a signal when the AVP is missing (the simulator-side fix lives in FIX-226).

## Problem Context & Critical Schema Clarifications

The story dispatch names AC-1 as "CHECK constraint `duration_sec >= 0`" on `sessions`. **The `sessions` table has no `duration_sec` column** — the actual columns relevant to duration are `started_at TIMESTAMPTZ NOT NULL` and `ended_at TIMESTAMPTZ NULL` (see `migrations/20260320000002_core_schema.up.sql:390-413`). Duration is computed at read time as `EXTRACT(EPOCH FROM (COALESCE(ended_at,NOW()) - started_at))`.

Per advisor review, AC-1 is executed as **Option B** (source-invariant, no denormalization):

- `CHECK (ended_at IS NULL OR ended_at >= started_at)` on `sessions`

This is the minimum additive invariant that prevents negative duration at the source. Option A (materialize a `duration_sec` column + backfill) was rejected because (i) it duplicates a derivable value across a TimescaleDB hypertable, (ii) requires a second code change in `session_radius.go Finalize` to keep it in sync, and (iii) adds migration scope with zero analytics benefit — queries already compute duration on-read.

AC-2 (`cdrs.duration_sec >= 0`) is applied literally — the `cdrs` table DOES have `duration_sec INTEGER NOT NULL DEFAULT 0` (core_schema:429) and is a TimescaleDB hypertable.

AC-3 references "SIM's assigned `ip_pool_id`" — **`sims` has no `ip_pool_id` column**. Derived relationship:

- `sims.ip_address_id → ip_addresses.pool_id` (direct: when allocated)
- `sims.apn_id → ip_pools.apn_id` (transitive: APN-scoped pool set, which may have 1..N pools)

Validation semantics codified below in "Service-Layer framed_ip Validation (AC-3)".

AC-7 NAS-IP backend: `internal/aaa/radius/server.go:773-775` already calls `rfc2865.NASIPAddress_Lookup` in Acct-Start and passes the result into `session.Session.NASIP`, which `session_radius.go` persists to the `sessions.nas_ip INET` column. This story VERIFIES end-to-end correctness, adds a regression test, and emits a WARN log + metric when the AVP is missing (that signal is the closure marker for FIX-226 simulator work). It does NOT re-implement AVP extraction.

## Architecture Context

### Components Involved

| Component | Layer | File(s) | Responsibility in FIX-207 |
|---|---|---|---|
| `sessions` hypertable | DB (TimescaleDB) | `migrations/20260320000002_core_schema.up.sql:390-413` | New CHECK constraint `ended_at >= started_at` (AC-1) |
| `cdrs` hypertable | DB (TimescaleDB) | `migrations/20260320000002_core_schema.up.sql:418-435` | New CHECK constraint `duration_sec >= 0` (AC-2) |
| `session_quarantine` | DB (plain table, new) | `migrations/2026042100000X_session_quarantine.up.sql` | Holds rows that would violate the new invariants (AC-6) |
| `internal/aaa/validator/imsi.go` | Go package (new) | — | Pure-Go IMSI format validator `^\d{14,15}$` (AC-4) |
| `internal/config/config.go` | Config | existing | New `IMSIStrictValidation bool` env toggle (AC-4) |
| `internal/apierr/apierr.go` | API error catalog | existing | New `CodeInvalidIMSIFormat = "INVALID_IMSI_FORMAT"` (AC-4) |
| `internal/aaa/radius/server.go` | AAA protocol | existing | Enforce IMSI validator in `handleDirectAuth` + emit missing-NAS-IP log/metric (AC-4, AC-7) |
| `internal/aaa/session/session.go` | AAA session mgr | existing | Framed-IP pool validation at session Create (AC-3) |
| `internal/api/sim/handler.go` | API | existing | IMSI validator on SIM Create (AC-4) |
| `internal/analytics/cdr/consumer.go` | NATS consumer | existing | IMSI validator before CDR persist; drop + metric (AC-4) |
| `internal/job/data_integrity.go` | Job (new) | — | Daily cron scan for invariant violations (AC-5) |
| `internal/observability/metrics/metrics.go` | Metrics | existing | Two new counters (AC-4, AC-7) |
| `internal/store/cdr.go` `internal/store/session_radius.go` | Data access | existing | No schema-code changes; CHECK constraint enforces at DB |

### Data Flow — CHECK constraint on hypertables

```
Migration sequence for AC-1, AC-2, AC-6:
  1. Migration A — session_quarantine table + retro cleanup (data-repair, idempotent)
     - CREATE TABLE session_quarantine (not a hypertable; plain table; small)
     - INSERT INTO session_quarantine SELECT * FROM sessions WHERE ended_at IS NOT NULL AND ended_at < started_at (+ metadata)
     - INSERT INTO session_quarantine SELECT * FROM cdrs WHERE duration_sec < 0 (+ metadata)
     - DELETE FROM sessions WHERE ... (only quarantined rows)
     - DELETE FROM cdrs WHERE duration_sec < 0
     - All inside BEGIN/COMMIT.
  2. Migration B — add CHECK constraints
     - ALTER TABLE sessions ADD CONSTRAINT chk_sessions_ended_after_started CHECK (ended_at IS NULL OR ended_at >= started_at)
     - ALTER TABLE cdrs ADD CONSTRAINT chk_cdrs_duration_nonneg CHECK (duration_sec >= 0)
     - Both plain CHECK (NO `NOT VALID` — PG16 rejects NOT VALID CHECK on partitioned/hypertables; plain CHECK works but scans table).
     - Add migration-time RAISE WARNING if sessions > 100k OR cdrs > 1M (track prod cutover as D-067).

Service-layer flow (runtime):
  RADIUS Access-Request → handleDirectAuth
    → (AC-4) validator.ValidateIMSI(imsi, cfg.IMSIStrictValidation)
    → if invalid: Access-Reject + metric argus_imsi_invalid_total{source="radius"} + log
  RADIUS Acct-Start → handleAcctStart
    → (AC-7) if nasIP == "" after rfc2865.NASIPAddress_Lookup: metric argus_radius_nas_ip_missing_total + WARN log (reason="no_avp")
    → session.Create(sess) → (AC-3) manager.validateFramedIP(sim, sess.FramedIP) before sessionStore.Create
  CDR consumer (NATS) → handleEvent
    → (AC-4) if imsi present on event and invalid: drop + metric argus_imsi_invalid_total{source="cdr"}
    → CDRStore.CreateIdempotent — CHECK constraint enforces duration_sec >= 0 at DB
  Daily cron (03:17 UTC) → DataIntegrityJob.Run
    → SELECT counts for 4 invariants over last 24h: neg-duration sessions, neg-duration cdrs,
      orphan framed_ip (not in any APN pool), malformed IMSI (against current strict flag)
    → publish metric argus_data_integrity_violations_total{kind="..."} + WARN log + notification on > 0
```

### API Specifications

FIX-207 does not add new HTTP endpoints. It augments existing paths with new failure modes:

- `POST /api/v1/sims` (existing, `internal/api/sim/handler.go:258`)
  - NEW validation: if `req.IMSI` fails IMSI regex and `IMSIStrictValidation=true` → 400 `INVALID_IMSI_FORMAT`
  - Error envelope:
    ```json
    {"status":"error","error":{"code":"INVALID_IMSI_FORMAT",
      "message":"IMSI does not match expected format (14-15 digits).",
      "details":[{"field":"imsi","value":"abc123","expected":"^\\d{14,15}$"}]}}
    ```
- RADIUS (port 1812) Access-Request — new Access-Reject reason `INVALID_IMSI_FORMAT` when strict mode is on and UserName is not 14-15 digits. Pre-existing `MISSING_IMSI` remains for empty UserName.

### Database Schema

Source for `sessions`, `cdrs`: `migrations/20260320000002_core_schema.up.sql` (ACTUAL, canonical).
Source for hypertable conversion: `migrations/20260320000003_timescaledb_hypertables.up.sql`.

```sql
-- Source: core_schema.up.sql:390-413 (ACTUAL — unchanged by this story; CHECK added in new migration)
CREATE TABLE sessions (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    nas_ip INET,             -- populated from RFC 2865 §5.4 NAS-IP-Address AVP (AC-7 backend path)
    framed_ip INET,           -- subject to AC-3 validation on Create
    calling_station_id VARCHAR(50),
    called_station_id VARCHAR(100),
    rat_type VARCHAR(10),
    session_state VARCHAR(20) NOT NULL DEFAULT 'active',
    auth_method VARCHAR(20),
    policy_version_id UUID,
    acct_session_id VARCHAR(100),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,    -- AC-1 invariant: ended_at IS NULL OR ended_at >= started_at
    terminate_cause VARCHAR(50),
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    packets_in BIGINT NOT NULL DEFAULT 0,
    packets_out BIGINT NOT NULL DEFAULT 0,
    last_interim_at TIMESTAMPTZ
    -- NOTE: no duration_sec column. See "Problem Context" for Option-B rationale.
);
-- Hypertable, partitioned by started_at (timescaledb_hypertables.up.sql:4).

-- Source: core_schema.up.sql:418-435 (ACTUAL — unchanged; CHECK added in new migration)
CREATE TABLE cdrs (
    id BIGSERIAL,
    session_id UUID NOT NULL,
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    rat_type VARCHAR(10),
    record_type VARCHAR(20) NOT NULL,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    duration_sec INTEGER NOT NULL DEFAULT 0,  -- AC-2 invariant: duration_sec >= 0
    usage_cost DECIMAL(12,4),
    carrier_cost DECIMAL(12,4),
    rate_per_mb DECIMAL(8,4),
    rat_multiplier DECIMAL(4,2) DEFAULT 1.0,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Hypertable, partitioned by timestamp (timescaledb_hypertables.up.sql:21).
```

#### New table — `session_quarantine` (AC-6)

Single consolidated quarantine table (advisor recommendation: one quarantine surface beats two). Not a hypertable — small, operational, query-friendly.

```sql
-- Source: NEW (FIX-207)
-- Location: migrations/20260421000001_session_quarantine.up.sql (naming matches plan task numbering)
CREATE TABLE IF NOT EXISTS session_quarantine (
    id BIGSERIAL PRIMARY KEY,
    original_table TEXT NOT NULL CHECK (original_table IN ('sessions', 'cdrs')),
    original_id TEXT NOT NULL,             -- sessions.id::text or cdrs.id::text
    tenant_id UUID,                         -- copied from source row if present
    violation_reason TEXT NOT NULL,         -- e.g. 'negative_duration', 'ended_before_started'
    row_data JSONB NOT NULL,                -- full row snapshot (to_jsonb(t.*))
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    quarantined_by TEXT NOT NULL            -- 'fix207_retro' or 'fix207_scan' or 'runtime_reject'
);
CREATE INDEX IF NOT EXISTS idx_session_quarantine_table_time
  ON session_quarantine (original_table, quarantined_at DESC);
CREATE INDEX IF NOT EXISTS idx_session_quarantine_tenant
  ON session_quarantine (tenant_id, quarantined_at DESC) WHERE tenant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_session_quarantine_reason
  ON session_quarantine (violation_reason);
```

#### Migration A — `migrations/20260421000001_session_quarantine.up.sql`

```sql
-- FIX-207 Migration A: create session_quarantine table + retro cleanup (AC-6, prep for AC-1/AC-2)
-- Idempotent: running twice inserts no additional rows (uses NOT EXISTS guard).
-- Non-destructive: original rows copied into quarantine BEFORE deletion from hypertables.

BEGIN;

-- 1. Schema
CREATE TABLE IF NOT EXISTS session_quarantine (
    id BIGSERIAL PRIMARY KEY,
    original_table TEXT NOT NULL CHECK (original_table IN ('sessions', 'cdrs')),
    original_id TEXT NOT NULL,
    tenant_id UUID,
    violation_reason TEXT NOT NULL,
    row_data JSONB NOT NULL,
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    quarantined_by TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_quarantine_table_time
  ON session_quarantine (original_table, quarantined_at DESC);
CREATE INDEX IF NOT EXISTS idx_session_quarantine_tenant
  ON session_quarantine (tenant_id, quarantined_at DESC) WHERE tenant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_session_quarantine_reason
  ON session_quarantine (violation_reason);

-- 2. Count summary (diagnostic NOTICE)
DO $$
DECLARE
    bad_sessions INTEGER;
    bad_cdrs INTEGER;
BEGIN
    SELECT COUNT(*) INTO bad_sessions FROM sessions
      WHERE ended_at IS NOT NULL AND ended_at < started_at;
    SELECT COUNT(*) INTO bad_cdrs FROM cdrs WHERE duration_sec < 0;
    RAISE NOTICE 'FIX-207 retro: % sessions with ended_at<started_at; % cdrs with duration_sec<0',
      bad_sessions, bad_cdrs;
END $$;

-- 3. Quarantine sessions (ended_at before started_at) — only those NOT already quarantined
INSERT INTO session_quarantine (original_table, original_id, tenant_id, violation_reason, row_data, quarantined_by)
SELECT 'sessions',
       s.id::text,
       s.tenant_id,
       'ended_before_started',
       to_jsonb(s.*),
       'fix207_retro'
FROM sessions s
WHERE s.ended_at IS NOT NULL AND s.ended_at < s.started_at
  AND NOT EXISTS (
    SELECT 1 FROM session_quarantine q
    WHERE q.original_table = 'sessions' AND q.original_id = s.id::text
      AND q.violation_reason = 'ended_before_started'
  );

-- 4. Quarantine cdrs (duration_sec < 0) — only those NOT already quarantined
INSERT INTO session_quarantine (original_table, original_id, tenant_id, violation_reason, row_data, quarantined_by)
SELECT 'cdrs',
       c.id::text,
       c.tenant_id,
       'negative_duration',
       to_jsonb(c.*),
       'fix207_retro'
FROM cdrs c
WHERE c.duration_sec < 0
  AND NOT EXISTS (
    SELECT 1 FROM session_quarantine q
    WHERE q.original_table = 'cdrs' AND q.original_id = c.id::text
      AND q.violation_reason = 'negative_duration'
  );

-- 5. Delete quarantined source rows so CHECK constraint can be added in Migration B
--    Use EXISTS join against quarantine so we never delete rows we haven't first preserved.
DELETE FROM sessions
 WHERE ended_at IS NOT NULL AND ended_at < started_at
   AND EXISTS (
     SELECT 1 FROM session_quarantine q
     WHERE q.original_table = 'sessions' AND q.original_id = sessions.id::text
   );

DELETE FROM cdrs
 WHERE duration_sec < 0
   AND EXISTS (
     SELECT 1 FROM session_quarantine q
     WHERE q.original_table = 'cdrs' AND q.original_id = cdrs.id::text
   );

COMMIT;
```

Down migration: no-op (data repair is one-way; see FIX-206 precedent). Documented in down.sql header.

#### Migration B — `migrations/20260421000002_session_cdr_invariants.up.sql`

```sql
-- FIX-207 Migration B: CHECK constraints on sessions + cdrs for data integrity (AC-1, AC-2).
-- Must run AFTER Migration A (retro cleanup) — filename lexical order enforces this.
-- Plain CHECK (no NOT VALID — PG16 rejects NOT VALID CHECK on partitioned tables;
-- plain CHECK scans the table once under ACCESS EXCLUSIVE. On prod-scale data this may
-- stall briefly; emit a WARNING so ops are aware. See D-067 runbook tracking.

BEGIN;

-- Prod-safety: warn if row counts are large (hypertables may have many chunks).
DO $$
DECLARE
    session_count BIGINT;
    cdr_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO session_count FROM sessions;
    SELECT COUNT(*) INTO cdr_count FROM cdrs;
    IF session_count > 100000 THEN
        RAISE WARNING 'FIX-207: sessions has % rows — ALTER TABLE ADD CONSTRAINT will hold ACCESS EXCLUSIVE during scan. See ROUTEMAP D-067 for prod cutover plan.', session_count;
    END IF;
    IF cdr_count > 1000000 THEN
        RAISE WARNING 'FIX-207: cdrs has % rows — ALTER TABLE ADD CONSTRAINT will hold ACCESS EXCLUSIVE during scan. See ROUTEMAP D-067 for prod cutover plan.', cdr_count;
    END IF;
END $$;

ALTER TABLE sessions
    ADD CONSTRAINT chk_sessions_ended_after_started
    CHECK (ended_at IS NULL OR ended_at >= started_at);

ALTER TABLE cdrs
    ADD CONSTRAINT chk_cdrs_duration_nonneg
    CHECK (duration_sec >= 0);

COMMIT;
```

Down migration drops both constraints:

```sql
BEGIN;
ALTER TABLE cdrs DROP CONSTRAINT IF EXISTS chk_cdrs_duration_nonneg;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS chk_sessions_ended_after_started;
COMMIT;
```

### Service-Layer framed_ip Validation (AC-3)

Policy: **log + audit + continue** (advisor guidance — rejecting live sessions creates operator incidents; AC-3 allows either but production prefers audit-and-correct).

Location: `internal/aaa/session/session.go` `Manager.Create`, BEFORE `sessionStore.Create`. Implemented as a helper `Manager.validateFramedIP(ctx, sim *store.SIM, framedIPStr string) (ok bool, reason string)`:

1. If `framedIPStr == ""`: return `(true, "")` (no IP to check — dynamic alloc may add it later).
2. Parse `framedIP := net.ParseIP(framedIPStr)`. If parse fails: return `(false, "unparseable_framed_ip")`.
3. If `sim.IPAddressID != nil`: fetch via `ipPoolStore.GetIPAddressByID`; if the address string does NOT match framedIP: return `(false, "mismatch_assigned_address")`.
4. Else (no static assignment): enumerate `ip_pools` where `apn_id = sim.APNID` via new store method `IPPoolStore.ListByAPN(ctx, tenantID, apnID)`. For each pool, check `framedIP` is inside `cidr_v4` or `cidr_v6` (use `net.ParseCIDR` + `IPNet.Contains`). If no pool contains it: return `(false, "outside_apn_pools")`.
5. Return `(true, "")`.

On `(false, reason)`: emit WARN log with {sim_id, framed_ip, apn_id, reason}; increment metric `argus_framed_ip_pool_mismatch_total{reason=...}`; write an audit_logs row (action `fix207.framed_ip_mismatch`, entity_type `session`); **do not reject** — allow session Create to proceed. The daily scan (AC-5) will surface the aggregate.

Dependency: new `IPPoolStore.ListByAPN` method — 5-line SQL helper.

**Hot-path performance**: AC-3 validation adds 1 DB round-trip per session-create when `sim.IPAddressID == nil`. The APN→pools lookup is tenant-bounded (typically ≤3 pools per APN). If perf review flags this: cache `{apn_id}→pool_cidrs` in the SIMCache layer alongside the SIM lookup; out-of-scope for FIX-207 unless dispatch-time benchmark shows >2ms added latency — track as D-068.

### IMSI Validator (AC-4)

Location: `internal/aaa/validator/imsi.go` (new package, co-located near main RADIUS consumer).

Signature:

```go
package validator

import "regexp"

var imsiRE = regexp.MustCompile(`^\d{14,15}$`)

// ValidateIMSI enforces ^\d{14,15}$ when strict is true.
// When strict=false (IMSI_STRICT_VALIDATION=false), returns nil for any non-empty string.
// Empty IMSI is always invalid (strict or not).
func ValidateIMSI(imsi string, strict bool) error { ... }

// IsIMSIFormatValid is the pure predicate without the strict bypass, for scan-job use.
func IsIMSIFormatValid(imsi string) bool { ... }
```

Error type `ErrInvalidIMSIFormat` exported from the same package; callers wrap + translate to `apierr.CodeInvalidIMSIFormat`.

Config: `internal/config/config.go` — new field:

```go
IMSIStrictValidation bool `envconfig:"IMSI_STRICT_VALIDATION" default:"true"`
```

Wiring — three call sites:

1. `internal/aaa/radius/server.go` `handleDirectAuth` (line 545, right after `rfc2865.UserName_LookupString`): if strict-invalid → `sendReject(w, r.Packet, "INVALID_IMSI_FORMAT")` + metric + log. This is BEFORE any SIM lookup so cache/DB stay untouched.
2. `internal/aaa/radius/server.go` `handleAcctStart` (line 728): same validation; if invalid, skip session create + log. (Accounting is harder to reject — log and drop.)
3. `internal/api/sim/handler.go` POST /api/v1/sims Create (around existing validation block, line 258-330): if strict-invalid → 400 `INVALID_IMSI_FORMAT` with field detail. SIM CRUD rejection is the fail-fast path — the FIX-206 FK + this IMSI check together prevent malformed rows from entering `sims`.
4. `internal/analytics/cdr/consumer.go` `handleEvent`: if evt contains an IMSI field (currently NOT in the struct — add `IMSI string` to `sessionEvent`) and it's strict-invalid, DROP the event + metric `argus_imsi_invalid_total{source="cdr"}`. (Best-effort: events may not always carry IMSI; skip check when empty.)

**NOTE**: `sessionEvent` in `internal/analytics/cdr/consumer.go:64-79` currently lacks `IMSI`. The RADIUS server event payload at `server.go:842` already includes `"imsi": imsi`, so publisher-side is correct. This task adds the field to the consumer struct.

Error code addition — `internal/apierr/apierr.go`:

```go
CodeInvalidIMSIFormat = "INVALID_IMSI_FORMAT" // FIX-207 (malformed IMSI rejected at API/AAA)
```

Also add to `docs/architecture/ERROR_CODES.md` Validation section (same row format as INVALID_REFERENCE from FIX-206).

### Daily Data-Integrity Scan Job (AC-5)

Location: `internal/job/data_integrity.go` (new file). Mirrors `orphan_session.go` structure (see Pattern ref in Task 5).

Schedule: registered via existing `Scheduler.AddEntry` in `cmd/argus/main.go` (same site where other cron entries live). Cron: `17 3 * * *` (03:17 UTC daily — avoid top-of-hour congestion). Job type: `data_integrity_scan`.

Scan logic (4 invariants over last 24h):

```sql
-- 1. sessions with ended_at < started_at (invariant violated despite CHECK — possible via direct SQL)
SELECT COUNT(*) FROM sessions WHERE ended_at IS NOT NULL AND ended_at < started_at
  AND started_at >= NOW() - INTERVAL '24 hours';

-- 2. cdrs with duration_sec < 0
SELECT COUNT(*) FROM cdrs WHERE duration_sec < 0
  AND timestamp >= NOW() - INTERVAL '24 hours';

-- 3. framed_ip outside any APN pool CIDR (for SIMs with non-null apn_id)
-- Joined via NOT EXISTS against ip_pools with inet<<=cidr semantics. Raw scan only
-- for the "sim has apn + session has framed_ip" subset. Expected near-zero.
SELECT COUNT(*) FROM sessions s
  JOIN sims m ON s.sim_id = m.id
 WHERE s.framed_ip IS NOT NULL AND s.started_at >= NOW() - INTERVAL '24 hours'
   AND m.apn_id IS NOT NULL
   AND NOT EXISTS (
     SELECT 1 FROM ip_pools p
     WHERE p.apn_id = m.apn_id
       AND ((p.cidr_v4 IS NOT NULL AND s.framed_ip <<= p.cidr_v4)
         OR (p.cidr_v6 IS NOT NULL AND s.framed_ip <<= p.cidr_v6))
   );

-- 4. malformed IMSI (cannot be done in SQL cleanly due to "14-15 digits" quantifier —
--    use regex via SQL ~ operator)
SELECT COUNT(*) FROM sims WHERE imsi !~ '^\d{14,15}$';
```

Job publishes 4 metrics `argus_data_integrity_violations_total{kind="neg_duration_session"|"neg_duration_cdr"|"framed_ip_outside_pool"|"imsi_malformed"}` and, if any count > 0, creates one `notifications` row per violation kind per tenant (use existing notification store). Also writes a single summary row to `audit_logs` with the counts.

### AC-7 — NAS-IP Backend Verify

Existing path:

- `server.go:773` extracts NAS-IP from Acct-Start packet via `rfc2865.NASIPAddress_Lookup`.
- Result propagates to `session.Session{NASIP: nasIP}` (server.go:810).
- `session.Manager.Create` (session.go:113) passes `NASIP` as `*string` to `sessionStore.Create`.
- `session_radius.go:102` INSERT writes `nas_ip INET` column.

This story:

1. Adds a regression test `TestRadiusServer_AcctStart_NASIPPersisted` — RADIUS-mock Access-Start with NAS-IP-Address AVP 192.0.2.10 → asserts session row's `nas_ip` column equals `192.0.2.10`.
2. Adds a second test `TestRadiusServer_AcctStart_MissingNASIP_EmitsMetric` — Access-Start WITHOUT NAS-IP AVP → asserts `argus_radius_nas_ip_missing_total` counter incremented by 1 and log line `"NAS-IP AVP missing from Acct-Start"` emitted.
3. Adds the `argus_radius_nas_ip_missing_total` counter registration to `internal/observability/metrics/metrics.go`.
4. Inserts 4-line log+metric emission in `server.go:handleAcctStart` right after the NAS-IP extraction (line 775), conditional on `nasIP == ""`.

The simulator-side NAS-IP injection is explicitly OUT OF SCOPE — that lives in FIX-226 (Simulator Coverage). The metric this task wires up IS the closure signal FIX-226 uses to verify its fix.

## Prerequisites

- [x] FIX-206 DONE — orphan operator cleanup + sims FK constraints in place (confirms `sims.apn_id → apns` and `sims.operator_id → operators` are valid, which this story's Task 3 framed-IP validation depends on)
- [x] `sessions` and `cdrs` TimescaleDB hypertable migration (`20260320000003`) confirmed live
- [x] `internal/apierr/apierr.go` has `CodeInvalidReference` constant (FIX-206) — Task 3 parallels the same pattern for `CodeInvalidIMSIFormat`
- [x] `orphan_session.go` exists as scan-job pattern (DEV-170)
- [x] `cmd/argus/main.go` has Scheduler wiring point (verified in Task 5 dispatch notes)

## Task Decomposition

### Task 1 — Migration A: `session_quarantine` table + retro cleanup (AC-6)

- **Files:** Create `migrations/20260421000001_session_quarantine.up.sql`, `.down.sql`.
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `migrations/20260420000001_sims_orphan_cleanup.up.sql` (FIX-206 Migration A) — follow the same "BEGIN / DO NOTICE / INSERT-before-DELETE / COMMIT / idempotency via NOT EXISTS" pattern. Read the down.sql for no-op header convention.
- **Context refs:** "Database Schema" > "New table — session_quarantine", "Migration A"
- **What:**
  1. Create `session_quarantine` table + 3 indexes.
  2. Emit RAISE NOTICE with pre-cleanup counts.
  3. INSERT quarantined sessions rows (`ended_at < started_at`) with `to_jsonb(s.*)` snapshot, reason=`ended_before_started`, quarantined_by=`fix207_retro`. Guard with NOT EXISTS for idempotency.
  4. INSERT quarantined cdrs rows (`duration_sec < 0`) similarly, reason=`negative_duration`.
  5. DELETE the source rows AFTER quarantine copy succeeded (use EXISTS join to quarantine).
  6. down.sql: `DROP TABLE IF EXISTS session_quarantine CASCADE;` with header note that this cannot restore deleted rows.
- **Verify:**
  - `argus migrate up` on a DB seeded with 3 bad session rows + 5 bad cdr rows: `SELECT COUNT(*) FROM session_quarantine` returns 8; `SELECT COUNT(*) FROM sessions WHERE ended_at < started_at` returns 0; `SELECT COUNT(*) FROM cdrs WHERE duration_sec < 0` returns 0.
  - Second-run idempotency: re-applying migration inserts 0 new quarantine rows.

### Task 2 — Migration B: CHECK constraints (AC-1, AC-2) + prod-safety warning

- **Files:** Create `migrations/20260421000002_session_cdr_invariants.up.sql`, `.down.sql`.
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `migrations/20260420000002_sims_fk_constraints.up.sql` (FIX-206 Migration B) — for the "DO block warning on row count > threshold" pattern that tracks D-065-style runbook debt. Note this story uses D-067 as the new tracker.
- **Context refs:** "Database Schema" > "Migration B"
- **What:**
  1. Top-of-file DO block: count sessions + cdrs; RAISE WARNING if > 100k / 1M respectively, citing D-067 runbook.
  2. `ALTER TABLE sessions ADD CONSTRAINT chk_sessions_ended_after_started CHECK (ended_at IS NULL OR ended_at >= started_at);`
  3. `ALTER TABLE cdrs ADD CONSTRAINT chk_cdrs_duration_nonneg CHECK (duration_sec >= 0);`
  4. down.sql: `DROP CONSTRAINT IF EXISTS` both.
  5. Header comment cites FIX-206 D-065 precedent (WARNING pattern) + notes that plain CHECK on hypertables holds ACCESS EXCLUSIVE during scan.
- **Verify:**
  - After migration: `\d+ sessions` shows `chk_sessions_ended_after_started` constraint; `\d+ cdrs` shows `chk_cdrs_duration_nonneg`.
  - Attempt `INSERT INTO sessions ... ended_at = started_at - interval '1 second'` fails with PG error 23514 (check_violation).
  - Attempt `INSERT INTO cdrs ... duration_sec = -5` fails with 23514.
  - Re-applying migration errors gracefully (constraint already exists); Dev wraps both ALTER statements with `DO $$ BEGIN IF NOT EXISTS ... END $$` or relies on `argus migrate` dirty-flag tracking (verify empirically which works; prefer DO-wrap for pure idempotency).

### Task 3 — IMSI validator package + config toggle + error code + apierr wiring (AC-4, part 1)

- **Files:** Create `internal/aaa/validator/imsi.go`, `internal/aaa/validator/imsi_test.go`. Modify `internal/config/config.go` (add `IMSIStrictValidation`), `internal/apierr/apierr.go` (add `CodeInvalidIMSIFormat`), `docs/architecture/ERROR_CODES.md` (add row to Validation table + to Go constants ledger).
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/apierr/apierr.go` lines around `CodeInvalidReference` (FIX-206) for constant-definition style. Read `internal/config/config.go:36-60` for `envconfig` field placement convention. Read `internal/aaa/eap/sim.go` for zerolog-free pure-helper package layout (no logger dependency).
- **Context refs:** "IMSI Validator (AC-4)"
- **What:**
  1. Create `internal/aaa/validator/imsi.go`:
     - Exported `var ErrInvalidIMSIFormat = errors.New("validator: imsi: invalid format")` and `ErrEmptyIMSI`.
     - `imsiRE = regexp.MustCompile(\`^\\d{14,15}$\`)` as package-level var (compiles once).
     - `ValidateIMSI(imsi string, strict bool) error`: empty → `ErrEmptyIMSI`; if !strict → `nil`; else match regex → `nil` or `ErrInvalidIMSIFormat`.
     - `IsIMSIFormatValid(imsi string) bool`: predicate, always strict.
  2. Create `imsi_test.go` with table-driven cases: empty, 13-digit, 14-digit, 15-digit, 16-digit, alpha, space-padded, strict=false with junk → nil.
  3. `config.go`: add `IMSIStrictValidation bool \`envconfig:"IMSI_STRICT_VALIDATION" default:"true"\`` grouped with other validation/feature flags.
  4. `apierr.go`: add `CodeInvalidIMSIFormat = "INVALID_IMSI_FORMAT"` under Validation block.
  5. `ERROR_CODES.md` Validation Errors table: add row for `INVALID_IMSI_FORMAT` (400 status) with example envelope showing `details: [{field:"imsi", value:"abc123", expected:"^\\d{14,15}$"}]`. Also add `CodeInvalidIMSIFormat` to the Go constants ledger block.
- **Verify:**
  - `go test ./internal/aaa/validator/...` PASS.
  - `go vet ./...` clean.
  - `grep -c 'CodeInvalidIMSIFormat' internal/apierr/apierr.go` returns 1.
  - `grep -c 'IMSI_STRICT_VALIDATION' internal/config/config.go` returns 1.
  - `grep -c 'INVALID_IMSI_FORMAT' docs/architecture/ERROR_CODES.md` returns ≥2 (row + constant).

### Task 4 — Wire IMSI validator into RADIUS, SIM handler, CDR consumer (AC-4, part 2)

- **Files:** Modify `internal/aaa/radius/server.go`, `internal/api/sim/handler.go`, `internal/analytics/cdr/consumer.go`. Add metric in `internal/observability/metrics/metrics.go` if not present.
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/radius/server.go:545-562` for reject pattern (`s.sendReject(w, r.Packet, "SIM_NOT_FOUND")`); read `internal/api/sim/handler.go:313-331` for SIM Create validation branch pattern (FIX-206 established); read `internal/analytics/cdr/consumer.go:81-105` for event-handle drop pattern.
- **Context refs:** "IMSI Validator (AC-4)" > wiring call sites, "API Specifications"
- **What:**
  1. RADIUS `handleDirectAuth` (server.go:545): right after `imsi, err := rfc2865.UserName_LookupString` and the `err != nil || imsi == ""` reject, add strict-mode validator check. Inject `IMSIStrictValidation bool` into `ServerConfig` struct + `NewServer` or via a simpler getter on the Server struct (Dev picks the less-invasive path; preserve existing constructor signatures where possible — an `s.imsiStrict bool` set via `SetIMSIStrictValidation` is acceptable). On invalid: `s.sendReject(w, r.Packet, "INVALID_IMSI_FORMAT")` + `s.recordAuthMetric(ctx, uuid.Nil, false, startTime)` + metric increment.
  2. RADIUS `handleAcctStart` (server.go:728): similar check; on invalid, log WARN + return (no Acct response needed beyond standard ack — accounting swallows malformed records rather than rejecting the network layer).
  3. SIM handler Create (handler.go:~310): add a line `if err := validator.ValidateIMSI(req.IMSI, cfg.IMSIStrictValidation); err != nil { return apierr with CodeInvalidIMSIFormat + field=imsi, value=req.IMSI }`. Wire the config flag through the handler's dependency struct.
  4. CDR consumer (consumer.go:64-79): add `IMSI string \`json:"imsi,omitempty"\`` field to `sessionEvent` struct. In `handleEvent`, after unmarshal, if `evt.IMSI != ""` and `!validator.IsIMSIFormatValid(evt.IMSI)`: `c.logger.Warn().Str("imsi", evt.IMSI).Msg("cdr event: malformed imsi — dropping"); metric++; return`. Strict-only when `IMSIStrictValidation=true` (wire config into Consumer struct via constructor).
  5. Metric: register `argus_imsi_invalid_total` counter vector with label `source` ∈ {radius_auth, radius_acct, api_sim, cdr}. Add to `metrics.go` alongside existing counters.
- **Verify:**
  - `go build ./...` clean.
  - `go vet ./...` clean.
  - Unit test added to `server_test.go`: Access-Request with IMSI="abc" → Access-Reject reason="INVALID_IMSI_FORMAT".
  - Unit test added to `handler_test.go`: POST /sims with imsi="abc" → 400 + code=`INVALID_IMSI_FORMAT`.
  - Unit test added to `consumer_test.go`: event with imsi="abc" → no CDR row created, metric counter++.
  - All tests pass.

### Task 5 — Framed-IP pool validation (AC-3)

- **Files:** Modify `internal/aaa/session/session.go` (add `validateFramedIP` method + call in `Create`), `internal/store/ippool.go` (add `ListByAPN` method). Add unit tests in `internal/aaa/session/session_test.go`.
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/session/session.go:113-163` (existing `Manager.Create`) for the DB-backed path; read `internal/store/ippool.go:335-349` (`GetByID`) for store-method signature style.
- **Context refs:** "Service-Layer framed_ip Validation (AC-3)"
- **What:**
  1. Store: add `IPPoolStore.ListByAPN(ctx, tenantID, apnID uuid.UUID) ([]IPPool, error)`. Query `SELECT ippoolColumns FROM ip_pools WHERE tenant_id=$1 AND apn_id=$2 AND state='active'`.
  2. Manager: add helper `(m *Manager) validateFramedIP(ctx context.Context, sim *store.SIM, framedIP string) (ok bool, reason string)` implementing the 5-step logic in the "Service-Layer framed_ip Validation" section.
  3. Manager must be extended to hold `ipPoolStore *store.IPPoolStore` — add `WithIPPoolStore` ManagerOption mirroring existing `WithSIMStore` at session.go:107.
  4. In `Manager.Create` (session.go:113), BEFORE `sessionStore.Create`, resolve SIM via `m.simStore.GetByIMSIAndOperator` (or equivalent existing getter — check `internal/store/sim.go` for what's available; advisor note: if resolver is expensive, accept `sim *store.SIM` as optional arg via updated Session struct field rather than re-lookup) then call `validateFramedIP`. On `(false, reason)`: WARN log, metric `argus_framed_ip_pool_mismatch_total{reason=...}`, audit log entry; DO NOT block Create.
  5. Audit writes: reuse existing audit store if wired; otherwise emit structured log line that the daily scan (Task 6) will pick up.
  6. Metric register: `argus_framed_ip_pool_mismatch_total` counter vector with label `reason` ∈ {unparseable_framed_ip, mismatch_assigned_address, outside_apn_pools}.
- **Verify:**
  - Unit test `TestManager_Create_RejectsFramedIP_OutsidePool`: SIM with apn_id=X, pool on X has CIDR 10.0.0.0/24, session with framed_ip=192.168.1.1 → WARN log emitted, metric counter++, session IS still created.
  - Unit test `TestManager_Create_AcceptsFramedIP_InsidePool`: same setup but framed_ip=10.0.0.50 → no warning, no metric change.
  - Unit test `TestManager_Create_MismatchAssignedAddress`: SIM has ip_address_id pointing at 10.0.0.5, session has framed_ip=10.0.0.9 → warning reason=`mismatch_assigned_address`.
  - `go test ./internal/aaa/session/... ./internal/store/...` PASS.

### Task 6 — Daily data-integrity scan job (AC-5)

- **Files:** Create `internal/job/data_integrity.go`, `internal/job/data_integrity_test.go`. Modify `cmd/argus/main.go` to register the cron entry (minimal addition).
- **Depends on:** Task 2 (CHECK constraints must exist before the scan treats their violations as anomalies)
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/orphan_session.go` in FULL — this is the exact pattern (sessionQuerier interface, Start/Stop lifecycle, Run method with SQL + log emission). Read `cmd/argus/main.go` for existing cron entry registration (grep for `AddEntry(job.CronEntry{`) + job processor registration.
- **Context refs:** "Daily Data-Integrity Scan Job (AC-5)"
- **What:**
  1. Define `DataIntegrityDetector` struct mirroring `OrphanSessionDetector` (same lifecycle methods).
  2. Implement `Run(ctx)`: runs 4 invariant queries listed in "Daily Data-Integrity Scan Job". Each returns a count; all 4 counts emit WARN log (even at 0, emit DEBUG; > 0 emits WARN). Publish metric `argus_data_integrity_violations_total{kind=...}` (4 kinds as documented). If any count > 0, create notifications: one per (tenant, kind) tuple via existing `notificationStore.Create` if wired, else structured log line sufficient for ops alerting.
  3. For violations found, insert rows into `session_quarantine` (reuse Task 1's table) with `quarantined_by='fix207_scan'` — this makes the scan results queryable as a forensic trail.
  4. Schedule: `17 3 * * *` daily (see advisor's "avoid :00 / :30" guidance — 17 minutes past 3am UTC). Register in main.go alongside existing cron entries.
  5. Bounded scan: queries use `>= NOW() - INTERVAL '24 hours'` to prevent full-table scans on 10M+ row hypertables. Document this rationale in a comment at the top of `Run`.
- **Verify:**
  - `go test ./internal/job/data_integrity_test.go` PASS.
  - Test `TestDataIntegrityDetector_Run_ReportsNegDurationCDR`: seed 3 cdr rows with duration_sec=-1 (via direct SQL bypass of CHECK — use table-level INSERT INTO a temp view OR acknowledge CHECK now blocks this — in which case test seeds quarantine rows and asserts re-ingestion). If CHECK genuinely blocks bad-data seeding, the test pivots to validating the framed_ip invariant + IMSI invariant which are policy-enforced but not DB-CHECK-enforced.
  - Metric assertion: `argus_data_integrity_violations_total{kind="imsi_malformed"}` increments by N when N sims have imsi not matching regex.
  - cmd/argus/main.go cron entry count increases by 1 (grep-level check).

### Task 7 — NAS-IP backend verify: regression test + missing-AVP metric + log (AC-7)

- **Files:** Modify `internal/aaa/radius/server.go` (add ~6 lines for log+metric on missing NAS-IP). Create `internal/aaa/radius/nas_ip_test.go` (or append to existing `server_test.go`). Register metric in `internal/observability/metrics/metrics.go`.
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/aaa/radius/server.go:772-780` (existing NAS-IP / Framed-IP extraction) for exact insertion point. Read `internal/aaa/radius/server_test.go` for RADIUS-mock test harness (UDP-less in-process Packet construction).
- **Context refs:** "AC-7 — NAS-IP Backend Verify"
- **What:**
  1. In `handleAcctStart` (server.go:772-775), replace:
     ```
     nasIP := ""
     if ip, err := rfc2865.NASIPAddress_Lookup(r.Packet); err == nil {
         nasIP = ip.String()
     }
     ```
     with:
     ```
     nasIP := ""
     if ip, err := rfc2865.NASIPAddress_Lookup(r.Packet); err == nil {
         nasIP = ip.String()
     } else {
         logger.Warn().Msg("NAS-IP AVP missing from Acct-Start — FIX-226 simulator gap")
         metrics.NASIPMissingTotal.Inc()
     }
     ```
  2. Register `argus_radius_nas_ip_missing_total` counter in `metrics.go` (no labels needed — single series).
  3. Test 1 `TestHandleAcctStart_PersistsNASIP`: construct Acct-Start packet with `rfc2865.NASIPAddress_Set(pkt, net.ParseIP("192.0.2.10"))` → dispatch → assert DB row `sessions.nas_ip = '192.0.2.10'`.
  4. Test 2 `TestHandleAcctStart_MissingNASIP_EmitsSignal`: Acct-Start packet with NAS-IP AVP absent → dispatch → assert counter value == 1. Use `prometheus/testutil.ToFloat64`.
  5. Add a line comment in server.go above the new else branch: `// FIX-207 AC-7: surface missing-AVP signal. Simulator-side fix lives in FIX-226.`
- **Verify:**
  - Both new tests pass.
  - `grep -c 'argus_radius_nas_ip_missing_total' internal/observability/metrics/metrics.go` returns 1.
  - `go build ./...` clean.

### Task 8 — Documentation: ROUTEMAP D-067 + FIX-207 story closure notes + ERROR_CODES table already updated in Task 3

- **Files:** Modify `docs/ROUTEMAP.md` (add D-067 Tech Debt row), `docs/stories/fix-ui-review/FIX-207-session-cdr-data-integrity.md` (add implementation note section documenting Option B decision + hypertable CHECK behaviour observed). Modify `docs/brainstorming/decisions.md` to record DEC-NNN (Option B for sessions duration invariant).
- **Depends on:** Task 2
- **Complexity:** low
- **Pattern ref:** Read `docs/ROUTEMAP.md` Tech Debt table tail for row format; read `docs/brainstorming/decisions.md` for DEC-NNN style.
- **Context refs:** "Problem Context & Critical Schema Clarifications"
- **What:**
  1. ROUTEMAP: add D-067 row "sessions/cdrs CHECK constraint prod cutover — plain CHECK holds ACCESS EXCLUSIVE during scan on 10M+ rows; needs runbook for large production deployment (similar to D-065 for FK NOT VALID). Target: pre-prod infrastructure."
  2. FIX-207 story file: add section "Implementation Notes" documenting the schema-gap pivot (Option B vs A) and why AC-1 was executed as `ended_at >= started_at`; also document the AC-3 derived-pool-lookup chain and AC-7 backend-only scope cut.
  3. decisions.md: DEC-NNN "FIX-207: sessions duration invariant Option B (source predicate, no denormalized column). Chosen over Option A (materialized duration_sec) because (i) duration is a derived quantity — one source of truth, (ii) avoids keeping a computed column in sync at Finalize time, (iii) reduces migration complexity on a hypertable. Trade-off: analytics queries keep the EXTRACT(EPOCH FROM ...) pattern."
- **Verify:**
  - `grep -c 'D-067' docs/ROUTEMAP.md` returns 1.
  - `grep -c 'Implementation Notes' docs/stories/fix-ui-review/FIX-207-session-cdr-data-integrity.md` returns 1.
  - decisions.md tail contains new DEC entry.

## Dependency Graph & Wave Layout

```
Wave 1 (parallel):
  Task 1 (Migration A: quarantine + retro)
  Task 3 (IMSI validator package + config + error code)
  Task 5 (framed_ip validation + ListByAPN + metric)
  Task 7 (NAS-IP verify + metric + tests)

Wave 2 (depends on Wave 1):
  Task 2 (Migration B: CHECK constraints) — depends on Task 1
  Task 4 (Wire IMSI validator into 4 call sites) — depends on Task 3

Wave 3:
  Task 6 (Daily scan job) — depends on Task 2
  Task 8 (Docs) — depends on Task 2
```

File-conflict analysis (Wave 1 parallel safety):
- Task 1 touches only new migration files → no conflict.
- Task 3 touches `internal/aaa/validator/*` (new), `internal/config/config.go`, `internal/apierr/apierr.go`, `docs/architecture/ERROR_CODES.md`.
- Task 5 touches `internal/aaa/session/session.go`, `internal/store/ippool.go`, `internal/aaa/session/session_test.go`.
- Task 7 touches `internal/aaa/radius/server.go`, `internal/aaa/radius/nas_ip_test.go` (new), `internal/observability/metrics/metrics.go`.

**Potential conflict**: Task 4 (Wave 2) will modify `server.go` + `metrics.go` + `consumer.go` + `sim/handler.go` — Task 7 (Wave 1) ALSO modifies `server.go` and `metrics.go`. Resolution: Task 7 runs BEFORE Task 4 — Wave 1 vs. Wave 2 ordering already enforces this. Both tasks add metric registrations to `metrics.go` so the file will have concurrent edits across waves, but not in the same wave. **Safe**.

## Acceptance Criteria Mapping

| AC | Implemented In | Verified By |
|---|---|---|
| AC-1 (sessions duration invariant, via Option B: `ended_at >= started_at`) | Task 2 | Migration test + `TestSessions_RejectEndedBeforeStarted` (in Task 2 verify block) |
| AC-2 (`cdrs.duration_sec >= 0` CHECK) | Task 2 | Same migration test + `TestCDRs_RejectNegativeDuration` |
| AC-3 (framed_ip ∈ SIM's APN pool set; log + audit + continue) | Task 5 | `TestManager_Create_RejectsFramedIP_OutsidePool` + 2 sibling cases |
| AC-4 (IMSI validator `^\d{14,15}$` + `IMSI_STRICT_VALIDATION` toggle + `INVALID_IMSI_FORMAT` error code) | Task 3 + Task 4 | `validator/imsi_test.go` + RADIUS reject test + SIM handler 400 test + CDR consumer drop test |
| AC-5 (daily cron scan with metric + notification) | Task 6 | `TestDataIntegrityDetector_Run_*` + cron entry registration grep |
| AC-6 (`session_quarantine` + retro cleanup, not delete) | Task 1 | Post-migration counts + idempotency test |
| AC-7 (NAS-IP extraction verified + missing-AVP signal; simulator-side = FIX-226) | Task 7 | 2 new RADIUS tests + metric registration grep |

## Story-Specific Compliance Rules

- **DB — Migration ordering**: Task 1 (quarantine + retro cleanup) MUST run before Task 2 (CHECK constraints). Filename lexical order (`20260421000001` < `20260421000002`) enforces this.
- **DB — Plain CHECK on hypertables**: plain `ADD CONSTRAINT ... CHECK` works on TimescaleDB hypertables in PG16; `NOT VALID` is rejected. Dev MUST NOT use `NOT VALID`. Header comment in Migration B documents this explicitly.
- **DB — Quarantine before delete**: Task 1 MUST INSERT into quarantine BEFORE DELETE from source. Uses `EXISTS` join against quarantine rows so the DELETE predicate is guaranteed to have matching quarantine evidence.
- **API — Error envelope**: Task 3 `INVALID_IMSI_FORMAT` follows the standard envelope with `details: [{field, value, expected}]` — mirror `INVALID_REFERENCE` row in ERROR_CODES.md.
- **Hot-path perf**: Task 5 framed_ip validation adds 1 round-trip per session-create; document in plan (see "Hot-path performance" under AC-3) + track D-068 if benchmark flags it.
- **ADR-001 (Tenant Isolation)**: Task 5 `IPPoolStore.ListByAPN` MUST filter by `tenant_id` — not just `apn_id` — to preserve tenant scoping. Already specified in method signature.
- **feedback_no_defer_seed.md**: `make db-seed` MUST pass clean after migrations land. If the new quarantine migration on a freshly-seeded DB produces any quarantined rows, that's a seed-data bug — Dev fixes seed in-story. (Expected behavior: clean seed produces zero quarantined rows.)
- **feedback_autonomous_quality.md**: No shortcuts on tests. Every task's verify block is a hard gate; if any verify fails, Dev fixes before moving on.

## Bug Pattern Warnings

- **PAT-004 (goroutine cardinality)**: Not applicable — this story has no scheduler fanout.
- **PAT-006 (shared payload struct silently omitted at construction sites)**: Task 4 adds `IMSI` field to `sessionEvent` in `cdr/consumer.go`. The publisher side at `server.go:842` already emits `"imsi": imsi`. No other construction site for `sessionEvent` exists — verified via `grep -rn 'sessionEvent{' internal/`. If Dev finds additional sites, MUST populate the new field there too.
- **PAT-007 (mutex-alone ≠ happens-before)**: Not applicable — this story has no concurrent map writes.
- **PAT-009 (nullable FK in analytics — COALESCE)**: Related. Task 6 scan query #3 joins `sessions → sims → ip_pools` on `apn_id`; `sim.apn_id` is nullable after FIX-206. Query filters `m.apn_id IS NOT NULL` explicitly to avoid both NULL-join ambiguity and pgx scan-into-string panics.
- **PAT-010 (single-flight refresh)**: Not applicable — backend-only, no browser-side token handling.

## Tech Debt (from ROUTEMAP)

- **D-062 (sessions hypertable FK deferred)**: RELATED but not unblocked by FIX-207. This story adds a CHECK (not an FK) — the FK deferral stands.
- **D-063 (cdrs hypertable FK deferred)**: Same — still OPEN post-FIX-207.
- **D-064 (operator_health_logs hypertable FK deferred)**: Unrelated — this story does not touch op_health_logs.
- **D-065 (FIX-206 prod FK cutover runbook)**: Precedent for the Warning pattern in Task 2. Still OPEN (pre-prod infrastructure).
- **D-066 (pre-existing 13 test failures from FIX-206 gate)**: Not touched by this story.
- **New debt**: **D-067** — FIX-207 Migration B plain-CHECK prod cutover runbook. Task 8 adds this row.
- **New debt (conditional)**: **D-068** — framed_ip validation hot-path cache. Track ONLY if Task 5 benchmark shows >2ms added latency per session-create on the hot path.

## Mock Retirement

No mocks affected. Story is backend + migrations + metrics + test.

## Risks & Mitigations

- **Risk 1 — Plain CHECK stalls on large `cdrs` table**: `cdrs` is a 7-day-compressed hypertable but still scans on CHECK add. Mitigation: RAISE WARNING in Task 2 alerts operators; D-067 runbook captures per-chunk CHECK-add strategy for large deployments. Dev-volume (≤10k cdrs) is fast.
- **Risk 2 — TimescaleDB rejects CHECK on hypertable**: Low — TimescaleDB docs confirm CHECK constraints propagate across chunks for the parent. If Task 2 empirically fails with `cannot add CHECK to partitioned table`, Dev falls back to per-chunk CHECK-add (iterate `SELECT chunk_name FROM timescaledb_information.chunks WHERE hypertable_name = 'sessions'` + ALTER each chunk + add constraint on parent last). Task 2 includes this as a fallback in the migration header comments.
- **Risk 3 — framed_ip validation breaks legitimate flows (NAT, double-NAT, aliases)**: Mitigation: Task 5 uses log + audit + CONTINUE (not reject). Aggregate surfaces via Task 6 scan; ops triage before hardening to reject.
- **Risk 4 — IMSI strict mode rejects test-network IMSIs (non-PLMN)**: Mitigation: `IMSI_STRICT_VALIDATION=false` env toggle preserves open mode. Task 3 default is `true` (align with production); test environments override. Test suites explicitly test BOTH modes.
- **Risk 5 — `sessionEvent` struct change in Task 4 breaks existing CDR events**: The added field `IMSI string \`json:"imsi,omitempty"\`` is additive and has `omitempty` — old events unmarshal with zero value and pass the `if evt.IMSI != ""` guard. Mitigated.
- **Risk 6 — Task 6 scan job runs on empty DB and emits spurious notifications**: Mitigated by the explicit `if count > 0` guard before notification creation. Zero counts emit DEBUG only.
- **Risk 7 — Task 5 hot-path adds latency to session create**: Measured; fallback is the SIMCache-level APN→pool cache. Tracked as D-068 only if benchmark flags it.
- **Risk 8 — Migration A running twice creates duplicate quarantine rows**: Mitigated by the `NOT EXISTS` guard on both INSERT statements in Task 1. Idempotency test in verify block.

## Pre-Validation Checklist (self-run before writing)

- [x] Min 100 lines (current: 470+ — PASS for M story, exceeds bar for L)
- [x] Min 3 tasks (current: 8 — PASS)
- [x] Required sections present (Goal, Architecture Context, Tasks, Acceptance Criteria Mapping) — PASS
- [x] Embedded schema cites migration source (core_schema.up.sql lines explicit) — PASS
- [x] AC-1 column-gap acknowledged + Option B rationale documented — PASS
- [x] AC-3 derived-pool logic spelled out step-by-step — PASS
- [x] AC-7 scoped as backend-only; simulator side = FIX-226 explicit — PASS
- [x] Hypertable CHECK behaviour documented (plain CHECK works, NOT VALID rejected) — PASS
- [x] IMSI_STRICT_VALIDATION toggle in config task (Task 3) — PASS
- [x] Quarantine-before-CHECK migration ordering enforced (Task 1 → Task 2) — PASS
- [x] Pattern refs on every task — PASS (8/8)
- [x] Context refs on every task — PASS (8/8)
- [x] At least 1 high-complexity task (Task 1, Task 2 both high) — PASS (2 high)
- [x] Wave layout identifies file conflicts (Task 7 vs Task 4 on server.go + metrics.go — enforced by wave ordering) — PASS
- [x] Bug Pattern Warnings section explicitly addresses PAT-006 (shared payload struct — sessionEvent IMSI field addition) — PASS
- [x] Tech Debt added (D-067 new; D-068 conditional) — PASS
- [x] No UI → Design Token Map not required (backend-only story) — CORRECT
