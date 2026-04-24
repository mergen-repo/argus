# Post-Story Review: FIX-228 — Login Forgot Password Flow + Version Footer

> Date: 2026-04-25

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-229 | Alert Feature Enhancements — different domain (alerts table); no shared code paths with password reset. No impact. | NO_CHANGE |
| FIX-230 | Policy Rollout DSL Match — backend only, unrelated domain. No impact. | NO_CHANGE |
| FIX-231 | Policy Version State Machine — backend only. No impact. | NO_CHANGE |
| FIX-232 | Rollout UI — different UI section. No impact. | NO_CHANGE |
| Future auth hardening | DEV-324 constant-time enumeration defense (dummy bcrypt spy) established here. Consider backporting to `internal/api/auth/handler.go` login path if timing-based username enumeration is ever flagged in a security audit. Not a blocker — login already uses bcrypt uniformly; this note is a forward-looking opportunity. | NO_CHANGE (note only) |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/USERTEST.md` | Added `## FIX-228:` section with 9 scenario groups (forgot link + a11y, forgot existing email + mailhog, forgot non-existent email enumeration defense, rate-limit 6th request, reset valid token full roundtrip + single-use enforcement, reset missing/invalid token inline panel, password policy error + autocomplete, version footer on 3 pages, mailhog fixture check) | UPDATED |
| `docs/FRONTEND.md` | Added Tailwind v4 utility class note (auto-derived from `@theme`): `text-danger`, `bg-danger-dim`, `border-danger`, `text-success`, `bg-success-dim`, `text-accent`, etc. are the project tokens. DO NOT use Tailwind default palette. | UPDATED |
| `docs/SCREENS.md` | Corrected total count header: 81 → 83 (SCR-193 Forgot Password + SCR-194 Reset Password added by FIX-228). | UPDATED |
| `docs/ROUTEMAP.md` | (1) Reverted FIX-228 status from `[x] DONE (2026-04-25)` → `[~] IN PROGRESS` — per AUTOPILOT protocol, flip to DONE belongs to Commit step. (2) Added D-131: dead `cleanupPasswordResetUser` helper in `internal/store/password_reset_test.go` deferred to FIX-24x (test cleanup). | UPDATED |
| `CLAUDE.md` | Added Mailhog row to Docker Services table: `:1025 (SMTP), http://localhost:8025 (Web UI) — Dev SMTP catch-all for password reset emails (FIX-228 DEV-328)`. | UPDATED |
| `docs/brainstorming/decisions.md` | DEV-323..DEV-332 (10 entries) confirmed present at lines 558-567. No new entries required. | NO_CHANGE |
| `docs/architecture/ERROR_CODES.md` | `PASSWORD_RESET_INVALID_TOKEN` confirmed at line 43. | NO_CHANGE |
| `docs/architecture/CONFIG.md` | 3 new env vars confirmed at lines 164-166. | NO_CHANGE |
| `docs/architecture/api/_index.md` | API-317 + API-318 confirmed at lines 35-36. | NO_CHANGE |
| `docs/architecture/db/_index.md` | TBL-54 confirmed at line 64 (TBL-50 was already taken by `user_views`). | NO_CHANGE |
| `docs/ARCHITECTURE.md` | FE-first story; new auth flow endpoints tracked in `api/_index.md` (API-317/318) and table in `db/_index.md` (TBL-54). Top-level ARCHITECTURE.md does not enumerate individual auth flows — consistent with existing pattern. | NO_CHANGE |
| `docs/GLOSSARY.md` | No new domain terms required. "Rate Limiting" (line 182) and "Password Policy" (line 266) already present. Opaque token / enumeration defense are implementation details, not platform domain terms. | NO_CHANGE |
| `docs/FUTURE.md` | No new future opportunities surfaced. | NO_CHANGE |
| `Makefile` | No new services or targets added (mailhog is a compose service, not a Makefile target). | NO_CHANGE |

## Plan Deviations — Confirmed Decisions

| Deviation | Disposition |
|-----------|-------------|
| Plan specified TBL-50 for `password_reset_tokens`; TBL-50 already taken by `user_views`. Developer used TBL-54 (next available after TBL-53). Documented in step log (STEP_3), `db/_index.md` uses TBL-54, COMMENT ON TABLE says `TBL-54`. | ACCEPTED — correctly handled, no doc gap. |
| Plan AC-4 wording said "jwt" token. DEV-323 explicitly documented this as a drafting error and pinned the implementation to OWASP-standard opaque SHA-256-hashed token. `decisions.md` DEV-323 captures rationale. | ACCEPTED — standard opaque token is superior; decision documented. |

## Cross-Doc Consistency

- Contradictions found: 0
- SCREENS.md total count was 81 (stale) — corrected to 83 during this review.
- CLAUDE.md Docker Services missing mailhog — corrected during this review.

## Decision Tracing (Check #11)

| Decision | Verified in code/config/doc |
|----------|-----------------------------|
| DEV-323 (opaque token, SHA-256, single-use) | `internal/store/password_reset.go` (FindByHash + DeleteByHash), migration `token_hash BYTEA UNIQUE` |
| DEV-324 (constant-time bcrypt spy) | `internal/api/auth/handler.go` `dummyBcryptHook` field + `dummyBcryptHash` pkg var; `password_reset.go` calls hook on not-found path |
| DEV-325 (always-200 generic body) | `password_reset.go` — 200 returned on not-found, email-fail, and success paths |
| DEV-326 (rate-limit before DB lookup, DB-only, `email_rate_key` window) | `CountRecentForEmail` call at top of `RequestPasswordReset` before `userStore.GetByEmail` |
| DEV-327 (SendTo + embed.FS templates) | `internal/notification/email.go` + `templates.go` with `//go:embed templates/*.tmpl` |
| DEV-328 (mailhog compose fixture) | `deploy/docker-compose.yml` mailhog service; `.env.example` SMTP_HOST=mailhog |
| DEV-329 (Vite `__APP_VERSION__`) | `web/vite.config.ts` define block; `web/src/components/layout/auth-layout.tsx` footer |
| DEV-330 (no-referrer meta) | `web/index.html` `<meta name="referrer" content="no-referrer" />` |
| DEV-331 (reuse ValidatePasswordPolicy) | `internal/auth/auth.go` `ResetPasswordForUser` calls `ValidatePasswordPolicy` |
| DEV-332 (inline PurgeExpired GC) | `PurgeExpired` called at start of `RequestPasswordReset` |

Orphaned decisions: 0

## USERTEST Completeness (Check #12)

- Entry exists: YES (added in this review)
- Type: UI scenarios — 9 scenario groups covering all 8 ACs + mailhog fixture
- Status: UPDATED (was MISSING, now added)

## Tech Debt Pickup (Check #13)

- Items targeting FIX-228 BEFORE this review: 0 (D-130 was added BY this story, not targeting it)
- D-130 (mailhog SHA pin): OPEN — targeting `FIX-24x (infra pinning)`. Correctly deferred.
- D-131 (dead `cleanupPasswordResetUser` helper): ADDED in this review — targeting `FIX-24x (test cleanup)`.

## Mock Status (Check #14)

- Frontend-First: N/A — no mocks existed for the new password reset API surface (new feature, not replacement).

## PAT-019 Candidate Disposition (Check #14 extension)

Gate flagged **PAT-019 candidate** (typed-nil pointer wrapped in non-nil interface) from C-01. Not yet written to `docs/brainstorming/bug-patterns.md` — gate explicitly deferred this to Commit step. Current state: no PAT-019 entry exists. Commit step MUST author the entry.

## PAT-017 Wiring Trace Verification (Check #2 extension)

Step log reports: `PasswordResetRateLimitPerHour` — 7 hits; `PasswordResetTokenTTLMinutes` — 4 hits; `WithPasswordReset` — 2 (definition + call site). Meets the plan's ≥5 hit requirement.

`WithUserStore` for `authHandler`: single call site at `cmd/argus/main.go:520` — clean. Other `WithUserStore` hits belong to `anomalyapi` and `adminHandler` — different handler types, no PAT-011 concern.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | SCREENS.md total count header showed 81 despite FIX-228 adding 2 screens (SCR-193, SCR-194) → count should be 83 | NON-BLOCKING | FIXED | Updated header from "Total: 81" to "Total: 83" |
| 2 | ROUTEMAP FIX-228 status was pre-emptively flipped to `[x] DONE (2026-04-25)` by Dev Task 11 — per AUTOPILOT protocol, flip belongs to Commit step | NON-BLOCKING | FIXED | Reverted to `[~] IN PROGRESS` |
| 3 | CLAUDE.md Docker Services table missing Mailhog service (added in FIX-228 DEV-328) | NON-BLOCKING | FIXED | Added `Mailhog | :1025 (SMTP), http://localhost:8025 (Web UI)` row |
| 4 | USERTEST.md had no `## FIX-228:` section | NON-BLOCKING | FIXED | Added 9 scenario groups covering all 8 ACs + mailhog fixture |
| 5 | FRONTEND.md documented `--danger` CSS variable values but not the derived Tailwind utility classes (`text-danger`, `bg-danger-dim`, `border-danger`). Plan's token map incorrectly listed `text-destructive` / `border-destructive` — actual project tokens are `text-danger` / `bg-danger-dim` as used in existing login.tsx and new reset.tsx | NON-BLOCKING | FIXED | Added Tailwind v4 utility class note explaining auto-derivation from `@theme` and canonical token names |
| 6 | Dead `cleanupPasswordResetUser` helper in `internal/store/password_reset_test.go` (retained as gate M-03 minimum-diff) | NON-BLOCKING | DEFERRED D-131 | Added D-131 to ROUTEMAP Tech Debt targeting FIX-24x (test cleanup) |
| 7 | PAT-019 (typed-nil interface trap) written to `docs/brainstorming/bug-patterns.md` | UPDATED | FIXED (this review) | Comprehensive entry authored at `bug-patterns.md` L26 covering root cause, 3 safe patterns, Gate grep rule, constructor-level nil-interface test recipe. C-01 code fix already applied at Gate. |

## Project Health

- Stories completed: FIX-228 at Review step (Commit pending)
- Current phase: UI Review Remediation Wave 7 (P3 stories)
- Next story: FIX-229 — Alert Feature Enhancements
- Blockers: None
- Open tech debt: D-130 (mailhog SHA pin), D-131 (dead test helper)

## Check Summary

| # | Check | Status | Notes |
|---|-------|--------|-------|
| 1 | Plan accuracy (TBL-50→54, jwt→opaque deviation) | PASS | Both deviations correctly documented in step log + decisions.md |
| 2 | Architecture evolution (ARCHITECTURE.md, API index, DB index) | NO_CHANGE | API-317/318 in `api/_index.md`; TBL-54 in `db/_index.md`; top-level ARCHITECTURE.md consistent with pattern |
| 3 | USERTEST completeness | UPDATED | Added `## FIX-228:` section with 9 scenario groups |
| 4 | New terms (GLOSSARY) | NO_CHANGE | No new domain terms; existing entries cover the domain |
| 5 | Screen updates (SCREENS.md) | UPDATED | Total count corrected 81→83; SCR-193/194 entries already present |
| 6 | FRONTEND.md token documentation | UPDATED | Added Tailwind v4 utility class derivation note + `text-danger` / `bg-danger-dim` canonical form |
| 7 | FUTURE.md relevance | NO_CHANGE | No new future opportunities surfaced |
| 8 | Decisions captured (decisions.md DEV-323..332) | PASS | All 10 entries confirmed at lines 558-567 |
| 9 | Makefile / .env.example consistency | PASS | `.env.example` has SMTP_HOST=mailhog, PASSWORD_RESET_* vars; mailhog is compose service not Makefile target |
| 10 | CLAUDE.md consistency | UPDATED | Added Mailhog row to Docker Services table |
| 11 | Decision tracing (10 DEV-NNN) | PASS | All 10 decisions verified in code — 0 orphaned |
| 12 | USERTEST completeness | UPDATED | (Same as check #3) |
| 13 | Tech debt pickup | PASS | D-130 correctly OPEN; D-131 added; 0 targeting-FIX-228 items missed |
| 14 | Bug-patterns.md (PAT-019) | UPDATED | PAT-019 WRITTEN at `bug-patterns.md` L26 with root cause + 3 safe patterns + grep rule + constructor-level nil-interface test recipe. C-01 code fix already applied at Gate. |
