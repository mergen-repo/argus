# Deliverable: STORY-039 — Compliance Reporting (BTK, KVKK, GDPR)

## Summary

Implemented compliance framework with auto-purge job, audit log pseudonymization, KVKK/GDPR data subject access/erasure, BTK reporting, and compliance dashboard. Per-tenant configurable retention (30-365 days). Hash chain verification before pseudonymization. Purge sweep replaces stub processor.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/store/compliance.go` | ComplianceStore: purge queries, BTK stats, DSAR export, pseudonymization |
| `internal/store/compliance_test.go` | Hash determinism, salt uniqueness tests |
| `internal/compliance/service.go` | Compliance service: purge sweep, DSAR, erasure, dashboard, BTK report |
| `internal/compliance/service_test.go` | Service tests |
| `internal/job/purge_sweep.go` | PurgeSweepProcessor: replaces stub, batch purge with audit entries |
| `internal/job/purge_sweep_test.go` | Processor tests |
| `internal/api/compliance/handler.go` | REST handler: 5 compliance endpoints |
| `internal/api/compliance/handler_test.go` | Handler validation tests |

### Modified Files
| File | Change |
|------|--------|
| `internal/gateway/router.go` | 5 compliance routes (tenant_admin) |
| `cmd/argus/main.go` | Replaced purge_sweep stub, wired compliance service |

## API Endpoints
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/compliance/dashboard` | tenant_admin | Compliance dashboard |
| GET | `/api/v1/compliance/btk-report` | tenant_admin | BTK monthly report (JSON/CSV) |
| PUT | `/api/v1/compliance/retention` | tenant_admin | Update retention period |
| GET | `/api/v1/compliance/dsar/:simId` | tenant_admin | Data subject access request |
| POST | `/api/v1/compliance/erasure/:simId` | tenant_admin | Right to erasure |

## Key Features
- Auto-purge: daily cron, TERMINATED → PURGED with SHA-256 hash anonymization
- Per-tenant salt for one-way pseudonymization
- Hash chain verification before pseudonymization
- KVKK/GDPR: DSAR JSON export, right to erasure with immediate purge
- BTK: monthly SIM statistics per operator with CSV export
- Gate fixes: error message leak, per-tenant retention, chain check in purge sweep

## Test Coverage
- 19 new tests, 963 total passing, 0 regressions
