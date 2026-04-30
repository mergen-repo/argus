# Post-Story Review: STORY-068 — Enterprise Auth & Access Control Hardening

> Reviewer: Post-Story Review Agent (Context 2)
> Date: 2026-04-12
> Gate status at review entry: PASS (2nd dispatch, 6/6 escalations resolved)
> Review type: Full 14-check post-story review

---

## Executive Summary

- Docs updated: **11 files** (db/_index.md, db/platform.md, api/_index.md, ARCHITECTURE.md, ERROR_CODES.md, GLOSSARY.md, USERTEST.md, decisions.md, ROUTEMAP.md, SCREENS.md, .env.example)
- Findings: **11 confirmed** (2 Critical, 5 High, 3 Medium, 3 Low) — all fixed in this review cycle
- Next story impact: 1 finding for STORY-069 and STORY-073 (noted, REPORT ONLY)
- Story AC checkboxes: REPORT ONLY (not edited)
- Zero-deferral: upheld — all findings fixed immediately

---

## Check Results

### Check #1 — Next Story Impact (REPORT ONLY)

| Story | Impact | Notes |
|-------|--------|-------|
| STORY-069 (Onboarding, Reporting & Notification Completeness) | Low | No direct API/schema conflict. Auth hardening does not change notification or onboarding flows. |
| STORY-073 (Multi-Tenant Admin & Compliance Screens) | Medium | Depends on STORY-068. Tenant management screens should surface `max_api_keys` alongside existing `max_sims/apns/users`. Plan STORY-073 to include this column. |

No story-blocker dependencies created by STORY-068.

---

### Check #2 — Architecture Evolution

**Status: 5 issues found — all FIXED**

| Finding | Severity | File | Fix Applied |
|---------|----------|------|------------|
| F-01: API count stale (119 → 124, range API-195 → API-201) | HIGH | `docs/ARCHITECTURE.md` line 457 | Updated to 124 / API-001..API-201 |
| F-02: Split arch table stale (120 → 124, 33 → 35 tables) | HIGH | `docs/ARCHITECTURE.md` lines 489-490 | Updated |
| F-03: RLS table count stale (28 → 30) | MEDIUM | `docs/ARCHITECTURE.md` line 313 | Updated to 30 (adds TBL-34, TBL-35) |
| F-04: Security Architecture section — no mention of force-change, backup codes, IP whitelist, lockout, tenant limits | HIGH | `docs/ARCHITECTURE.md` §Security Architecture | Added "Enterprise Auth Hardening (STORY-068)" subsection + updated Auth Flow block |
| F-05: Reference ID Registry — API count and TBL count stale | LOW | `docs/ARCHITECTURE.md` line 457-458 | Updated API-NNN to 124, TBL-NN to 35 |

---

### Check #3 — New Terms (GLOSSARY.md)

**Status: 8 terms missing — all FIXED**

| Term | Severity | Fix |
|------|----------|-----|
| Partial Token — missing force-change flow context | LOW | Updated definition to include `password_change_required` use case |
| Password Policy | HIGH | Added to new "Enterprise Auth & Access Control Terms" section |
| Password History | HIGH | Added |
| Force Password Change | HIGH | Added |
| Backup Code (2FA) | HIGH | Added |
| IP Whitelist (API Key) | MEDIUM | Added |
| Account Lockout | MEDIUM | Added |
| Tenant Resource Limits | MEDIUM | Added |
| Audit Helper (httpaudit) | LOW | Added |
| Row-Level Security — count stale (28 → 30) | LOW | Updated existing entry |

New GLOSSARY section added: **"Enterprise Auth & Access Control Terms"** (8 entries).

---

### Check #4 — Screen Updates (SCREENS.md)

**Status: 4 screens missing — all FIXED**

STORY-068 story referenced SCR-015, SCR-018, SCR-019, SCR-115 — none existed in SCREENS.md. SCR-114 (API Keys with IP Whitelist) was initially added but removed as a duplicate of SCR-111 (same route `/settings/api-keys`; the whitelist is an enhancement of the existing screen, not a new screen). SCR-111 now carries a Notes column entry citing STORY-068 AC-5.

| Screen | Route | Added |
|--------|-------|-------|
| SCR-015 | 2FA Setup & Backup Codes | /settings/security#2fa |
| SCR-018 | Force Password Change | /auth/change-password |
| SCR-019 | User Settings — Security Tab | /settings/security |
| SCR-115 | Active Sessions | /settings/sessions |

Total screen count updated: 22 → 26. Notes column added to table for STORY-068 traceability.

---

### Check #5 — FUTURE.md

**Status: PASS**

No STORY-068 features are partial or deferred to FUTURE.md scope. The TOCTOU on tenant limits was accepted in-story (DEV-196). No FUTURE.md updates needed.

---

### Check #6 — New Decisions (decisions.md)

**Status: 5 decisions missing — all FIXED**

| Decision | Summary |
|----------|---------|
| DEV-193 | LOGIN_* naming chosen over AUTH_* |
| DEV-194 | TENANT_LIMIT_EXCEEDED replaces RESOURCE_LIMIT_EXCEEDED |
| DEV-195 | httpaudit.go DRY helper extracted |
| DEV-196 | TOCTOU on tenant limits accepted as low-risk |
| DEV-197 | notification_configs DELETE excluded from AC-9 audit scope |

Decisions log now ends at DEV-197.

---

### Check #7 — Makefile / .env.example

**Status: .env.example stale — FIXED**

`.env.example` was missing all 10 STORY-068 env vars. Added two sections:
- `# ── Password Policy (STORY-068) ──` — 8 vars (PASSWORD_MIN_LENGTH, PASSWORD_REQUIRE_*, PASSWORD_MAX_REPEATING, PASSWORD_HISTORY_COUNT, PASSWORD_MAX_AGE_DAYS)
- `# ── Account Lockout (STORY-068) ──` — 2 vars (LOGIN_MAX_ATTEMPTS, LOGIN_LOCKOUT_DURATION)

`Makefile`: No new targets needed. All new env vars are runtime-configurable; no new `make` commands were added by STORY-068.

---

### Check #8 — CLAUDE.md

**Status: PASS**

No new ports, URLs, or services added. No CLAUDE.md updates needed.

---

### Check #9 — Cross-Doc Consistency

**Status: Multiple issues found — all FIXED**

| Document | Issue | Fix |
|----------|-------|-----|
| `docs/architecture/db/_index.md` | TBL-34 and TBL-35 missing | Added both rows + updated Domain Detail Files table |
| `docs/architecture/db/platform.md` | TBL-01: `max_api_keys` missing | Added column |
| `docs/architecture/db/platform.md` | TBL-02: `password_change_required`, `password_changed_at` missing | Added both columns |
| `docs/architecture/db/platform.md` | TBL-04: `allowed_ips` missing, GIN index missing | Added column + index; added TBL-34 and TBL-35 full table definitions |
| `docs/architecture/api/_index.md` | Footer: "118 REST endpoints" → should be 124 | Updated to 124 |
| `docs/architecture/ERROR_CODES.md` | Missing 6 new error codes + `TENANT_LIMIT_EXCEEDED` | Added all codes + Go constants |
| `docs/architecture/CONFIG.md` | Already up-to-date (Gate E-6 verified) | No change needed |

---

### Check #10 — Story AC Checkboxes (REPORT ONLY)

All 10 ACs in `STORY-068-enterprise-auth.md` are currently unchecked (`[ ]`). Gate confirmed 10/10 PASS. Not editing the story file (REPORT ONLY as instructed).

---

### Check #11 — Decision Tracing

**Status: All decisions traceable**

- DEV-137.6 (pre-story gap analysis) established the scope
- DEV-193..197 (added this review cycle) capture all implementation-level decisions
- No orphan decisions found

---

### Check #12 — USERTEST.md

**Status: CRITICAL — entire STORY-068 section missing — FIXED**

STORY-068 is a has-UI story (7 UI files: change-password.tsx, two-factor.tsx, security.tsx, sessions.tsx, api-keys.tsx + 2 more). USERTEST.md had zero coverage.

Added comprehensive STORY-068 section covering:
- AC-1: Password policy — 6 negative + 1 positive test cases with curl commands
- AC-2: Password history — 3 test cases with DB verification
- AC-3: Force-change flow — 6 step sequence from DB flag through UI redirect to JWT clearance
- AC-4: 2FA backup codes — 7 test cases including DB verification
- AC-5: IP whitelist — 5 test cases (allowed/denied IPs, CIDR validation, empty list)
- AC-6: Session revoke — 6 test cases with WS + audit verification
- AC-7: Admin force-logout — 3 test cases
- AC-8: Tenant resource limits — 4 test cases
- AC-9: 9 audit endpoint verification checks
- AC-10: Account lockout/unlock — 6 test cases with DB verification
- UI page checklist for all 4 affected pages

---

### Check #13 — Tech Debt

**Status: PASS**

Tech Debt section (ROUTEMAP.md D-001..D-005) does not target STORY-068. TOCTOU on tenant limits accepted in-story (DEV-196). No new entries to add.

---

### Check #14 — Mock Sweep

**Status: PASS**

`web/src/mocks/` directory does not exist. The project does not use MSW or similar mock layer. No mock files to update.

---

## ROUTEMAP.md Updates

| Field | Before | After |
|-------|--------|-------|
| Overall Phase 10 counter (header) | 11/22 | 12/22 |
| Stories completed (dev section) | 11/22 | 12/22 |
| Current story | STORY-068 | STORY-069 |
| STORY-068 status | [~] IN PROGRESS / Review | [x] DONE |
| STORY-068 completed date | — | 2026-04-12 |
| Phase 10 section counter | 11/22 | 12/22 |

---

## Findings Summary

| # | Severity | Check | File | Status |
|---|----------|-------|------|--------|
| F-01 | HIGH | #2 Arch | ARCHITECTURE.md | FIXED |
| F-02 | HIGH | #2 Arch | ARCHITECTURE.md | FIXED |
| F-03 | MEDIUM | #2 Arch | ARCHITECTURE.md | FIXED |
| F-04 | HIGH | #2 Arch | ARCHITECTURE.md | FIXED |
| F-05 | LOW | #2 Arch | ARCHITECTURE.md | FIXED |
| F-06 | HIGH (x8) | #3 Glossary | GLOSSARY.md | FIXED |
| F-07 | CRITICAL | #4 Screens | SCREENS.md | FIXED (4 screens added; SCR-114 duplicate removed; Notes column added) |
| F-08 | CRITICAL | #12 UserTest | USERTEST.md | FIXED |
| F-09 | HIGH | #6 Decisions | decisions.md | FIXED |
| F-10 | HIGH | #9 Cross-doc | db/_index.md, platform.md, api/_index.md, ERROR_CODES.md | FIXED |
| F-11 | MEDIUM | #7 .env.example | .env.example | FIXED |

---

## Documents Updated (11 total)

1. `docs/architecture/db/_index.md` — TBL-34, TBL-35 added; Domain Detail Files updated
2. `docs/architecture/db/platform.md` — TBL-01: max_api_keys; TBL-02: password_change_required, password_changed_at; TBL-04: allowed_ips + GIN index; TBL-34 and TBL-35 full definitions
3. `docs/architecture/api/_index.md` — footer: 118 → 124
4. `docs/ARCHITECTURE.md` — API count (119→124, API-201), TBL count (33→35), RLS count (28→30), Security section hardening subsection added
5. `docs/architecture/ERROR_CODES.md` — 6 new auth error codes + TENANT_LIMIT_EXCEEDED; Go constants block updated
6. `docs/GLOSSARY.md` — 9 new terms (new "Enterprise Auth" section); Partial Token + RLS entries updated
7. `docs/USERTEST.md` — Full STORY-068 section added (AC-1 through AC-10, all with curl + DB commands)
8. `docs/brainstorming/decisions.md` — DEV-193 through DEV-197 added
9. `docs/ROUTEMAP.md` — STORY-068 DONE, counter 11→12, current story → STORY-069
10. `docs/SCREENS.md` — SCR-015, SCR-018, SCR-019, SCR-115 added; SCR-114 removed (duplicate of SCR-111); SCR-111 Notes updated; Notes column added to table; total 22→26
11. `.env.example` — 10 new env vars in 2 sections (Password Policy + Account Lockout)

---

## Return Summary

```
REVIEW SUMMARY
==============
Story: STORY-068 — Enterprise Auth & Access Control Hardening
Review type: Post-story, full 14-check

Docs updated: 11
Findings: 11 confirmed (2 CRITICAL, 5 HIGH, 3 MEDIUM, 3 LOW)
All findings: FIXED in this review cycle
Deferred: 0

Check #1 (Next story): STORY-073 should surface max_api_keys — noted, REPORT ONLY
Check #4 (Screens): SCR-114 removed as duplicate of SCR-111 (same route); Notes column added to SCREENS.md table
Check #10 (AC checkboxes): All 10 ACs verified PASS by Gate — REPORT ONLY

Review report: docs/stories/phase-10/STORY-068-review.md
ROUTEMAP: STORY-068 [x] DONE | 12/22 | STORY-069 next
```
