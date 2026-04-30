# FIX-211: Severity Taxonomy Unification (critical/high/medium/low/info)

## Problem Statement
Severity values diverge across systems:
- Anomalies/alerts: critical/high/medium/low/info (5 levels)
- Violations: critical/warning/info (3 levels)
- Notifications settings UI: warning/error/info
- SLA: multiple

Inconsistent filtering, aggregation, and alert routing.

## User Story
As an SRE, I want a single unified severity taxonomy so alerts, violations, and notifications sort and filter consistently.

## Architecture Reference
- DB: multiple `severity` columns with different enums
- Postgres CHECK constraints to enforce new enum

## Findings Addressed
F-37, F-167 (violations severity 3-level gap)

## Acceptance Criteria
- [ ] **AC-1:** Canonical enum: `critical | high | medium | low | info` (5 levels, documented order).
- [ ] **AC-2:** Migration adds CHECK constraint to: `anomalies.severity`, `alerts.severity` (FIX-209), `policy_violations.severity`, `notifications.severity`.
- [ ] **AC-3:** Existing violations severity mapping: `warning → medium`, `info → info`, `critical → critical`. Data migration script.
- [ ] **AC-4:** Backend validators (handlers) accept only the 5 values; return 400 on others.
- [ ] **AC-5:** FE UI dropdowns (Alerts, Violations, Notifications Prefs) use consistent 5-value list with uniform color coding: red/orange/yellow/blue/gray.
- [ ] **AC-6:** `docs/architecture/ERROR_CODES.md` — severity taxonomy section.
- [ ] **AC-7:** Report builders (FIX-248 SLA, Audit Log) use consistent severity filter.

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_unify_severity.up.sql`
- `internal/store/anomaly.go`, `violation.go`, `notification/service.go`, `alert.go`
- `web/src/components/severity-badge.tsx` (NEW common component)
- All filter dropdowns

## Risks & Regression
- **Risk 1 — Breaking API consumers:** Mitigation — accept both old+new during 1 release cycle, log warnings.
- **Risk 2 — Visual regression:** New color palette shipped across all badges simultaneously.

## Test Plan
- Unit: enum validation per handler
- Integration: severity=warning → 400 after migration
- Browser: all severity chips use same color across pages

## Plan Reference
Priority: P0 · Effort: M · Wave: 2
