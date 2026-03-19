# STORY-007: Audit Log Service — Tamper-Proof Hash Chain

## User Story
As a compliance officer, I want every state-changing operation logged with tamper-proof integrity, so that we meet ISO 27001 and BTK audit requirements.

## Description
Implement audit service (SVC-10) with append-only hash chain logging. Each entry links to previous via SHA-256 hash. Supports search, export, and integrity verification.

## Architecture Reference
- Services: SVC-10 (Audit Service)
- API Endpoints: API-140 to API-142
- Database Tables: TBL-19 (audit_logs)
- Source: docs/architecture/db/platform-services.md (TBL-19)
- Spec: docs/architecture/ALGORITHMS.md (Section 2: Audit Hash Chain), docs/architecture/ERROR_CODES.md

## Screen Reference
- SCR-090: Audit Log (docs/screens/SCR-090-audit-log.md)

## Acceptance Criteria
- [ ] Every state-changing API call creates an audit entry via NATS event
- [ ] Entry contains: tenant_id, user_id, action, entity_type, entity_id, before/after JSONB, diff, IP, user_agent, correlation_id
- [ ] Hash computed: SHA-256(tenant_id|user_id|action|entity_type|entity_id|created_at|prev_hash)
- [ ] prev_hash links to previous entry's hash (chain)
- [ ] GET /api/v1/audit-logs supports: date range, user, action, entity_type, entity_id filters
- [ ] GET /api/v1/audit-logs/verify checks hash chain integrity (last N entries)
- [ ] POST /api/v1/audit-logs/export generates CSV for date range
- [ ] Table partitioned by month (auto-partition creation)
- [ ] Pseudonymization function available for KVKK purge (replaces IMSI/MSISDN/ICCID with SHA-256 hash)

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-140 | GET | /api/v1/audit-logs | `?from&to&user_id&action&entity_type&entity_id&cursor&limit` | `[{id,user_name,action,entity_type,entity_id,diff,created_at}]` | JWT(tenant_admin+) | 200 |
| API-141 | GET | /api/v1/audit-logs/verify | `?count=100` | `{verified: bool, entries_checked: int, first_invalid: int?}` | JWT(tenant_admin+) | 200 |
| API-142 | POST | /api/v1/audit-logs/export | `{from: date, to: date}` | `{download_url: string}` | JWT(tenant_admin+) | 202 |

## Dependencies
- Blocked by: STORY-006 (NATS event bus)
- Blocks: None directly, but all subsequent stories publish audit events

## Test Scenarios
- [ ] Creating a user generates audit entry with before=null, after=user data
- [ ] Updating a user generates audit entry with before/after diff
- [ ] Hash chain is valid for 100 consecutive entries
- [ ] Tampering with one entry breaks chain verification
- [ ] Date range filter returns correct entries
- [ ] Export generates valid CSV
- [ ] Pseudonymization replaces IMSI with hash, chain remains valid

## Effort Estimate
- Size: L
- Complexity: High
