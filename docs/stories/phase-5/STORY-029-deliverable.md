# Deliverable: STORY-029 — OTA SIM Management via APDU Commands

## Summary

Implemented Over-The-Air (OTA) SIM management via APDU commands with SMS-PP and BIP delivery channels. Supports 5 command types (UPDATE_FILE, INSTALL_APPLET, DELETE_APPLET, READ_FILE, SIM_TOOLKIT), single and bulk OTA operations, delivery status tracking, OTA security (KIC/KID encryption + MAC), and per-SIM rate limiting.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/ota/types.go` | OTA domain types, command types, delivery channels, status enum |
| `internal/ota/apdu.go` | APDU command builder — constructs valid byte sequences per command type |
| `internal/ota/delivery.go` | SMS-PP (GSM 03.48) and BIP delivery channel encoding |
| `internal/ota/ratelimit.go` | Redis-based per-SIM rate limiting |
| `internal/ota/security.go` | OTA security — KIC/KID encryption (AES-CBC), MAC computation |
| `internal/api/ota/handler.go` | HTTP handler — 4 endpoints for OTA command management |
| `internal/job/ota.go` | OTA bulk job processor — replaces stub from STORY-031 |
| `internal/store/ota.go` | PostgreSQL store — CRUD for ota_commands table |
| `migrations/20260321000002_ota_commands.up.sql` | Create ota_commands table with 5 indexes |
| `migrations/20260321000002_ota_commands.down.sql` | Drop ota_commands table |
| `internal/ota/types_test.go` | Tests for types, validation, serialization |
| `internal/ota/apdu_test.go` | Tests for APDU builder — all 5 command types |
| `internal/ota/security_test.go` | Tests for encryption, MAC, all security modes |
| `internal/ota/delivery_test.go` | Tests for SMS-PP/BIP encoding |
| `internal/ota/ratelimit_test.go` | Tests for rate limiter configuration |
| `internal/store/ota_test.go` | Tests for store structs and filters |
| `internal/job/ota_test.go` | Tests for OTA job processor |

### Modified Files
| File | Change |
|------|--------|
| `cmd/argus/main.go` | Wire OTAHandler into RouterDeps, register real OTA processor (replace stub) |
| `internal/gateway/router.go` | Register 4 OTA routes under `/api/v1/ota/` |

## Architecture References Fulfilled
- SVC-03 (Core API): OTA command creation endpoints
- SVC-09 (Job Runner): Bulk OTA execution via job processor
- TBL-10 (sims): SIM reference for OTA commands
- TBL-20 (jobs): OTA bulk jobs with type='ota_command'

## API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/ota/commands` | Send OTA command to single SIM |
| GET | `/api/v1/ota/commands` | List OTA commands (filterable by SIM, type, status) |
| GET | `/api/v1/ota/commands/:id` | Get OTA command details |
| POST | `/api/v1/ota/commands/bulk` | Send bulk OTA command to segment |

## Test Coverage
- 78 OTA-specific tests across 7 test files
- 784 total tests passing, 0 failures, 0 regressions
- All 11 acceptance criteria covered
