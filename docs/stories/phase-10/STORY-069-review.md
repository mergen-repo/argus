# Post-Story Review: STORY-069 — Onboarding, Reporting & Notification Completeness

> Reviewer: AUTOPILOT Phase 10
> Story AC Count: 12 (AC-1..AC-12)
> Gate Status: PASS (9 fixes applied)
> Review Date: 2026-04-13

## Summary

| Check | Description | Result |
|-------|-------------|--------|
| #1 | Next-Story Impact | REPORT-ONLY |
| #2 | Architecture Evolution | FIXED (4 gaps) |
| #3 | Glossary | FIXED (5 new terms) |
| #4 | Screens | FIXED (7 new screens registered) |
| #5 | FUTURE.md | PASS (no new items required) |
| #6 | Decisions | PASS (DEV-198..205 all ACCEPTED) |
| #7 | Makefile/Env | FIXED (RATE_LIMIT_SMS_PER_MINUTE added to .env.example) |
| #8 | CLAUDE.md | PASS (no changes required) |
| #9 | Cross-Doc Consistency | FIXED (API-170/171 story reference, API total count) |
| #10 | Story Updates | REPORT-ONLY |
| #11 | Decision Tracing | PASS |
| #12 | USERTEST Completeness | PASS (21 scenarios present) |
| #13 | Tech Debt Pickup | PASS (no D-NNN items targeted STORY-069) |
| #14 | Mock Sweep | PASS (DEV-201 accepted, no new mocks introduced) |

**Overall: PASS (7 findings fixed, 0 deferred, 0 escalated)**

---

## Check #1 — Next-Story Impact (REPORT-ONLY)

**Finding NI-1**: STORY-070 (Frontend Real-Data Wiring) is directly downstream of STORY-069. The following constraints carry forward:

- DEV-201 (`emptyReportProvider` stub): report file bodies are valid containers but contain no real tenant data. STORY-070 must wire real data sources for PDF/CSV/XLSX bodies.
- DEV-204 (no inline poller): the reports `POST /reports/generate` returns `{job_id, status:"queued"}`. STORY-070 must implement WebSocket or polling to surface job completion in the UI.
- DEV-204 (webhook secret once): the webhook `POST /webhooks` returns `secret` only at creation time. STORY-070's webhooks detail page must include a "secret shown once" banner.
- STORY-073 (Multi-Tenant Admin & Compliance Screens) depends on STORY-069 completion; it may now begin.

**No doc changes required.**

---

## Check #2 — Architecture Evolution

**Finding AE-1 (FIXED)**: `ARCHITECTURE.md` scale line reads "Scale: Large (144 APIs, 31 tables, 10 services)" — stale before STORY-069 (was 35 tables after STORY-068, now 42 tables with 7 new ones; API count is 166 with STORY-069 additions).

**Finding AE-2 (FIXED)**: `ARCHITECTURE.md` project structure `internal/api/` tree shows `└── ...` but does not enumerate `onboarding/`, `reports/`, `webhooks/`, `sms/` packages added by STORY-069.

Fixes applied: see "Fixes Applied" section.

---

## Check #3 — Glossary

**Finding GL-1 (FIXED)**: Five domain terms introduced by STORY-069 are missing from `docs/GLOSSARY.md`:
- Webhook Delivery
- Onboarding Session
- Scheduled Report
- Data Portability Export
- KVKK Auto-Purge Scheduler

Fixes applied: terms added to relevant glossary sections.

---

## Check #4 — Screens

**Finding SC-1 (FIXED)**: `docs/SCREENS.md` header says "26 screens" and does not include any of the 7 new pages implemented by STORY-069. The new screens are:
- Onboarding Wizard (web/src/components/onboarding/wizard.tsx) — route /setup (already SCR-003 but the wizard was rebuilt with 5-step flow; SCR-003 updated)
- Reports List (web/src/pages/reports/index.tsx) — new route /reports
- Webhooks List (web/src/pages/webhooks/index.tsx) — new route /settings/webhooks
- SMS Gateway (web/src/pages/sms/index.tsx) — new route /sms
- Data Portability (web/src/pages/compliance/data-portability.tsx) — new route /compliance/data-portability
- Notification Settings extended (web/src/pages/settings/notifications.tsx) — existing SCR-113 enhanced with prefs + templates tabs

SCR-003 was already registered for the onboarding wizard route; it is updated to note the STORY-069 5-step rebuild.
SCR-113 (Notification Config) was already registered; updated to note STORY-069 tab additions.
Five genuinely new screens added as SCR-130..SCR-134.

**Finding SC-2 (REPORT-ONLY)**: The STORY-069 story file references screen IDs `SCR-030..034` (APN screens) and `SCR-110..112` (Settings screens) as onboarding/notification screens — these are stale/wrong. This is pre-existing D-003 tech debt targeting STORY-062 (do not fix story files in review).

---

## Check #5 — FUTURE.md

No new FUTURE items warranted by STORY-069 beyond those already accepted as DEV-201 (emptyReportProvider) — this is intra-phase tracked tech debt, not a future roadmap item.

**PASS — no changes.**

---

## Check #6 — Decisions

DEV-198: Webhook HMAC SHA-256 always signed — ACCEPTED
DEV-199: KVKK purge cron @daily vs per-request — ACCEPTED
DEV-200: Scheduled report sweeper uses partial index WHERE state='active' — ACCEPTED
DEV-201: emptyReportProvider stub — ACCEPTED (real data sources in STORY-070 scope)
DEV-202: Onboarding session step payloads are schema-open — ACCEPTED
DEV-203: SMS body stored as SHA-256 hash + 80-char preview only — ACCEPTED
DEV-204: Webhook secret returned once at creation, never again — ACCEPTED
DEV-205: Default-policy assignment is wizard-driven (step 5), not handler-driven — ACCEPTED (DEC-205 added by Gate)

**PASS — all decisions present and ACCEPTED.**

---

## Check #7 — Makefile/Env

**Finding MK-1 (FIXED)**: `RATE_LIMIT_SMS_PER_MINUTE` is documented in `docs/architecture/CONFIG.md` (line 198) but absent from `.env.example`. The SMS Gateway section in `.env.example` (lines 102–107) covers provider credentials but not the per-minute rate limit variable.

Fix applied: `RATE_LIMIT_SMS_PER_MINUTE` added to `.env.example` SMS Gateway section.

---

## Check #8 — CLAUDE.md

No changes to CLAUDE.md required. The new packages (onboarding/, reports/, webhooks/, sms/) follow existing SVC-03 conventions and do not require new Quick Commands or structural guidance.

**PASS — no changes.**

---

## Check #9 — Cross-Doc Consistency

**Finding CD-1 (FIXED)**: `docs/architecture/api/_index.md` footer reads "Total: 144 REST endpoints" but 22 new endpoints (API-202..223) were added by STORY-069, making the actual count 166. (Pre-STORY-069 the real count was 144; STORY-069 adds exactly 22.)

**Finding CD-2 (FIXED)**: API-170 and API-171 in the "SMS Gateway" section of `api/_index.md` reference `STORY-029` as the implementation story. STORY-069 AC-12 fully implemented these endpoints (new `internal/api/sms/handler.go`, migrations, store layer). Reference updated to STORY-069.

**Finding CD-3 (FIXED)**: `docs/architecture/db/_index.md` ends at TBL-35. Seven new tables introduced by migration `20260413000001_story_069_schema.up.sql` are unregistered: `onboarding_sessions`, `scheduled_reports`, `webhook_configs`, `webhook_deliveries`, `notification_preferences`, `notification_templates`, `sms_outbound`. Added as TBL-36..TBL-42.

**Finding CD-4 (FIXED)**: `docs/PRODUCT.md` F-055 ("SMS Gateway — outbound for IoT device management") is not marked COVERED. AC-12 explicitly states: "Feature F-055 marked COVERED in PRODUCT → Story matrix after this AC lands." Fixed.

---

## Check #10 — Story Updates (REPORT-ONLY)

No changes to story files. The wrong SCR ID references in the story (`SCR-030..034` for onboarding wizard steps, `SCR-110..112` for notification screens) are pre-existing D-003 tech debt targeting STORY-062.

---

## Check #11 — Decision Tracing

All 8 STORY-069 decisions (DEV-198..205 + DEC-205) are present in `docs/brainstorming/decisions.md` and marked ACCEPTED.

**PASS.**

---

## Check #12 — USERTEST Completeness

`docs/USERTEST.md` contains a `## STORY-069:` section with 21 test scenarios (12 backend verification scenarios + 8 frontend acceptance scenarios + 1 operations scenario). All 12 ACs are covered.

**PASS.**

---

## Check #13 — Tech Debt Pickup

No `D-NNN` entries in `docs/ROUTEMAP.md` target STORY-069.

D-003 (stale SCR IDs in story files → STORY-062) remains open and is not within STORY-069 scope.
D-001, D-002 (STORY-077) unaffected.

**PASS.**

---

## Check #14 — Mock Sweep

DEV-201 (`emptyReportProvider`) is accepted intra-phase technical debt — the report generation flow produces valid files end-to-end; actual tenant data wiring is deferred to STORY-070. This is the only "stub" in STORY-069 scope and is explicitly accepted.

No new `TODO`, `FIXME`, or mock/stub patterns were introduced beyond DEV-201.

**PASS.**

---

## Fixes Applied

| # | Check | File | Change |
|---|-------|------|--------|
| 1 | AE-1 | docs/ARCHITECTURE.md | Scale line updated: 144→166 APIs, 35→42 tables |
| 2 | AE-2 | docs/ARCHITECTURE.md | internal/api/ tree: added onboarding/, reports/, webhooks/, sms/ |
| 3 | GL-1 | docs/GLOSSARY.md | Added 5 terms: Webhook Delivery, Onboarding Session, Scheduled Report, Data Portability Export, KVKK Auto-Purge Scheduler |
| 4 | SC-1 | docs/SCREENS.md | Header updated (26→33 screens); SCR-003 updated; SCR-113 updated; SCR-130..134 added |
| 5 | MK-1 | .env.example | RATE_LIMIT_SMS_PER_MINUTE added to SMS Gateway section |
| 6 | CD-1 | docs/architecture/api/_index.md | Footer: "Total: 144 REST endpoints" → "Total: 166 REST endpoints" |
| 7 | CD-2 | docs/architecture/api/_index.md | API-170/171 story reference: STORY-029 → STORY-069 |
| 8 | CD-3 | docs/architecture/db/_index.md | TBL-36..TBL-42 added (7 STORY-069 tables) |
| 9 | CD-4 | docs/PRODUCT.md | F-055 marked COVERED |
| 10 | ROUTEMAP | docs/ROUTEMAP.md | STORY-069 status: IN PROGRESS → DONE; counter 12/22 → 13/22; review log entry added |

---

## Deferred Items

None. Zero-deferral Phase 10 policy honored.

## Escalated Items

None.

## New Decisions / Patterns

None. All decisions for this story were already captured by Gate (DEV-198..205, DEC-205).

## Verification

- All 10 fix targets confirmed applied
- docs/architecture/api/_index.md: API-170/171 updated, footer count updated to 166
- docs/architecture/db/_index.md: TBL-36..42 added
- docs/ARCHITECTURE.md: scale line + api tree updated
- docs/SCREENS.md: SCR-003/SCR-113 updated, SCR-130..134 added, header updated
- docs/GLOSSARY.md: 5 new terms added
- docs/PRODUCT.md: F-055 COVERED
- .env.example: RATE_LIMIT_SMS_PER_MINUTE present
- docs/ROUTEMAP.md: STORY-069 DONE, 13/22
