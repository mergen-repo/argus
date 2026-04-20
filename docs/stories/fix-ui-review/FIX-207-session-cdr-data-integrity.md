# FIX-207: Session/CDR Data Integrity — Negative Duration, Cross-Pool IP, IMSI Format

## Problem Statement
Observed data quality issues in sessions/CDRs:
- Negative `duration_sec` values in some session rows (clock skew / stop before start)
- Cross-pool IP assignments (SIM in pool A got IP from pool B)
- IMSI format validation gaps (15-digit PLMN compliance not enforced)
- NAS-IP empty string (F-160 — RADIUS simulator gap)

## User Story
As a data engineer, I want session and CDR records to pass invariant checks (duration ≥ 0, IP ∈ assigned pool, IMSI format valid) so analytics queries and audit trails are trustworthy.

## Architecture Reference
- Backend: `internal/store/session.go`, `internal/store/cdr.go`, `internal/aaa/session/manager.go`
- Validation: CHECK constraints + service-layer sanity checks

## Findings Addressed
F-98, F-99, F-100, F-101 (verify), F-34, F-160 (NAS-IP — linked to simulator coverage FIX-226)

## Acceptance Criteria
- [ ] **AC-1:** Sessions table CHECK constraint: `duration_sec >= 0`. Migration blocks negative values.
- [ ] **AC-2:** Same for `cdrs.duration_sec >= 0`.
- [ ] **AC-3:** Session create path validates `framed_ip` belongs to SIM's assigned `ip_pool_id`. If mismatch: log warning + reject OR auto-correct with audit entry.
- [ ] **AC-4:** IMSI format validator: `^\d{14,15}$` (MCC 3 + MNC 2-3 + MSIN 9-10). Reject auth/session creation for malformed IMSI with explicit error code.
- [ ] **AC-5:** Data integrity cron job (daily): scans recent sessions/CDRs, reports invariant violations. Surfaces to ops as metric + notification.
- [ ] **AC-6:** Retro cleanup: one-time script to quarantine (not delete) existing negative-duration rows into `session_quarantine` table for investigation.
- [ ] **AC-7:** NAS-IP propagation — RADIUS handler extracts NAS-IP-Address AVP (RFC 2865 §5.4) and persists on sessions. Simulator also sends it (FIX-226 scope).

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_session_cdr_invariants.up.sql`
- `internal/store/session.go`, `cdr.go` — service-layer validation
- `internal/aaa/radius/server.go` — NAS-IP extraction (if missing)
- `internal/job/data_integrity.go` — daily scan
- Simulator coordination with FIX-226

## Risks & Regression
- **Risk 1 — Existing invalid data:** AC-6 quarantine first, then add CHECK constraint.
- **Risk 2 — IMSI validator breaks legitimate non-standard IMSI:** Some test networks use non-PLMN IMSI. Provide config toggle `IMSI_STRICT_VALIDATION=true|false`.

## Test Plan
- Unit: invariant checks for each rule
- Integration: RADIUS Access-Request with bogus IMSI → reject with known error code
- Regression: historical session data survives migration (quarantine works)

## Plan Reference
Priority: P0 · Effort: M · Wave: 1

## Implementation Notes (post-dev, 2026-04-20)

### AC-1 schema-gap pivot: Option B (source predicate, no denormalized column)

During planning (2026-04-20), discovered that `sessions` has NO `duration_sec` column — the table stores `started_at + ended_at` and duration is derived at query time via `EXTRACT(EPOCH FROM (ended_at - started_at))`. AC-1 as literally written ("Sessions table CHECK constraint: duration_sec >= 0") was infeasible without adding a denormalized column.

**Decision**: Option B — use the equivalent invariant `ended_at IS NULL OR ended_at >= started_at` as the CHECK constraint. Rationale:
1. Duration is a derived quantity — enforcing it at source (start/end ordering) is cleaner than materializing.
2. Avoids a computed column that must be kept in sync with `started_at`/`ended_at` at every Finalize.
3. Reduces migration complexity on a TimescaleDB hypertable.

Trade-off: analytics queries retain the `EXTRACT(EPOCH FROM ...)` pattern; no new query rewrites needed.

Full rationale recorded as DEV-268 in `docs/brainstorming/decisions.md`.

### AC-3 derived pool lookup

`sims` has no `ip_pool_id` column. Validation traverses:
- `sim.ip_address_id → ip_addresses.pool_id` (for SIMs with static IP), else
- `sim.apn_id → ip_pools.apn_id` (via new `IPPoolStore.ListByAPN`), matching framed_ip against `cidr_v4` / `cidr_v6` containment.

Policy = log + audit + continue (NOT reject): rejecting live AAA on framed_ip mismatch causes operator incidents; the daily scan (AC-5) surfaces aggregates for forensics.

### AC-7 backend-only scope

NAS-IP extraction at `server.go:~773` already existed. This story adds:
- Missing-AVP signal: `argus_radius_nas_ip_missing_total` counter + WARN log in the `else` branch.
- Regression tests (`TestHandleAcctStart_PersistsNASIP`, `TestHandleAcctStart_MissingNASIP_EmitsSignal`).

Simulator-side NAS-IP injection is explicitly OUT OF SCOPE — lives in FIX-226 (Simulator Coverage). The counter is the closure signal FIX-226 uses to verify its fix.

### New tech debt

- **D-067** — Migration B plain-CHECK prod cutover runbook (tracked in ROUTEMAP).
- **D-068** (conditional) — framed_ip validation hot-path cache. Track ONLY if post-dev benchmark shows >2ms added latency per session-create.

### Migrations applied

- `migrations/20260421000001_session_quarantine.up.sql` — Migration A (quarantine table + retro cleanup, 131 bad sessions + 0 bad cdrs quarantined on live dev DB at 2026-04-20).
- `migrations/20260421000002_session_cdr_invariants.up.sql` — Migration B (CHECK constraints on sessions + cdrs, propagated across all hypertable chunks).

Rejection probes confirmed (live DB): INSERT with `ended_at < started_at` → PG error 23514; INSERT with `duration_sec=-5` → PG error 23514.

### Post-migration reconciliation (Gate F-A5, 2026-04-20)

The two migrations above were applied via direct `psql` during development. `schema_migrations` tracking was consequently bypassed, so a follow-up run of `make db-migrate` would attempt to re-apply them (the DO-block guards make the re-apply a no-op, but the `schema_migrations` row set remains inconsistent until reconciled).

Operators should reconcile with ONE of the following — pick whichever fits the deployment runbook:

1. Re-run the standard migrator (idempotent DO blocks absorb the replay):
   ```
   make db-migrate
   ```
2. OR insert the two version rows directly:
   ```sql
   INSERT INTO schema_migrations (version, dirty) VALUES
     (20260421000001, false),
     (20260421000002, false)
   ON CONFLICT DO NOTHING;
   ```

Both are safe; option 2 is preferred when the DB holds ≥100k sessions and you want to avoid the `ALTER TABLE ADD CONSTRAINT` re-scan on the hypertable.

### Gate follow-ups (2026-04-20)

- `main.go` NewManager wiring completed (was missing at 3 sites → AC-3 framed_ip validation was runtime no-op at dispatch time). Fixed during Gate. Verified via `go build ./...`.
- Helper-level NAS-IP test (`TestExtractNASIPFromPacket_ReturnsIP`) renamed to reflect actual scope; end-to-end persistence coverage tracked as D-071 in ROUTEMAP.
- Diameter Gx/Gy and 5G SBA IMSI validator coverage is out of scope for FIX-207 — plan explicitly scoped to 4 call sites (RADIUS auth/acct, SIM handler, CDR consumer). Diameter/SBA coverage is deferred to a follow-up story (not ROUTEMAP-tracked — will be captured when the containing initiative is scoped).
- `DataIntegrityDetector` quarantine surface for IMSI violations and notification-store wiring are tracked as D-069 and D-070 respectively.
