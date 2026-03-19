# STORY-039: Compliance Reporting (BTK, KVKK, GDPR)

## User Story
As a tenant admin, I want automated compliance reporting and data lifecycle management for BTK, KVKK, and GDPR regulations, so that the platform meets legal requirements for data retention and privacy.

## Description
Compliance framework: auto-purge job (TERMINATED → PURGED after tenant-configurable retention period), pseudonymization of audit logs after retention, compliance dashboard showing data retention status, KVKK/GDPR data subject access requests, and BTK reporting templates. Scheduled job handles data lifecycle transitions.

## Architecture Reference
- Services: SVC-10 (Audit Service — pseudonymization), SVC-09 (Job Runner — purge job)
- Database Tables: TBL-19 (audit_logs), TBL-10 (sims — state PURGED), TBL-01 (tenants — purge_retention_days)
- Source: docs/architecture/services/_index.md (SVC-09, SVC-10)

## Screen Reference
- SCR-090: Audit Log — compliance filter, pseudonymized view
- SCR-121: Tenant Management — retention period configuration

## Acceptance Criteria
- [ ] Auto-purge scheduled job: runs daily, finds TERMINATED SIMs where purge_at < NOW()
- [ ] Purge action: SIM state → PURGED, personal data anonymized (IMSI→hash, MSISDN→hash)
- [ ] Purge preserves: anonymized usage aggregates, CDR summaries (no personal identifiers)
- [ ] Audit log pseudonymization: after retention_days, replace user identifiers with pseudonyms
- [ ] Pseudonymization is one-way (irreversible hash with per-tenant salt)
- [ ] KVKK/GDPR data subject access: export all data for a specific SIM/user as JSON
- [ ] KVKK/GDPR right to erasure: trigger early purge for specific SIM (override retention)
- [ ] BTK reporting template: generate monthly SIM statistics report (active count, operator breakdown)
- [ ] Compliance dashboard: show total SIMs per state, pending purges, retention compliance %
- [ ] Tenant-configurable retention period: purge_retention_days (default 90, min 30, max 365)
- [ ] Audit log hash chain verified before pseudonymization (tamper check)
- [ ] Purge job creates audit entry (system-initiated) for each purged SIM
- [ ] Compliance report exportable as PDF/CSV

## Dependencies
- Blocked by: STORY-007 (audit log), STORY-011 (SIM state machine — PURGED state), STORY-031 (job runner for scheduled purge)
- Blocks: None

## Test Scenarios
- [ ] Purge job: SIM terminated 91 days ago (retention=90) → purged, data anonymized
- [ ] Purge job: SIM terminated 89 days ago → not purged (within retention)
- [ ] Anonymized SIM: IMSI replaced with SHA-256 hash, MSISDN replaced
- [ ] Audit log pseudonymization: user email replaced with pseudonym after retention
- [ ] Data subject access request → JSON export with all SIM data
- [ ] Right to erasure → SIM purged immediately, confirmation generated
- [ ] BTK monthly report → correct SIM counts per operator
- [ ] Compliance dashboard → accurate counts, pending purge count
- [ ] Hash chain verification failure → pseudonymization blocked, alert generated
- [ ] Tenant retention=30 → purge at 31 days after termination

## Effort Estimate
- Size: L
- Complexity: High
