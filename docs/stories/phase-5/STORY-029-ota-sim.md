# STORY-029: OTA SIM Management via APDU Commands

## User Story
As a SIM manager, I want to send Over-The-Air (OTA) commands to SIMs for remote file/applet management, so that I can update SIM configurations without physical access.

## Description
OTA SIM management using APDU (Application Protocol Data Unit) commands sent via SMS-PP or BIP (Bearer Independent Protocol). Supports remote file update, applet installation/deletion, and SIM toolkit commands. Bulk OTA operations run as background jobs via SVC-09 (Job Runner). OTA commands queued per SIM with delivery tracking.

## Architecture Reference
- Services: SVC-03 (Core API — OTA command creation), SVC-09 (Job Runner — bulk OTA execution)
- Database Tables: TBL-10 (sims), TBL-20 (jobs — type='ota_command')
- Packages: internal/api/ota, internal/job/ota
- Source: docs/architecture/services/_index.md (SVC-03, SVC-09)

## Screen Reference
- SCR-021: SIM Detail — OTA command history
- SCR-080: Job List — OTA bulk jobs

## Acceptance Criteria
- [ ] OTA command types: UPDATE_FILE, INSTALL_APPLET, DELETE_APPLET, READ_FILE, SIM_TOOLKIT
- [ ] Single SIM OTA: send APDU command to specific SIM, track delivery status
- [ ] Bulk OTA: send same command to segment of SIMs via job runner
- [ ] APDU command builder: construct valid APDU byte sequences for common operations
- [ ] OTA delivery via SMS-PP: encode APDU in SMS envelope (GSM 03.48)
- [ ] OTA delivery via BIP: TCP/IP bearer channel (when SIM supports it)
- [ ] Delivery status tracking: queued → sent → delivered → executed → confirmed / failed
- [ ] OTA security: SIM OTA keys (KIC, KID) used for encryption and MAC
- [ ] Bulk OTA job: partial success handling, retry failed SIMs, error report
- [ ] Rate limiting: max N OTA commands per SIM per hour (configurable)
- [ ] OTA command history stored per SIM with timestamp, command type, status, response

## Dependencies
- Blocked by: STORY-011 (SIM CRUD), STORY-031 (job runner for bulk OTA)
- Blocks: None

## Test Scenarios
- [ ] Send UPDATE_FILE OTA to single SIM → command queued, delivery tracked
- [ ] Bulk OTA to 1000 SIMs → job created, progress tracked
- [ ] OTA delivery confirmed → status=confirmed
- [ ] OTA delivery failed → status=failed, error reason logged
- [ ] APDU builder produces valid byte sequence for UPDATE_FILE
- [ ] OTA rate limit exceeded → 429 OTA_RATE_LIMIT
- [ ] Bulk OTA partial failure → job completes with error report
- [ ] OTA command history for SIM shows all past commands

## Effort Estimate
- Size: L
- Complexity: High
