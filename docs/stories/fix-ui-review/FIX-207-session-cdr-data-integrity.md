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
